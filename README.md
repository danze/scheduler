# scheduler

![Build and test](https://github.com/danze/scheduler/actions/workflows/go.yml/badge.svg?branch=main)

Package scheduler provides concurrent task scheduler with support for
cancellation and timeout. `Task` defines a user submitted task
executed by this scheduler.

```go
type Task func() (any, error)
```

### Examples

```go
func exampleCompletedTask() {
	s := scheduler.New()
	id, err := s.Submit(func () (any, error) {
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
```

For more, see [examples](doc/examples/main.go).

### Implementation

Conceptual diagram showing how a `Scheduler` works internally.

![](doc/diagram1.svg)

A `Scheduler` provides APIs to submit task for execution, get task
status, cancel task, and stop the scheduler itself. If the scheduler
is stopped by the user, all non-complete tasks are stopped as well.

Internal variables maintained by a `Scheduler`:

- **result**: a Go channel used to send task status messages from
  **Runners** to the scheduler.
- **stopped**: a Go channel used to send stop signal from the scheduler
  to all active Runners and the Closer goroutine. This happens when
  the user stops the scheduler.
- **tasks**: a Go map type used to store task ID and latest status.
- **wg**: a `sync.WaitGroup` used to wait for all active **Runners**
  to exit when this scheduler is stopping.
- **mu**: a `sync.Mutex` used to control shared access to `tasks` map.

Goroutines maintained by a `Scheduler`:

- **Runner**: created by the scheduler for each new task. Its job is
  to run the given task, listen for cancel, timeout, and stop signals,
  and when the task is complete, to report back output to
  the scheduler. **Runner** runs the task in a separate **Executor**
  goroutine. While the task is running, **Runner** waits for a message
  from one of these channels:
    - **taskChan**: Task specific channel used by an **Executor** to send
      task output to a **Runner**.
    - **cancel**: Task specific cancel channel used by the scheduler to
      notify a **Runner** to cancel a task.
    - **stopped**: global channel used by the scheduler to notify all
      active goroutines that the scheduler is shutting down, and thus
      they need to exit.
    - **timer.C**: Task specific timer that fires when the task's
      timeout elapse.

  **Runner** sends two status messages to **Updater** goroutine.
  Task `Running` message is sent immediately after the **Runner**
  starts up. Then a second and final message is sent when the
  task completes, and this could be one of `Completed`,
  `Canceled`, `TimedOut`, or `Stopped` (see above diagram).

  Note that if a task finishes with one of `Canceled`, `TimedOut`, or
  `Stopped` status, the associated **Executor** goroutine is left running
  behind when the **Runner** exits. The **Executor** left behind exits
  when the user task returns. Therefore, the user
  should avoid blocking forever to prevent goroutine leak.

- **Executor**: created by **Runner** to run a task. After the task is
  complete, **Executor** reports output back to **Runner**. And
  **Runner** reports output back to **Updater**.
- **Updater**: created when a scheduler is initialized, and it remains
  active until the scheduler is stopped. **Updater** reads task status
  messages from `result` channel that are sent by active **Runners**.
  Then it updates the `tasks` map in the scheduler. If the scheduler
  is stopped, **Updater** reads all remaining messages in `result`
  before exiting.
- **Closer**: created when a scheduler is initialized, and it remains
  active until the scheduler is stopped. If the scheduler is stopped,
  all active **Runners** will exit immediately. The job of **Closer** is
  to wait for all **Runners** to exit and then to signal **Updater** to
  exit as well by closing the `result` channel.

#### Schedule a task

When a task is submitted by a user, it's assigned an ID and the status
is set to `Scheduled`. Then the scheduler immediately starts to run the
task by launching a new **Runner** goroutine.

#### Cancel a task

If a user cancels a task, the scheduler closes the task’s `cancel`
channel to signal to the associated **Runner** that it needs to cancel the task.
Then the **Runner** updates the task status to `Canceled` and exits. If the task
is already complete, canceling it has no effect.

#### Task timeout

If the timeout duration of a task elapses before the task completes,
the associated **Runner** updates the task status to `TimedOut` and exits.

#### Stop a scheduler

When the scheduler is stopped, all active goroutines receive a stop signal.
All **Runners** send a `Stopped` status message and exit immediately. Then
**Closer** closes `result` channel and this allows **Updater** to exit
after reading all messages from the channel.

![](doc/diagram2.svg)
