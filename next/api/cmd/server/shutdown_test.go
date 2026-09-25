package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"
)

type lifecycleTestServer struct {
	shutdown  func(context.Context) error
	closeFunc func() error
	closed    chan struct{}
}

func (s *lifecycleTestServer) Shutdown(ctx context.Context) error {
	if s.shutdown != nil {
		return s.shutdown(ctx)
	}
	return nil
}

func (s *lifecycleTestServer) Close() error {
	if s.closeFunc != nil {
		return s.closeFunc()
	}
	if s.closed != nil {
		close(s.closed)
	}
	return nil
}

func TestShutdownBudgetsReserveTheConfiguredTailSafely(t *testing.T) {
	for _, test := range []struct {
		name    string
		total   time.Duration
		drain   time.Duration
		reserve time.Duration
	}{
		{name: "default", total: 30 * time.Second, drain: 28 * time.Second, reserve: 2 * time.Second},
		{name: "short", total: 15 * time.Second, drain: 13*time.Second + 500*time.Millisecond, reserve: 1500 * time.Millisecond},
		{name: "sub-nanosecond reserve", total: time.Nanosecond, drain: time.Nanosecond, reserve: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			drain, reserve := shutdownBudgets(test.total)
			if drain != test.drain || reserve != test.reserve || drain < 0 || reserve < 0 {
				t.Fatalf("shutdownBudgets(%s) = %s + %s", test.total, drain, reserve)
			}
		})
	}
	if drain, reserve := shutdownBudgets(0); drain != 0 || reserve != 0 {
		t.Fatalf("zero budget = %s + %s", drain, reserve)
	}
}

func TestGracefulShutdownDrainsAnInFlightHTTPRequestAndClosesAdmission(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce = make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			select {
			case <-startedOnce:
			default:
				close(startedOnce)
				close(started)
			}
			<-release
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "done")
	})
	server := &http.Server{Handler: handler}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.Serve(listener) }()

	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	requestDone := make(chan struct {
		status int
		err    error
	}, 1)
	go func() {
		response, requestErr := client.Get("http://" + listener.Addr().String() + "/slow")
		if requestErr != nil {
			requestDone <- struct {
				status int
				err    error
			}{err: requestErr}
			return
		}
		_, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		requestDone <- struct {
			status int
			err    error
		}{status: response.StatusCode, err: readErr}
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("in-flight request did not start")
	}

	signals := make(chan os.Signal, 2)
	workDone := make(chan struct{})
	close(workDone)
	stopStarted := make(chan struct{})
	resourceClosed := make(chan struct{})
	status := make(chan int, 1)
	startedAt := time.Now()
	go func() {
		status <- runServerLifecycle(
			server,
			serverErrors,
			signals,
			2*time.Second,
			func(context.Context) { close(stopStarted) },
			workDone,
			func() error { close(resourceClosed); return nil },
			func(os.Signal) {},
		)
	}()
	signals <- syscall.SIGTERM
	<-stopStarted

	admissionClosed := false
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		response, requestErr := client.Get("http://" + listener.Addr().String() + "/new")
		if requestErr != nil {
			admissionClosed = true
			break
		}
		_ = response.Body.Close()
		time.Sleep(5 * time.Millisecond)
	}
	if !admissionClosed {
		t.Fatal("server accepted requests after shutdown began")
	}
	close(release)
	select {
	case result := <-requestDone:
		if result.err != nil || result.status != http.StatusOK {
			t.Fatalf("in-flight request = status %d, error %v", result.status, result.err)
		}
	case <-time.After(time.Second):
		t.Fatal("in-flight request was not drained")
	}
	select {
	case code := <-status:
		if code != 0 {
			t.Fatalf("graceful shutdown status = %d", code)
		}
	case <-time.After(time.Second):
		t.Fatal("graceful shutdown did not finish")
	}
	select {
	case <-resourceClosed:
	case <-time.After(time.Second):
		t.Fatal("dependency was not closed after workers")
	}
	if elapsed := time.Since(startedAt); elapsed >= 2*time.Second {
		t.Fatalf("completed work consumed the full shutdown budget: %s", elapsed)
	}
}

