package main

import (
	"fmt"
	"github.com/danze/scheduler"
	"os"
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

func main() {
	example1()
	//example2()
	//example3()
	//example4()
}

func example1() {
	s := scheduler.New()
	id, err := s.Submit(&myTask{exec: time.Second})
	if err != nil {
		fmt.Printf("failed to submit task: %v", err)
		os.Exit(1)
	}
	status, err := s.Status(id)
	for err == nil && !status.Completed() {
		time.Sleep(100 * time.Millisecond)
		status, err = s.Status(id)
	}
	if err != nil {
		fmt.Printf("failed to get task status: %v", err)
		os.Exit(1)
	}
	fmt.Println((*status).StringDetailed())
}

func example2() {
	s := scheduler.New()
	id, err := s.Submit(&myTask{exec: time.Second})
	if err != nil {
		fmt.Printf("failed to submit task: %v", err)
		os.Exit(1)
	}
	err = s.Cancel(id)
	if err != nil {
		fmt.Printf("failed to cancel task: %v", err)
		os.Exit(1)
	}
	time.Sleep(time.Second)
	status, err := s.Status(id)
	if err != nil {
		fmt.Printf("failed to get task status: %v", err)
		os.Exit(1)
	}
	fmt.Println((*status).StringDetailed())
}

func example3() {
	s := scheduler.New()
	id, err := s.Submit(&myTask{exec: 2 * time.Second, timeout: time.Second})
	if err != nil {
		fmt.Printf("failed to submit task: %v", err)
		os.Exit(1)
	}
	status, err := s.Status(id)
	for err == nil && !status.Completed() {
		time.Sleep(100 * time.Millisecond)
		status, err = s.Status(id)
	}
	if err != nil {
		fmt.Printf("failed to get task status: %v", err)
		os.Exit(1)
	}
	fmt.Println((*status).StringDetailed())
}

func example4() {
	s := scheduler.New()
	var taskIDs []string
	for x := 0; x < 10; x++ {
		id, err := s.Submit(&myTask{exec: 5 * time.Second})
		if err != nil {
			fmt.Printf("failed to submit task: %v", err)
			os.Exit(1)
		}
		taskIDs = append(taskIDs, id)
	}
	s.Stop()
	time.Sleep(time.Second)
	for _, id := range taskIDs {
		status, err := s.Status(id)
		if err != nil {
			fmt.Printf("failed to submit task: %v", err)
			os.Exit(1)
		}
		if !status.Completed() {
			fmt.Printf("scheduler failed to stop task: %v", id)
			os.Exit(1)
		}
		fmt.Println((*status).StringDetailed())
	}
}
