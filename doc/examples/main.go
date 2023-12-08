package main

import (
	"fmt"
	"github.com/danze/scheduler"
	"os"
	"time"
)

func main() {
	exampleCompletedTask()
	exampleCanceledTask()
	exampleTimedOutTask()
	exampleStoppedTask()
}

func exampleCompletedTask() {
	s := scheduler.New()
	id, err := s.Submit(func() (any, error) {
		time.Sleep(time.Second)
		return "Result is 42", nil
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
	fmt.Println((*status).StringDetailed())
}

func exampleCanceledTask() {
	s := scheduler.New()
	id, err := s.Submit(func() (any, error) {
		time.Sleep(time.Second)
		return "Result is 42", nil
	})
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

func exampleTimedOutTask() {
	s := scheduler.New()
	myTask := func() (any, error) {
		time.Sleep(2 * time.Second)
		return "Result is 42", nil
	}
	myTaskTimeout := time.Second
	id, err := s.SubmitWithTimeout(myTask, myTaskTimeout)
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

func exampleStoppedTask() {
	s := scheduler.New()
	var taskIDs []string
	for x := 0; x < 10; x++ {
		id, err := s.Submit(func() (any, error) {
			time.Sleep(5 * time.Second)
			return "Result is 42", nil
		})
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
		fmt.Println((*status).StringDetailed())
	}
}