func TestUnexpectedServerExitRunsBoundedCleanup(t *testing.T) {
	server := &lifecycleTestServer{}
	serverErrors := make(chan error, 1)
	serverErrors <- errors.New("listener failed")
	workDone := make(chan struct{})
	close(workDone)
	resourceClosed := make(chan struct{})
	status := runServerLifecycle(server, serverErrors, make(chan os.Signal), 200*time.Millisecond, nil, workDone, func() error {
		close(resourceClosed)
		return nil
	}, func(os.Signal) {})
	if status == 0 {
		t.Fatal("unexpected server exit produced a successful status")
	}
	select {
	case <-resourceClosed:
	default:
		t.Fatal("unexpected server exit skipped dependency cleanup")
	}
}

func TestGracefulShutdownConsumesDeadlineJoinOnlyOnce(t *testing.T) {
	workDone := make(chan struct{})
	close(workDone)
	forcedCloseDone := make(chan struct{})
	resourceClosed := make(chan bool, 1)
	server := &lifecycleTestServer{
		shutdown: func(context.Context) error {
			return context.DeadlineExceeded
		},
		closeFunc: func() error {
			close(forcedCloseDone)
			return nil
		},
	}
	signals := make(chan os.Signal, 1)
	signals <- syscall.SIGTERM
	status := runServerLifecycle(server, make(chan error), signals, 200*time.Millisecond, nil, workDone, func() error {
		select {
		case <-forcedCloseDone:
			resourceClosed <- true
		default:
			resourceClosed <- false
		}
		return nil
	}, func(os.Signal) {})
	if status == 0 {
		t.Fatal("deadline shutdown produced a successful status")
	}
	select {
	case forced := <-resourceClosed:
		if !forced {
			t.Fatal("dependencies closed before forced HTTP close completed")
		}
	default:
		t.Fatal("deadline shutdown skipped dependency cleanup after joining")
	}
}

func TestGracefulShutdownWaitsForForcedCloseBeforeDependencies(t *testing.T) {
	workDone := make(chan struct{})
	close(workDone)
	closeStarted := make(chan struct{})
	releaseClose := make(chan struct{}, 1)
	forceReturned := make(chan struct{})
	resourceClosed := make(chan struct{})
	resourceOrder := make(chan bool, 1)
	defer func() {
		select {
		case releaseClose <- struct{}{}:
		default:
		}
	}()
	server := &lifecycleTestServer{
		shutdown: func(context.Context) error {
			return context.DeadlineExceeded
		},
		closeFunc: func() error {
			close(closeStarted)
			<-releaseClose
			close(forceReturned)
			return nil
		},
	}
	signals := make(chan os.Signal, 1)
	signals <- syscall.SIGTERM
	status := make(chan int, 1)
	go func() {
		status <- runServerLifecycle(server, make(chan error), signals, time.Second, nil, workDone, func() error {
			select {
			case <-forceReturned:
				resourceOrder <- true
			default:
				resourceOrder <- false
			}
			close(resourceClosed)
			return nil
		}, func(os.Signal) {})
	}()
	select {
	case <-closeStarted:
	case <-time.After(time.Second):
		t.Fatal("forced HTTP close did not start")
	}
	select {
	case <-resourceClosed:
		t.Fatal("dependencies closed while server.Close was blocked")
	default:
	}
	releaseClose <- struct{}{}
	select {
	case code := <-status:
		if code == 0 {
			t.Fatal("deadline shutdown produced a successful status")
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown did not finish after forced close completed")
	}
	select {
	case <-resourceClosed:
	case <-time.After(time.Second):
		t.Fatal("dependencies were not closed after forced close")
	}
	select {
	case inOrder := <-resourceOrder:
		if !inOrder {
			t.Fatal("dependencies closed before server.Close completed")
		}
	default:
		t.Fatal("dependency close order was not recorded")
	}
}

func TestGracefulShutdownTimeoutDoesNotWaitForUnresponsiveWork(t *testing.T) {
	workDone := make(chan struct{})
	server := &lifecycleTestServer{shutdown: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	signals := make(chan os.Signal, 2)
	status := make(chan int, 1)
	started := time.Now()
	go func() {
		status <- runServerLifecycle(server, make(chan error), signals, 120*time.Millisecond, nil, workDone, nil, func(os.Signal) {})
	}()
	signals <- syscall.SIGTERM
	select {
	case code := <-status:
		if code == 0 {
			t.Fatal("unresponsive work produced a successful shutdown")
		}
	case <-time.After(time.Second):
		t.Fatal("unresponsive work blocked shutdown")
	}
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("timeout shutdown was not bounded: %s", elapsed)
	}
}

func TestShutdownSignalSubprocess(t *testing.T) {
	if os.Getenv("TEMVIA_SHUTDOWN_HELPER") == "1" {
		runShutdownSignalHelper(t)
		return
	}
	for _, test := range []struct {
		name        string
		mode        string
		total       string
		twoSignals  bool
		wantStatus  int
		waitTimeout time.Duration
		maxElapsed  time.Duration
	}{
		{name: "normal signal", mode: "normal", total: "5s", wantStatus: 0, waitTimeout: 2 * time.Second},
		{name: "second signal", mode: "timeout", total: "5s", twoSignals: true, wantStatus: 128 + int(syscall.SIGTERM), waitTimeout: 2 * time.Second, maxElapsed: time.Second},
		{name: "single signal timeout", mode: "timeout", total: "300ms", wantStatus: 1, waitTimeout: 2 * time.Second, maxElapsed: time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=TestShutdownSignalSubprocess", "-test.v")
			cmd.Env = append(os.Environ(),
				"TEMVIA_SHUTDOWN_HELPER=1",
				"TEMVIA_SHUTDOWN_MODE="+test.mode,
				"TEMVIA_SHUTDOWN_TOTAL="+test.total,
			)
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			cmd.Stderr = cmd.Stdout
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			waitCalled := false
			defer func() {
				if !waitCalled {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
				}
			}()

			events := make(chan string, 128)
			go func() {
				scanner := bufio.NewScanner(stdout)
				for scanner.Scan() {
					events <- scanner.Text()
				}
				close(events)
			}()
			if !waitShutdownHelperEvent(events, "SHUTDOWN_HELPER_READY", time.Second) {
				t.Fatal("shutdown helper did not become ready")
			}
			startedAt := time.Now()
			if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatal(err)
			}
			if !waitShutdownHelperEvent(events, "SHUTDOWN_HELPER_SHUTDOWN_STARTED", time.Second) {
				t.Fatal("shutdown helper did not confirm the first signal")
			}
			if test.twoSignals {
				if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
					t.Fatal(err)
				}
			}

			waitErr, timedOut := waitForShutdownHelper(cmd, test.waitTimeout)
			waitCalled = true
			if timedOut {
				t.Fatalf("shutdown helper did not exit within %s", test.waitTimeout)
			}
			if got := shutdownHelperExitCode(cmd, waitErr); got != test.wantStatus {
				t.Fatalf("shutdown helper exit status = %d, want %d (wait error: %v)", got, test.wantStatus, waitErr)
			}
			if test.maxElapsed > 0 {
				if elapsed := time.Since(startedAt); elapsed >= test.maxElapsed {
					t.Fatalf("shutdown helper did not finish within %s: %s", test.maxElapsed, elapsed)
				}
			}
		})
	}
}

