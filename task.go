package scheduler

import (
	"context"
	"fmt"
)

// Task defines a user-submitted function that is executed by the Scheduler.
// The task receives a context.Context which it should monitor via ctx.Done()
// to handle cancellation or timeouts gracefully.
type Task func(context.Context) (any, error)

// Status represents the current state of a Task in the Scheduler.
type Status int

const (
	// TaskScheduled indicates the task has been submitted but hasn't started yet.
	TaskScheduled Status = 1 + iota
	// TaskRunning indicates the task is currently being executed.
	TaskRunning
	// TaskStopped indicates the task was aborted because the Scheduler was stopped.
	TaskStopped
	// TaskCancelled indicates the task was explicitly cancelled by the user.
	TaskCancelled
	// TaskCompleted indicates the task finished execution (either successfully or with an error).
	TaskCompleted
	// TaskTimedOut indicates the task was aborted because it exceeded its timeout.
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

// TaskStatus contains the latest state and results of a task identified by ID.
// If the task has finished (as indicated by Completed()), then either Output or
// Err will contain the final result.
type TaskStatus struct {
	ID     string // Unique identifier for the task.
	Status Status // Current state of the task.
	Output any    // The result of the task if it completed successfully.
	Err    error  // The error returned by the task or why it failed.
}

// Completed returns true if the task has finished execution, been cancelled,
// timed out, or the scheduler has stopped.
func (t *TaskStatus) Completed() bool {
	s := t.Status
	return s != TaskRunning && s != TaskScheduled
}

// String returns a brief string representation of the TaskStatus.
func (t *TaskStatus) String() string {
	return fmt.Sprintf("TaskStatus id: %s, status: %v, output: %v, error: %v",
		t.ID, t.Status, t.Output, t.Err)
}

// StringDetailed returns a more descriptive string representation of the
// TaskStatus, including results if the task is completed.
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
