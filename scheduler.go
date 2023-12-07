package scheduler

import (
	"fmt"
	"github.com/danze/scheduler/logger"
	"github.com/google/uuid"
	"sync"
	"time"
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
		logger.Debug("scheduler stopped")
		logger.Debug("waiting for all task goroutines to exit ...")
		s.wg.Wait()
		close(s.result)
		logger.Debug("closed result channel")
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
				logger.Debug("exiting Updater goroutine")
				return
			}
		}
	}()

	return s
}

// Stop stops this Scheduler and all tasks that are not completed.
func (s *Scheduler) Stop() {
	close(s.stopped)
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
	go s.runner(id, task, cancel)
	return id, nil
}

type taskResult struct {
	output any
	err    error
}

func (s *Scheduler) runner(id string, t Task, cancel chan struct{}) {
	defer s.wg.Done()

	s.result <- TaskStatus{
		ID:     id,
		Status: TaskRunning,
	}

	var taskChan = make(chan *taskResult, 1)
	go func() {
		// this Executor goroutine will never exit until after t.Exec() returns
		output, err := t.Exec()
		taskChan <- &taskResult{output, err}
		close(taskChan)
		logger.Debug("Task exited [" + id + "]")
	}()

	timer := time.NewTimer(t.Timeout())
	defer func() {
		// allows the garbage collector to reclaim the timer
		timer.Stop()
	}()

	for {
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
		case <-timer.C:
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
	close(status.cancel)
	return nil
}