func waitShutdownHelperEvent(events <-chan string, want string, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case line, ok := <-events:
			if !ok {
				return false
			}
			if strings.Contains(line, want) {
				return true
			}
		case <-timer.C:
			return false
		}
	}
}

func waitForShutdownHelper(cmd *exec.Cmd, timeout time.Duration) (error, bool) {
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-done:
		return err, false
	case <-timer.C:
		_ = cmd.Process.Kill()
		return <-done, true
	}
}

func shutdownHelperExitCode(cmd *exec.Cmd, waitErr error) int {
	if waitErr == nil && cmd.ProcessState != nil {
		return cmd.ProcessState.ExitCode()
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) && exitErr.ProcessState != nil {
		return exitErr.ProcessState.ExitCode()
	}
	return -1
}

func runShutdownSignalHelper(t *testing.T) {
	server := &lifecycleTestServer{}
	serverErrors := make(chan error)
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	workDone := make(chan struct{})
	if os.Getenv("TEMVIA_SHUTDOWN_MODE") == "normal" {
		close(workDone)
	}
	total := time.Second
	if value := os.Getenv("TEMVIA_SHUTDOWN_TOTAL"); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid helper shutdown total: %v\n", err)
			os.Exit(2)
		}
		total = parsed
	}
	fmt.Fprintln(os.Stdout, "SHUTDOWN_HELPER_READY")
	stopWork := func(context.Context) {
		fmt.Fprintln(os.Stdout, "SHUTDOWN_HELPER_SHUTDOWN_STARTED")
	}
	os.Exit(runServerLifecycle(server, serverErrors, signals, total, stopWork, workDone, nil, terminateImmediately))
}

func TestShutdownServerUsesRealHTTPTypes(t *testing.T) {
	server := httptest.NewServer(healthHandler())
	defer server.Close()
	response, err := http.Get(server.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d", response.StatusCode)
	}
}
