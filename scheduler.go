package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Scheduler runs Task submitted by a user. It allows the user to get status
// or cancel a previously submitted Task. It also allows the user to stop this
// Scheduler which has the effect of stopping also tasks that are not
// complete.
type Scheduler struct {
	stopped chan struct{}
	result  chan TaskStatus
	tasks   map[string]struct {
		*TaskStatus
		cancel chan struct{}
	}
	wg        *sync.WaitGroup
	sysWg     *sync.WaitGroup // Waits for system goroutines (Closer/Updater)
	mu        *sync.Mutex
	isStopped bool
}

// New creates and initializes a new Scheduler.
func New() *Scheduler {
	s := &Scheduler{
		stopped: make(chan struct{}),
		result:  make(chan TaskStatus),
		tasks: make(map[string]struct {
			*TaskStatus
			cancel chan struct{}
		}),
		wg:    new(sync.WaitGroup),
		sysWg: new(sync.WaitGroup),
		mu:    new(sync.Mutex),
	}

	updateFunc := func(r TaskStatus) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		st, ok := s.tasks[r.ID]
		if !ok {
			return fmt.Errorf("unknown task: %s", r.ID)
		}
		st.Status = r.Status
		st.Output = r.Output
		st.Err = r.Err
		return nil
	}

	// "Closer" goroutine
	s.sysWg.Add(1)
	go func() {
		defer s.sysWg.Done()
		// After user stops the scheduler (`stopped` channel is closed), running tasks
		// will immediately exit with `TaskStopped` status and any new task submission
		// will fail.
		<-s.stopped
		// After all tasks exit, this goroutine will close `result` channel and signals
		// the "Updater" goroutine to start draining the channel and then exit.
		slog.Debug("scheduler stopped: waiting for all task goroutines to exit ...")
		s.wg.Wait()
		close(s.result)
		slog.Debug("scheduler stopped: all task goroutines exited and 'result' channel closed")
	}()

	// "Updater" goroutine receives status updates from tasks and
	// updates the schedule `tasks` map.
	s.sysWg.Add(1)
	go func() {
		defer s.sysWg.Done()
		var err error
		for {
			select {
			case r, ok := <-s.result:
				if !ok {
					slog.Debug("scheduler stopped: 'Updater' goroutine completed reading updates and exited")
					return
				}
				err = updateFunc(r)
				if err != nil {
					slog.Warn("failed to update task status in map: " + err.Error())
				}
			case <-s.stopped:
				// Continue draining s.result in the loop above until it is closed.
				// This case is kept to ensure we don't block if s.result is not yet closed.
			}
		}
	}()

	return s
}

// Stop stops this Scheduler and all tasks that are not completed.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if s.isStopped {
		s.mu.Unlock()
		return
	}
	s.isStopped = true
	close(s.stopped)
	s.mu.Unlock()
}

// Wait waits for all tasks to finish and the scheduler to fully stop.
// This should be called after Stop().
func (s *Scheduler) Wait() {
	s.sysWg.Wait()
}

// IsStopped checks if this Scheduler has been stopped.
func (s *Scheduler) IsStopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isStopped
}

// Submit schedules the given Task to run by this Scheduler. If the Task is
// scheduled successfully, it returns task ID otherwise an error.
func (s *Scheduler) Submit(task Task) (string, error) {
	return s.SubmitWithTimeout(task, time.Duration(1<<63-1))
}

// SubmitWithTimeout schedules the given Task to run by this Scheduler. It sets
// the task timeout to the given value. If the Task is
// scheduled successfully, it returns task ID otherwise an error.
func (s *Scheduler) SubmitWithTimeout(task Task, timeout time.Duration) (string, error) {
	s.mu.Lock()
	if s.isStopped {
		s.mu.Unlock()
		return "", fmt.Errorf("scheduler has been stopped")
	}
	id := uuid.NewString()
	cancel := make(chan struct{})
	s.tasks[id] = struct {
		*TaskStatus
		cancel chan struct{}
	}{
		TaskStatus: &TaskStatus{
			ID:     id,
			Status: TaskScheduled,
			Output: nil,
			Err:    nil,
		},
		cancel: cancel,
	}
	s.wg.Add(1)
	s.mu.Unlock()

	// creates Runner goroutine for this task
	go s.runner(id, task, timeout, cancel)
	return id, nil
}

type taskResult struct {
	output any
	err    error
}

