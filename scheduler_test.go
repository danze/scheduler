package scheduler

import (
	"bytes"
	"fmt"
	"github.com/danze/scheduler/logger"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

type myTask struct {
	exec, timeout time.Duration
}

func (t *myTask) Exec() (any, error) {
	time.Sleep(t.exec)
	return "Result is 42", nil
}

func (t *myTask) Timeout() time.Duration {
	if t.timeout == 0 {
		return time.Duration(1<<63 - 1)
	}
	return t.timeout.Abs()
}

var s *Scheduler
var buf bytes.Buffer

func beforeTest() {
	logger.SetLogLevel(logger.LevelDebug)
	buf.Reset()
	logger.SetOutput(&buf)
	s = New()
}

func afterTest(t *testing.T, taskID string) {
	logs := buf.String()
	fmt.Println(logs)
	if taskID != "" {
		expectedString := fmt.Sprintf("Task exited [%s]", taskID)
		assert.Contains(t, logs, expectedString, "Task goroutine did not exit")
	}
}

func Test_Completed1(t *testing.T) {
	// schedule 2 sec task
	runTaskHelper(t, &myTask{exec: 2 * time.Second})
}

func Test_Completed2(t *testing.T) {
	// schedule short-lived task
	runTaskHelper(t, &myTask{exec: time.Millisecond})
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
	id, err := s.Submit(&myTask{exec: 2 * time.Second})
	assert.Nil(t, err, "failed to submit task")

	err = s.Cancel(id)
	assert.Nil(t, err, "failed to cancel task")

	time.Sleep(time.Second)
	checkStatus(t, s, id, TaskCancelled)

	time.Sleep(2 * time.Second)
	afterTest(t, id)
}

func Test_TimedOut(t *testing.T) {
	beforeTest()
	id, err := s.Submit(&myTask{exec: 2 * time.Second, timeout: time.Second})
	assert.Nil(t, err, "failed to submit task")

	var wasRunning bool
	status, err := s.Status(id)
	for err == nil && !status.Completed() {
		if status.Status == TaskRunning {
			wasRunning = true
		}
		time.Sleep(100 * time.Millisecond)
		status, err = s.Status(id)
	}
	assert.Nil(t, err, "failed to get task status")
	assert.True(t, wasRunning)
	checkStatus(t, s, id, TaskTimedOut)

	time.Sleep(2 * time.Second)
	afterTest(t, id)
}

func Test_Stopped(t *testing.T) {
	beforeTest()
	task := &myTask{exec: time.Second}
	id, err := s.Submit(task)
	assert.Nil(t, err, "failed to submit task")

	s.Stop()
	time.Sleep(time.Second)
	checkStatus(t, s, id, TaskStopped)

	err = s.Cancel(id)
	assert.Nil(t, err, "cancelling completed task should have no effect")

	_, err = s.Submit(task)
	assert.NotNil(t, err, "expected an error")

	time.Sleep(2 * time.Second)
	afterTest(t, id)
}

func Test_MultipleCompleted(t *testing.T) {
	beforeTest()
	max := 1000
	taskID := make([]string, 0, max)
	for i := 0; i < max; i++ {
		task := &myTask{exec: 0}
		id, err := s.Submit(task)
		assert.Nil(t, err, "failed to submit task")
		taskID = append(taskID, id)
	}
	time.Sleep(time.Second)
	for _, id := range taskID {
		checkStatus(t, s, id, TaskCompleted)
	}
	afterTest(t, "")
}

func Test_MultipleCancelled(t *testing.T) {
	beforeTest()
	max := 1000
	taskID := make([]string, 0, max)
	for i := 0; i < max; i++ {
		task := &myTask{exec: 2 * time.Second}
		id, err := s.Submit(task)
		assert.Nil(t, err, "failed to submit task")
		taskID = append(taskID, id)
	}
	for _, id := range taskID {
		err := s.Cancel(id)
		assert.Nil(t, err, "failed to cancel task")
	}
	// Wait for Updater goroutine to finish processing all updates. Why?
	//		When all tasks get cancelled at the same time, there is spike
	//		in number of updates from Runner goroutines to updateFunc. This causes
	//		bottleneck in updateFunc as the function needs to acquire lock
	//		before accessing `tasks` map. This bottleneck issue should be
	//		alleviated when I switch to `sync.Map`.
	time.Sleep(2 * time.Second)
	for _, id := range taskID {
		checkStatus(t, s, id, TaskCancelled)
	}
	afterTest(t, "")
}

func Test_MultipleTimedOut(t *testing.T) {
	beforeTest()
	max := 1000
	taskID := make([]string, 0, max)
	for i := 0; i < max; i++ {
		task := &myTask{exec: time.Second, timeout: time.Millisecond}
		id, err := s.Submit(task)
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
	max := 1000
	taskID := make([]string, 0, max)
	for i := 0; i < max; i++ {
		task := &myTask{exec: 2 * time.Second}
		id, err := s.Submit(task)
		assert.Nil(t, err, "failed to submit task")
		taskID = append(taskID, id)
	}
	s.Stop()
	// Wait for Updater goroutine to finish processing all updates.
	time.Sleep(2 * time.Second)
	for _, id := range taskID {
		checkStatus(t, s, id, TaskStopped)
	}
	afterTest(t, "")
}

func checkStatus(t *testing.T, s *Scheduler, id string, expected Status) {
	status, err := s.Status(id)
	assert.Nil(t, err, "failed to get task status")
	assert.Equal(t, expected, status.Status)
	logger.Debug("status: " + (*status).String())
}
