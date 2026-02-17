package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/danze/scheduler/logger"
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
	wg *sync.WaitGroup
	mu *sync.Mutex
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
		wg: new(sync.WaitGroup),
		mu: new(sync.Mutex),
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
	go func() {
		// After user stops the scheduler (`stopped` channel is closed), running tasks
		// will immediately exit with `TaskStopped` status and any new task submission
		// will fail.
		<-s.stopped
		// After all tasks exit, this goroutine will close `result` channel and signals
		// the "Updater" goroutine to start draining the channel and then exit.
		logger.Debug("scheduler stopped: waiting for all task goroutines to exit ...")
		s.wg.Wait()
		close(s.result)
		logger.Debug("scheduler stopped: all task goroutines exited and 'result' channel closed")
	}()

	// "Updater" goroutine receives status updates from tasks and
	// updates the schedule `tasks` map.
	go func() {
		var err error
		for {
			select {
			case r := <-s.result:
				err = updateFunc(r)
				if err != nil {
					logger.Warn("failed to update task status in map: " + err.Error())
				}
			case <-s.stopped:
				for r := range s.result {
					err = updateFunc(r)
					if err != nil {
						logger.Warn("failed to update task status in map: " + err.Error())
					}
				}
				logger.Debug("scheduler stopped: 'Updater' goroutine completed reading updates and exited")
				return
			}
		}
	}()

	return s
}

// Stop stops this Scheduler and all tasks that are not completed.
func (s *Scheduler) Stop() {
	select {
	case <-s.stopped:
		// Already stopped
		return
	default:
		close(s.stopped)
	}
}

// IsStopped checks if this Scheduler has been stopped.
func (s *Scheduler) IsStopped() bool {
	select {
	case <-s.stopped:
		return true
	default:
		return false
	}
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
	if s.IsStopped() {
		return "", fmt.Errorf("scheduler has been stopped")
	}
	id := uuid.NewString()
	cancel := make(chan struct{})
	s.mu.Lock()
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
	s.mu.Unlock()
	s.wg.Add(1)
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
			logger.Debug(fmt.Sprintf("Task %v exited with result (output: %v, error: %v)", id, output, err))
		}()
		output, err = task(ctx)
		taskChan <- &taskResult{output, err}
	}()

	timedOut := time.After(timeout.Abs())

	// Prioritize task completion over cancellation/timeout/stop signals
	// to avoid marking a completed task with the wrong status
	select {
	case r := <-taskChan:
		// Task completed - this takes priority
		s.result <- TaskStatus{
			ID:     id,
			Status: TaskCompleted,
			Output: r.output,
			Err:    r.err,
		}
		return
	default:
		// Task not yet completed, check for cancel/stop/timeout
		select {
		case <-cancel:
			s.result <- TaskStatus{
				ID:     id,
				Status: TaskCancelled,
			}
			return
		case <-s.stopped:
			s.result <- TaskStatus{
				ID:     id,
				Status: TaskStopped,
			}
			return
		case <-timedOut:
			s.result <- TaskStatus{
				ID:     id,
				Status: TaskTimedOut,
			}
			return
		case r := <-taskChan:
			s.result <- TaskStatus{
				ID:     id,
				Status: TaskCompleted,
				Output: r.output,
				Err:    r.err,
			}
			return
		}
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
	// TODO: TaskStatus is a pointer type and could be updated by multiple goroutines
	return status.TaskStatus, nil
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
