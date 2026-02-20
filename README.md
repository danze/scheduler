# scheduler

![Build and test](https://github.com/danze/scheduler/actions/workflows/go.yml/badge.svg?branch=main)

Package `scheduler` provides a concurrent task scheduler with support for cancellation and timeouts. A `Task` is defined as a user-submitted function executed by the scheduler.

```go
type Task func(context.Context) (any, error)
```

Tasks receive a `context.Context` that is canceled when the task is explicitly canceled, expires due to a timeout, or when the scheduler is stopped. Tasks should monitor `ctx.Done()` and return early to prevent goroutine leaks.

### Examples

```go
func exampleCanceledTask() {
	s := scheduler.New()
	id, err := s.Submit(func(ctx context.Context) (any, error) {
		// Example of a cancellation-aware task
		select {
		case <-time.After(time.Second):
			return "Result is 42", nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	if err != nil {
		fmt.Printf("failed to submit task: %v\n", err)
		os.Exit(1)
	}
	err = s.Cancel(id)
	if err != nil {
		fmt.Printf("failed to cancel task: %v\n", err)
		os.Exit(1)
	}
	// Give some time for the cancellation to be processed
	time.Sleep(100 * time.Millisecond)
	status, err := s.Status(id)
	if err != nil {
		fmt.Printf("failed to get task status: %v\n", err)
		os.Exit(1)
	}
	fmt.Println((*status).StringDetailed())
	s.Stop()
}
```

For more details, see the [examples](doc/examples/main.go).

### Implementation

The following conceptual diagram illustrates the internal architecture of the `Scheduler`.

![](doc/diagram1.svg)

The `Scheduler` provides APIs to submit tasks for execution, retrieve task status, cancel tasks, and stop the scheduler itself. When the scheduler is stopped, all non-completed tasks are aborted.

#### Key Features
- **Thread Safety**: All methods are safe for concurrent use.
- **Deep Copying**: `Status()` returns a deep copy of task results, preventing data races when accessing results.
- **Robust Shutdown**: `Stop()` blocks until all internal status updates are processed and system goroutines have exited.
- **Circular Reference Support**: The internal deep-copy logic correctly handles complex data structures with circular references.

#### Internal State
A `Scheduler` maintains the following internal variables:

- **result**: A Go channel used to transmit task status messages from **Runners** to the scheduler.
- **stopped**: A Go channel used to broadcast a stop signal to all active Runners and the Closer goroutine.
- **tasks**: A map used to store task IDs and their corresponding status records.
- **wg**: A `sync.WaitGroup` used to track active **Runners** and ensure they exit during shutdown.
- **sysWg**: A `sync.WaitGroup` used to coordinate the shutdown of system goroutines (**Closer** and **Updater**).
- **mu**: A `sync.Mutex` used to synchronize access to the `tasks` map and the internal state.

#### Goroutines
The `Scheduler` manages several types of goroutines:

- **Runner**: Created for each new task. It executes the task, monitors for cancellation, timeout, or stop signals, and reports results back to the scheduler upon completion.
  - The Runner executes the task within a separate **Executor** goroutine.
  - While active, the Runner listens for messages from the following channels:
    - **taskChan**: A task-specific channel used by an Executor to send output back to the Runner.
    - **cancel**: A task-specific channel used to notify the Runner to abort the task.
    - **stopped**: A global channel used to notify all active goroutines that the scheduler is shutting down.
    - **timer.C**: A task-specific timer that fires when the task's timeout expires.

  The Runner sends two status updates to the **Updater**: a `Running` status immediately after starting, and a final status (`Completed`, `Cancelled`, `TimedOut`, or `Stopped`) upon termination.

  **Note**: If a task terminates with a status other than `Completed`, the Runner exits immediately. However, the associated **Executor** will continue running until the user task returns. Since the task's context is canceled, tasks that monitor `ctx.Done()` will exit promptly. Tasks that ignore the context may cause goroutine leaks.

- **Executor**: Created by the Runner to execute the user-submitted task. It reports its output back to the Runner upon completion.
- **Updater**: Initialized alongside the scheduler, this goroutine remains active until the scheduler is stopped. It consumes task status messages from the `result` channel and updates the internal `tasks` map. During shutdown, it drains all remaining messages from the channel before terminating.
- **Closer**: Initialized alongside the scheduler, this goroutine manages the shutdown sequence. When the scheduler is stopped, it waits for all Runners to finish and then closes the `result` channel to signal the Updater to exit.

#### Workflow Operations

##### Scheduling a Task
When a user submits a task, it is assigned a unique ID and its status is set to `Scheduled`. The scheduler then immediately begins execution by launching a new **Runner** goroutine.

##### Canceling a Task
When a user cancels a task, the scheduler closes the task's `cancel` channel, signaling the associated **Runner** to abort. The Runner then updates the task's status to `Cancelled` and terminates. Canceling a task that has already finished has no effect.

##### Task Timeout
If a task's timeout duration expires before completion, the associated **Runner** updates the status to `TimedOut` and terminates.

##### Stopping the Scheduler
Upon stopping the scheduler, all active goroutines receive a stop signal. Runners send a `Stopped` status message and exit immediately. The Closer then closes the `result` channel, allowing the Updater to process any pending messages before terminating.

The `Stop()` method **blocks** until this entire process is complete, ensuring that the scheduler's internal state is fully finalized before it returns.

![](doc/diagram2.svg)
