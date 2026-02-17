package scheduler

import (
	"context"
	"fmt"
)

// Task defines a user submitted task that is executed by Scheduler.
// The task receives a context.Context which should be checked for cancellation.
// Users should monitor ctx.Done() and return early if the context is canceled.
type Task func(context.Context) (any, error)

type Status int

const (
	TaskScheduled Status = 1 + iota
	TaskRunning
	TaskStopped
	TaskCancelled
	TaskCompleted
	TaskTimedOut
)

func (t Status) String() string {
	switch t {
	case TaskScheduled:
		return "Scheduled"
	case TaskRunning:
		return "Running"
	case TaskStopped:
		return "Stopped"
	case TaskCancelled:
		return "Cancelled"
	case TaskCompleted:
		return "Completed"
	case TaskTimedOut:
		return "TimedOut"
	default:
		return "InvalidState"
	}
}

// TaskStatus is status of the Task identified by ID. If this Task has
// finished running as defined by the Completed method, then either Output or
// Err is non-nil.
type TaskStatus struct {
	ID     string // Task ID
	Status Status // Latest status of this Task
	Output any    // Result if this Task completed successfully
	Err    error  // Error if this Task failed
}

// Completed returns true if this task is not running and not scheduled.
func (t *TaskStatus) Completed() bool {
	s := t.Status
	return s != TaskRunning && s != TaskScheduled
}

func (t *TaskStatus) String() string {
	return fmt.Sprintf("TaskStatus id: %s, status: %v, output: %v, error: %v",
		t.ID, t.Status, t.Output, t.Err)
}

func (t *TaskStatus) StringDetailed() string {
	statusString := fmt.Sprintf("ID: %s, Status: %v", t.ID, t.Status)
	if t.Completed() {
		if t.Err != nil {
			statusString = fmt.Sprintf("%s, Error: %v", statusString, t.Err)
		} else {
			statusString = fmt.Sprintf("%s, Output: %v", statusString, t.Output)
		}
	}
	return "TaskStatus [" + statusString + "]"
}
