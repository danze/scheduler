package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/danze/scheduler"
)

func main() {
	exampleCompletedTask()
	exampleCompletedWithPanicTask()
	exampleCanceledTask()
	exampleTimedOutTask()
	exampleStoppedTask()
}

var myTask = func(ctx context.Context) (any, error) {
	select {
	case <-time.After(time.Second):
		return "Result is 42", nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func exampleCompletedTask() {
	s := scheduler.New()
	id, err := s.Submit(myTask)
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
	fmt.Println(status)
}

func exampleCompletedWithPanicTask() {
	s := scheduler.New()
	id, err := s.Submit(func(ctx context.Context) (any, error) {
		panic("invalid state")
	})
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
	fmt.Println(status)
}

func exampleCanceledTask() {
	s := scheduler.New()
	id, err := s.Submit(myTask)
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
	fmt.Println(status)
}

func exampleTimedOutTask() {
	s := scheduler.New()
	timeout := time.Millisecond
	id, err := s.SubmitWithTimeout(myTask, timeout)
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
	fmt.Println(status)
}

func exampleStoppedTask() {
	s := scheduler.New()
	var taskIDs []string
	for x := 0; x < 3; x++ {
		id, err := s.Submit(myTask)
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
			fmt.Printf("failed to get task status: %v", err)
			os.Exit(1)
		}
		if !status.Completed() {
			fmt.Printf("scheduler failed to stop task: %v", id)
			os.Exit(1)
		}
		fmt.Println(status)
	}
}
