package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"syscall"
	"time"
)

const shutdownExitReserve = 2 * time.Second

type shutdownHTTPServer interface {
	Shutdown(context.Context) error
	Close() error
}

// shutdownReserve keeps a bounded tail of the configured total for canceling
// work, closing dependencies, and returning from the process. The subtraction
// is intentionally used instead of multiplying the duration, so positive
// short budgets cannot become negative or overflow.
func shutdownReserve(total time.Duration) time.Duration {
	if total <= 0 {
		return 0
	}
	reserve := total / 10
	if reserve > shutdownExitReserve {
		return shutdownExitReserve
	}
	return reserve
}

func shutdownBudgets(total time.Duration) (drain, reserve time.Duration) {
	if total <= 0 {
		return 0, 0
	}
	reserve = shutdownReserve(total)
	return total - reserve, reserve
}

// runServerLifecycle waits for the first termination/server-stop event, then
// drains HTTP, mail, and background work against one deadline. stopWork must
// cancel background loops and tell the mail dispatcher to stop claiming; the
// dispatcher receives the drain context so an already claimed job is not
// interrupted until the shared drain deadline.
func runServerLifecycle(
	server shutdownHTTPServer,
	serverErrors <-chan error,
	signals <-chan os.Signal,
	total time.Duration,
	stopWork func(context.Context),
	workDone <-chan struct{},
	closeResource func() error,
	terminate func(os.Signal),
) int {
	if server == nil || total <= 0 {
		return 1
	}

	var serverStartErr error
	select {
	case signal := <-signals:
		log.Printf("shutdown signal received: %s", signalName(signal))
	case serverStartErr = <-serverErrors:
		if serverStartErr == nil {
			serverStartErr = errors.New("HTTP server stopped unexpectedly")
		}
		log.Printf("API server stopped unexpectedly: %v", serverStartErr)
	}

	started := time.Now()
	drainBudget, reserve := shutdownBudgets(total)
	totalDeadline := started.Add(total)
	drainDeadline := started.Add(drainBudget)
	totalContext, cancelTotal := context.WithDeadline(context.Background(), totalDeadline)
	defer cancelTotal()
	drainContext, cancelDrain := context.WithDeadline(context.Background(), drainDeadline)
	defer cancelDrain()
	log.Printf("graceful shutdown started with %s total budget (%s drain, %s exit reserve)", total, drainBudget, reserve)

	if stopWork != nil {
		stopWork(drainContext)
	}

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Shutdown(drainContext)
	}()
	joinResult := make(chan error, 1)
	go func() {
		if workDone != nil {
			<-workDone
		}
		joinResult <- <-serverDone
	}()

	status := 0
	var serverShutdownErr error
	waitedPastDrain := false
	// joinResult is one-shot; retain the joined state when forcing close so
	// already-completed work is not awaited a second time.
	joined := false
	select {
	case serverShutdownErr = <-joinResult:
		joined = true
		joinResult = nil
		if !time.Now().Before(drainDeadline) || drainContext.Err() != nil {
			waitedPastDrain = true
		}
	case <-drainContext.Done():
		waitedPastDrain = true
	case signal := <-signals:
		return immediateShutdown(terminate, signal)
	}

	if serverStartErr != nil {
		status = 1
	}
	if serverShutdownErr != nil {
		if errors.Is(serverShutdownErr, context.DeadlineExceeded) {
			waitedPastDrain = true
		} else {
			status = 1
			log.Printf("HTTP server shutdown failed: %v", serverShutdownErr)
		}
	}

	if waitedPastDrain {
		status = 1
		log.Printf("graceful shutdown drain budget exhausted; forcing HTTP connections closed")
		forceDone := make(chan error, 1)
		go func() { forceDone <- server.Close() }()
		forceClosed := false
		for !joined || !forceClosed {
			select {
			case err := <-forceDone:
				if err != nil && !errors.Is(err, http.ErrServerClosed) {
					log.Printf("forcing HTTP connections closed failed: %v", err)
				}
				forceClosed = true
			case serverShutdownErr = <-joinResult:
				joined = true
				joinResult = nil
			case <-totalContext.Done():
				log.Printf("graceful shutdown timed out after %s", total)
				return 1
			case signal := <-signals:
				return immediateShutdown(terminate, signal)
			}
		}
	}

	if status != 0 && serverShutdownErr != nil && !errors.Is(serverShutdownErr, context.DeadlineExceeded) {
		log.Printf("shutdown completed with HTTP error: %v", serverShutdownErr)
	}
	if closeResource == nil {
		return status
	}
	if totalContext.Err() != nil {
		log.Printf("shutdown timed out before dependencies could close")
		return 1
	}
	closeDone := make(chan error, 1)
	go func() { closeDone <- closeResource() }()
	select {
	case err := <-closeDone:
		if err != nil {
			log.Printf("dependency close failed: %v", err)
			return 1
		}
		return status
	case <-totalContext.Done():
		log.Printf("shutdown timed out while closing dependencies")
		return 1
	case signal := <-signals:
		return immediateShutdown(terminate, signal)
	}
}

func immediateShutdown(terminate func(os.Signal), signal os.Signal) int {
	log.Printf("second termination signal received: %s; terminating immediately", signalName(signal))
	if terminate != nil {
		terminate(signal)
	}
	return 1
}

func terminateImmediately(signal os.Signal) {
	status := 1
	if value, ok := signal.(syscall.Signal); ok {
		status = 128 + int(value)
	}
	os.Exit(status)
}

func signalName(signal os.Signal) string {
	if signal == nil {
		return "unknown"
	}
	return fmt.Sprint(signal)
}
