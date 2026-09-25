package application

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
	"time"
)

type operationLogStoreFake struct {
	createErr         error
	createContexts    []context.Context
	createContextErrs []error
	createCalls       int
	retention         OperationLogRetention
	retentionErr      error
	deleteErr         error
	deleteContexts    []context.Context
	deleteCalls       int
	deleteStarted     chan struct{}
	deleteCompleted   chan struct{}
}

func (s *operationLogStoreFake) CreateOperationLog(ctx context.Context, _ OperationLogInput) error {
	s.createCalls++
	s.createContexts = append(s.createContexts, ctx)
	s.createContextErrs = append(s.createContextErrs, ctx.Err())
	return s.createErr
}

func (*operationLogStoreFake) ListOperationLogs(context.Context, OperationLogListOptions) (OperationLogPage, error) {
	return OperationLogPage{}, nil
}

func (*operationLogStoreFake) FindOperationLog(context.Context, string) (OperationLog, error) {
	return OperationLog{}, nil
}

func (s *operationLogStoreFake) DeleteExpiredOperationLogs(ctx context.Context, _ int) (int64, error) {
	s.deleteCalls++
	s.deleteContexts = append(s.deleteContexts, ctx)
	if s.deleteStarted != nil {
		close(s.deleteStarted)
	}
	if s.deleteCompleted != nil {
		defer close(s.deleteCompleted)
	}
	<-ctx.Done()
	return 0, s.deleteErrOrContext(ctx)
}

func (s *operationLogStoreFake) deleteErrOrContext(ctx context.Context) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	return ctx.Err()
}

func (s *operationLogStoreFake) GetOperationLogRetention(ctx context.Context) (OperationLogRetention, error) {
	if s.retentionErr != nil {
		return OperationLogRetention{}, s.retentionErr
	}
	if err := ctx.Err(); err != nil {
		return OperationLogRetention{}, err
	}
	return s.retention, nil
}

func (*operationLogStoreFake) SaveOperationLogRetention(context.Context, int64, int) (OperationLogRetention, error) {
	return OperationLogRetention{}, nil
}

func validOperationInput() OperationLogInput {
	return OperationLogInput{Action: "roles.create", ObjectType: "role", Result: OperationLogSuccess, OccurredAt: time.Now().UTC()}
}

func TestOperationLogRecordingStatusLifecycleAndSafeDiagnostics(t *testing.T) {
	store := &operationLogStoreFake{createErr: errors.New("secret SQL details")}
	var diagnostics bytes.Buffer
	service := NewOperationLogService(store)
	service.logger = log.New(&diagnostics, "", 0)

	if got := service.RecordingStatus(time.Now()); got.State != OperationLogStatusUnknown {
		t.Fatalf("initial state = %q, want unknown", got.State)
	}
	if err := service.Record(context.Background(), validOperationInput()); err == nil {
		t.Fatal("Record() succeeded through a failing store")
	}
	failedAt := time.Now().UTC()
	if got := service.RecordingStatus(failedAt); got.State != OperationLogStatusFailed || got.FailureCount != 1 {
		t.Fatalf("failed state = %#v", got)
	}
	if strings.Contains(diagnostics.String(), "secret") || !strings.Contains(diagnostics.String(), "storage") {
		t.Fatalf("unsafe or uncategorized recorder diagnostic = %q", diagnostics.String())
	}

	store.createErr = nil
	if err := service.Record(context.Background(), validOperationInput()); err != nil {
		t.Fatalf("Record() recovery error = %v", err)
	}
	if got := service.RecordingStatus(failedAt.Add(time.Hour)); got.State != OperationLogStatusRecovered {
		t.Fatalf("recovered state = %q", got.State)
	}
	if got := service.RecordingStatus(failedAt.Add(25 * time.Hour)); got.State != OperationLogStatusHealthy {
		t.Fatalf("healthy state after recovery window = %q", got.State)
	}

	store.createErr = errors.New("still unavailable")
	if err := service.Record(context.Background(), validOperationInput()); err == nil {
		t.Fatal("second failing Record() succeeded")
	}
	lastFailure := time.Now().UTC()
	if got := service.RecordingStatus(lastFailure.Add(25 * time.Hour)); got.State != OperationLogStatusFailed {
		t.Fatalf("continuous failure state = %q, want failed", got.State)
	}

	if err := service.Record(context.Background(), OperationLogInput{}); err == nil {
		t.Fatal("invalid input was accepted")
	}
	if !strings.Contains(diagnostics.String(), "validation") {
		t.Fatalf("validation failure was not categorized: %q", diagnostics.String())
	}
}

func TestOperationLogRecordUsesIndependentBoundedContext(t *testing.T) {
	store := &operationLogStoreFake{}
	service := NewOperationLogService(store)
	requestContext, cancel := context.WithCancel(context.Background())
	cancel()
	if err := service.Record(requestContext, validOperationInput()); err != nil {
		t.Fatalf("Record() with canceled request context = %v", err)
	}
	if len(store.createContexts) != 1 || store.createContextErrs[0] != nil {
		t.Fatalf("recorder inherited canceled request context: %#v", store.createContexts)
	}
	if _, ok := store.createContexts[0].Deadline(); !ok {
		t.Fatal("recorder context has no bounded deadline")
	}
}

func TestOperationLogCleanupPreservesShutdownCancellationAndLogsTimeout(t *testing.T) {
	store := &operationLogStoreFake{retention: OperationLogRetention{Days: 30}, deleteStarted: make(chan struct{}), deleteCompleted: make(chan struct{})}
	var diagnostics bytes.Buffer
	service := NewOperationLogService(store)
	service.cleanupTimeout = time.Hour
	service.cleanupLogger = log.New(&diagnostics, "", 0)
	ctx, cancel := context.WithCancel(context.Background())
	cleanupDone := make(chan error, 1)
	go func() { cleanupDone <- service.Cleanup(ctx) }()
	<-store.deleteStarted
	cancel()
	if err := <-cleanupDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("Cleanup() error = %v, want canceled", err)
	}
	if !strings.Contains(diagnostics.String(), "canceled") {
		t.Fatalf("cleanup cancellation was not categorized: %q", diagnostics.String())
	}

	timeoutStore := &operationLogStoreFake{retention: OperationLogRetention{Days: 30}, deleteStarted: make(chan struct{}), deleteCompleted: make(chan struct{})}
	var timeoutDiagnostics bytes.Buffer
	timeoutService := NewOperationLogService(timeoutStore)
	timeoutService.cleanupTimeout = time.Millisecond
	timeoutService.cleanupLogger = log.New(&timeoutDiagnostics, "", 0)
	if err := timeoutService.Cleanup(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Cleanup() timeout error = %v", err)
	}
	if !strings.Contains(timeoutDiagnostics.String(), "timeout") {
		t.Fatalf("cleanup timeout was not categorized: %q", timeoutDiagnostics.String())
	}
}
