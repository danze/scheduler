package scheduler

import (
	"context"
	"sync"
	"testing"
)

func TestConcurrency_SubmitPanic(t *testing.T) {
	// This test verifies that submitting a task while the scheduler is stopping
	// does not cause a panic (send on closed channel).
	for i := 0; i < 100; i++ {
		s := New()
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			s.Stop()
		}()

		go func() {
			defer wg.Done()
			_, _ = s.Submit(func(ctx context.Context) (any, error) {
				return nil, nil
			})
		}()

		wg.Wait()
		s.Wait()
	}
}

func TestConcurrency_CircularReference(t *testing.T) {
	// This test verifies that deepCopyAny handles circular references
	// without causing a stack overflow.
	type Node struct {
		Next *Node
	}
	n := &Node{}
	n.Next = n

	copied := deepCopyAny(n).(*Node)
	if copied == n {
		t.Error("deepCopyAny returned the same pointer, expected a copy")
	}
	if copied.Next != copied {
		t.Error("deepCopyAny did not preserve circular reference")
	}
}

func TestConcurrency_UnexportedFields(t *testing.T) {
	// This test documents the behavior of unexported fields in deepCopyAny.
	// Currently, they are not copied (reflect cannot set them).
	type Data struct {
		Public  string
		private string
	}
	d := Data{Public: "hello", private: "world"}
	copied := deepCopyAny(d).(Data)

	if copied.Public != "hello" {
		t.Errorf("Public field not copied: expected hello, got %s", copied.Public)
	}
	if copied.private != "" {
		t.Errorf("Private field was unexpectedly copied: %s", copied.private)
	}
}

func TestConcurrency_WaitOnStop(t *testing.T) {
	// This test verifies that s.Wait() actually waits for all tasks to finish.
	s := New()
	taskStarted := make(chan struct{})
	taskFinished := make(chan struct{})

	s.Submit(func(ctx context.Context) (any, error) {
		close(taskStarted)
		<-ctx.Done()
		// simulate some cleanup
		close(taskFinished)
		return nil, nil
	})

	<-taskStarted
	s.Stop()
	s.Wait()

	select {
	case <-taskFinished:
		// Task finished
	default:
		t.Error("Task did not finish after s.Wait() returned")
	}
}
