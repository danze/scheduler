// Package main demonstrates using the scheduler library to implement a
// concurrent microservices health checker.
//
// It spins up a local HTTP server simulating five microservices, then
// concurrently checks each endpoint with a per-request timeout. If a
// critical service reports an unhealthy status, all remaining in-flight
// checks are immediately cancelled. A SIGINT/SIGTERM triggers a graceful
// shutdown via the scheduler's Stop mechanism.
//
// Expected output:
//
//	[OK]      Notification Service        200  80ms
//	[OK]      User Service                200  100ms
//	[FAIL]    Payment Service             503  151ms
//
//	Critical service "Payment Service" is down — cancelling remaining 2 check(s).
//
//	[CANCEL]  Order Service               cancelled
//	[CANCEL]  Inventory Service           cancelled
//
//	--- Summary ---
//	  Passed:    2
//	  Failed:    1
//	  Cancelled: 2
//	  TimedOut:  0
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/danze/scheduler"
)

type endpoint struct {
	name     string
	path     string
	critical bool
}

// services is the list of microservice endpoints to health-check.
// Payment Service is marked critical: an unhealthy response from it will
// cancel all remaining checks.
var services = []endpoint{
	{name: "User Service", path: "/api/users", critical: false},
	{name: "Order Service", path: "/api/orders", critical: false},
	{name: "Payment Service", path: "/api/payments", critical: true},
	{name: "Inventory Service", path: "/api/inventory", critical: false},
	{name: "Notification Service", path: "/api/notifications", critical: false},
}

// checkResult holds the outcome of a single health check.
// Fields must be exported so the scheduler's deep copy can copy them via reflect.
type checkResult struct {
	Service    string
	StatusCode int
	Latency    time.Duration
}

// startServices returns a local HTTP server simulating the five microservices:
//   - users, orders, and notifications respond 200 OK (healthy)
//   - payments responds 503 (simulating an outage — critical)
//   - inventory hangs indefinitely (simulating a hung service — will time out or be cancelled)
func startServices() *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/users", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/orders", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/payments", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	mux.HandleFunc("/api/inventory", func(w http.ResponseWriter, r *http.Request) {
		// Hangs until the client disconnects (context cancelled) or 30 s elapses.
		select {
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
			w.WriteHeader(http.StatusOK)
		}
	})
	mux.HandleFunc("/api/notifications", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(80 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})

	return httptest.NewServer(mux)
}

func healthCheck(service, url string) scheduler.Task {
	return func(ctx context.Context) (any, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("building request: %w", err)
		}
		start := time.Now()
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request failed: %w", err)
		}
		resp.Body.Close()
		return checkResult{
			Service:    service,
			StatusCode: resp.StatusCode,
			Latency:    time.Since(start),
		}, nil
	}
}

func main() {
	srv := startServices()
	defer srv.Close()

	const checkTimeout = 2 * time.Second

	s := scheduler.New()

	// Cancel all in-flight checks and drain on SIGINT / SIGTERM.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-quit
		fmt.Println("\nInterrupted — stopping scheduler...")
		s.Stop()
	}()

	type entry struct {
		id       string
		endpoint endpoint
	}

	fmt.Printf("Checking %d services (timeout %s each)...\n\n", len(services), checkTimeout)

	var tasks []entry
	for _, ep := range services {
		id, err := s.SubmitWithTimeout(healthCheck(ep.name, srv.URL+ep.path), checkTimeout)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to schedule %s: %v\n", ep.name, err)
			continue
		}
		tasks = append(tasks, entry{id, ep})
	}

	byID := make(map[string]entry, len(tasks))
	pending := make(map[string]struct{}, len(tasks))
	for _, t := range tasks {
		byID[t.id] = t
		pending[t.id] = struct{}{}
	}

	var passed, failed, cancelled, timedOut, stopped int

	for len(pending) > 0 {
		for id := range pending {
			st, err := s.Status(id)
			if err != nil || !st.Completed() {
				continue
			}

			te := byID[id]
			delete(pending, id)
			_ = s.Remove(id)

			switch st.Status {
			case scheduler.TaskCompleted:
				if st.Err != nil {
					failed++
					fmt.Printf("  [ERROR]   %-26s  %v\n", te.endpoint.name, st.Err)
					break
				}
				r := st.Output.(checkResult)
				if r.StatusCode >= 200 && r.StatusCode < 300 {
					passed++
					fmt.Printf("  [OK]      %-26s  %d  %s\n", r.Service, r.StatusCode, r.Latency.Round(time.Millisecond))
				} else {
					failed++
					fmt.Printf("  [FAIL]    %-26s  %d  %s\n", r.Service, r.StatusCode, r.Latency.Round(time.Millisecond))
					if te.endpoint.critical {
						fmt.Printf("\n  Critical service %q is down — cancelling remaining %d check(s).\n\n",
							te.endpoint.name, len(pending))
						for remainingID := range pending {
							_ = s.Cancel(remainingID)
						}
					}
				}
			case scheduler.TaskTimedOut:
				timedOut++
				fmt.Printf("  [TIMEOUT] %-26s  timed out after %s\n", te.endpoint.name, checkTimeout)
			case scheduler.TaskCancelled:
				cancelled++
				fmt.Printf("  [CANCEL]  %-26s  cancelled\n", te.endpoint.name)
			case scheduler.TaskStopped:
				stopped++
				fmt.Printf("  [STOPPED] %-26s  stopped\n", te.endpoint.name)
			}
		}
		if len(pending) > 0 {
			time.Sleep(50 * time.Millisecond)
		}
	}

	fmt.Println("\n--- Summary ---")
	fmt.Printf("  Passed:    %d\n", passed)
	fmt.Printf("  Failed:    %d\n", failed)
	fmt.Printf("  Cancelled: %d\n", cancelled)
	fmt.Printf("  TimedOut:  %d\n", timedOut)
	if stopped > 0 {
		fmt.Printf("  Stopped:   %d\n", stopped)
	}

	if failed > 0 || timedOut > 0 {
		os.Exit(1)
	}
}