func (s *Scheduler) runner(id string, task Task, timeout time.Duration, cancel chan struct{}) {
	defer s.wg.Done()

	s.result <- TaskStatus{
		ID:     id,
		Status: TaskRunning,
	}

	// Create cancellable context for the task
	ctx, cancelCtx := context.WithCancel(context.Background())
	defer cancelCtx() // Ensure context is always canceled when Runner goroutine exits

	var taskChan = make(chan *taskResult, 1)
	// creates Executor goroutine to run user task
	go func() {
		var output any
		var err error
		defer func() {
			// check if user task panicked
			if r := recover(); r != nil {
				switch w := r.(type) {
				case error:
					err = w
				default:
					// panicked with non-error value
					err = fmt.Errorf("%v", r)
				}
				err = fmt.Errorf("panicked: %w", err)
				taskChan <- &taskResult{nil, err}
			}
			close(taskChan)
			slog.Debug(fmt.Sprintf("Task %v exited with result (output: %v, error: %v)", id, output, err))
		}()
		output, err = task(ctx)
		taskChan <- &taskResult{output, err}
	}()

	timedOut := time.After(timeout.Abs())

	var finalStatus TaskStatus
	// Prioritize task completion over cancellation/timeout/stop signals
	// to avoid marking a completed task with the wrong status
	select {
	case r := <-taskChan:
		// Task completed - this takes priority
		finalStatus = TaskStatus{
			ID:     id,
			Status: TaskCompleted,
			Output: r.output,
			Err:    r.err,
		}
	default:
		// Task not yet completed, check for cancel/stop/timeout
		select {
		case <-cancel:
			finalStatus = TaskStatus{
				ID:     id,
				Status: TaskCancelled,
			}
		case <-s.stopped:
			finalStatus = TaskStatus{
				ID:     id,
				Status: TaskStopped,
			}
		case <-timedOut:
			finalStatus = TaskStatus{
				ID:     id,
				Status: TaskTimedOut,
			}
		case r := <-taskChan:
			finalStatus = TaskStatus{
				ID:     id,
				Status: TaskCompleted,
				Output: r.output,
				Err:    r.err,
			}
		}
	}

	s.result <- finalStatus
	if finalStatus.Status != TaskCompleted {
		// If we return because of cancel/stop/timeout, the context is cancelled by defer cancelCtx().
		// We wait for the Executor goroutine to finish to avoid leaks.
		// If the user task is bad and hangs, this will block the runner (and thus s.wg.Wait() during Stop).
		<-taskChan
	}
}

// Status returns latest status of a task with the given ID.
func (s *Scheduler) Status(id string) (*TaskStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	status, ok := s.tasks[id]
	if !ok {
		return nil, fmt.Errorf("not found: %s", id)
	}
	ts := *status.TaskStatus
	ts.Output = deepCopyAny(ts.Output)
	return &ts, nil
}

// Cancel cancels a task with the given ID. If the task has already completed,
// Cancel has no effect.
func (s *Scheduler) Cancel(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	status, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("not found: %s", id)
	}
	select {
	case <-status.cancel:
		// Already closed
		return nil
	default:
		close(status.cancel)
	}
	return nil
}

// Remove removes a completed task from the scheduler's task map.
// This should be called to prevent memory leaks when tasks are no longer needed.
// Returns an error if the task is not found or is still running.
func (s *Scheduler) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	status, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("not found: %s", id)
	}
	if !status.TaskStatus.Completed() {
		return fmt.Errorf("cannot remove task that is still running: %s", id)
	}
	delete(s.tasks, id)
	return nil
}

func deepCopyAny(v any) any {
	if v == nil {
		return nil
	}
	visited := make(map[uintptr]reflect.Value)
	return deepCopyValue(reflect.ValueOf(v), visited).Interface()
}

func deepCopyValue(v reflect.Value, visited map[uintptr]reflect.Value) reflect.Value {
	if !v.IsValid() {
		return v
	}

	// Handle pointer-based types for circular references
	kind := v.Kind()
	if kind == reflect.Ptr || kind == reflect.Map || kind == reflect.Slice || kind == reflect.Interface {
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		if kind != reflect.Interface {
			ptr := v.Pointer()
			if copyV, ok := visited[ptr]; ok {
				return copyV
			}
		}
	}

	switch kind {
	case reflect.Ptr:
		dst := reflect.New(v.Type().Elem())
		visited[v.Pointer()] = dst
		dst.Elem().Set(deepCopyValue(v.Elem(), visited))
		return dst
	case reflect.Map:
		dst := reflect.MakeMap(v.Type())
		visited[v.Pointer()] = dst
		for _, k := range v.MapKeys() {
			dst.SetMapIndex(deepCopyValue(k, visited), deepCopyValue(v.MapIndex(k), visited))
		}
		return dst
	case reflect.Slice:
		dst := reflect.MakeSlice(v.Type(), v.Len(), v.Cap())
		visited[v.Pointer()] = dst
		for i := 0; i < v.Len(); i++ {
			dst.Index(i).Set(deepCopyValue(v.Index(i), visited))
		}
		return dst
	case reflect.Struct:
		dst := reflect.New(v.Type()).Elem()
		for i := 0; i < v.NumField(); i++ {
			if dst.Field(i).CanSet() {
				dst.Field(i).Set(deepCopyValue(v.Field(i), visited))
			}
		}
		return dst
	case reflect.Interface:
		dst := reflect.New(v.Type()).Elem()
		dst.Set(deepCopyValue(v.Elem(), visited))
		return dst
	default:
		// Primitive types (bool, int, float, string, etc.) are immutable values.
		// Chan and func are not deep-copyable; return as-is.
		return v
	}
}
