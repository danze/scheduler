package scheduler

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var myTaskOneMilliSec = newMockTask(time.Millisecond, false)
var myTaskOneSec = newMockTask(time.Second, false)
var myTaskPanics = newMockTask(time.Millisecond, true)

var s *Scheduler
var buf bytes.Buffer

func beforeTest() {
	buf.Reset()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	s = New()
}

func afterTest(t *testing.T, taskID string) {
	s.Stop()
	s.Wait()
	logs := buf.String()
	fmt.Println(logs)
	if taskID != "" {
		expectedString := fmt.Sprintf("Task %v exited with result", taskID)
		assert.Contains(t, logs, expectedString, "Task goroutine did not exit")
	}
}

func Test_Completed(t *testing.T) {
	// 1 sec task
	runTaskHelper(t, myTaskOneSec)
	// 1 milli sec task
	runTaskHelper(t, myTaskOneMilliSec)
	// task that panics
	runTaskHelper(t, myTaskPanics)
}

func runTaskHelper(t *testing.T, task Task) {
	beforeTest()
	id, err := s.Submit(task)
	assert.Nil(t, err, "failed to submit task")

	status, err := s.Status(id)
	for err == nil && !status.Completed() {
		time.Sleep(100 * time.Millisecond)
		status, err = s.Status(id)
	}
	checkStatus(t, s, id, TaskCompleted)
	afterTest(t, id)
}

func Test_Cancelled(t *testing.T) {
	beforeTest()
	id, err := s.Submit(myTaskOneSec)
	assert.Nil(t, err, "failed to submit task")

	err = s.Cancel(id)
	assert.Nil(t, err, "failed to cancel task")

	time.Sleep(time.Second)
	checkStatus(t, s, id, TaskCancelled)

	// cancelling twice has no effect
	err = s.Cancel(id)
	assert.Nil(t, err, "failed to cancel task second time")
	afterTest(t, id)
}

func Test_TimedOut(t *testing.T) {
	beforeTest()
	id, err := s.SubmitWithTimeout(myTaskOneSec, 200*time.Millisecond)
	assert.Nil(t, err, "failed to submit task")

	var startedRunning bool
	status, err := s.Status(id)
	for err == nil && !status.Completed() {
		if status.Status == TaskRunning {
			startedRunning = true
		}
		time.Sleep(50 * time.Millisecond)
		status, err = s.Status(id)
	}
	assert.Nil(t, err, "failed to get task status")
	assert.True(t, startedRunning)
	checkStatus(t, s, id, TaskTimedOut)
	afterTest(t, id)
}

func Test_Stopped(t *testing.T) {
	beforeTest()
	id, err := s.Submit(myTaskOneSec)
	assert.Nil(t, err, "failed to submit task")

	s.Stop()
	time.Sleep(time.Second)
	checkStatus(t, s, id, TaskStopped)

	err = s.Cancel(id)
	assert.Nil(t, err, "cancelling completed task should have no effect")

	_, err = s.Submit(myTaskOneSec)
	assert.NotNil(t, err, "stopped scheduler should not accept new tasks")
	afterTest(t, id)
}

func Test_MultipleCompleted(t *testing.T) {
	beforeTest()
	size := 1000
	taskID := make([]string, 0, size)
	var id string
	var err error
	for i := 0; i < size; i++ {
		delay := time.Duration(1+rand.Intn(1000)) * time.Millisecond
		if i%2 == 0 {
			id, err = s.Submit(newMockTask(delay, false))
		} else {
			id, err = s.Submit(newMockTask(delay, true))
		}
		assert.Nil(t, err, "failed to submit task")
		taskID = append(taskID, id)
	}
	time.Sleep(2 * time.Second)
	for _, id := range taskID {
		checkStatus(t, s, id, TaskCompleted)
		err = s.Remove(id)
		assert.Nil(t, err, "failed to remove task")
	}
	for _, id := range taskID {
		_, err = s.Status(id)
		assert.NotNil(t, err, "task %v not removed", id)
	}
	afterTest(t, "")
}

func Test_MultipleCancelled(t *testing.T) {
	beforeTest()
	size := 1000
	taskID := make([]string, 0, size)
	for i := 0; i < size; i++ {
		id, err := s.Submit(myTaskOneSec)
		assert.Nil(t, err, "failed to submit task")
		taskID = append(taskID, id)
	}
	for _, id := range taskID {
		err := s.Cancel(id)
		assert.Nil(t, err, "failed to cancel task")
	}
	// Wait for Updater goroutine to finish processing all updates.
	time.Sleep(time.Second)
	for _, id := range taskID {
		checkStatus(t, s, id, TaskCancelled)
	}
	afterTest(t, "")
}

func Test_MultipleTimedOut(t *testing.T) {
	beforeTest()
	size := 1000
	taskID := make([]string, 0, size)
	for i := 0; i < size; i++ {
		id, err := s.SubmitWithTimeout(myTaskOneSec, time.Millisecond)
		assert.Nil(t, err, "failed to submit task")
		taskID = append(taskID, id)
	}
	time.Sleep(time.Second)
	for _, id := range taskID {
		checkStatus(t, s, id, TaskTimedOut)
	}
	afterTest(t, "")
}

func Test_MultipleStopped(t *testing.T) {
	beforeTest()
	size := 1000
	taskID := make([]string, 0, size)
	for i := 0; i < size; i++ {
		id, err := s.Submit(myTaskOneSec)
		assert.Nil(t, err, "failed to submit task")
		taskID = append(taskID, id)
	}
	s.Stop()
	// Wait for Updater goroutine to finish processing all updates.
	time.Sleep(time.Second)
	for _, id := range taskID {
		checkStatus(t, s, id, TaskStopped)
	}
	afterTest(t, "")
}

func checkStatus(t *testing.T, s *Scheduler, id string, expected Status) {
	status, err := s.Status(id)
	assert.Nil(t, err, "failed to get task status")
	assert.Equal(t, expected, status.Status)
}

func newMockTask(delay time.Duration, returnWithPanic bool) Task {
	return func(ctx context.Context) (any, error) {
		select {
		case <-time.After(delay):
			if returnWithPanic {
				panic("invalid state")
			}
			return "Result is 42", nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
