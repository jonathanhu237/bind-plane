package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

const (
	OperationLogSuccess = "success"
	OperationLogFailure = "failure"

	OperationLogStatusUnknown   = "unknown"
	OperationLogStatusHealthy   = "healthy"
	OperationLogStatusFailed    = "failed"
	OperationLogStatusRecovered = "recovered"

	DefaultOperationLogRetentionDays = 180
	MinOperationLogRetentionDays     = 1
	MaxOperationLogRetentionDays     = 3650
)

// OperationLogInput is the safe, business-level description of an action. The
// HTTP layer supplies this explicitly; request bodies and passwords are never
// copied into an operation log automatically.
type OperationLogInput struct {
	ActorID          string
	ActorName        string
	ActorEmail       string
	ActorKind        string
	ActorLabel       string
	Action           string
	ObjectType       string
	ObjectID         string
	Result           string
	OccurredAt       time.Time
	SourceIP         string
	AttemptedAccount string
	Details          map[string]any
}

type OperationLog struct {
	ActorDeleted     bool
	ObjectDeleted    bool
	ID               string
	ActorID          string
	ActorName        string
	ActorEmail       string
	ActorKind        string
	ActorLabel       string
	Action           string
	ObjectType       string
	ObjectID         string
	Result           string
	OccurredAt       time.Time
	SourceIP         string
	AttemptedAccount string
	Details          map[string]any
}

type OperationLogListOptions struct {
	Cursor     string
	Limit      int
	From       time.Time
	To         time.Time
	ActorID    string
	Action     string
	ObjectType string
	ObjectID   string
	Result     string
}

type OperationLogPage struct {
	Items      []OperationLog
	NextCursor string
}

type OperationLogRecordingStatus struct {
	State         string     `json:"state"`
	FailureCount  int64      `json:"failureCount"`
	LastFailureAt *time.Time `json:"lastFailureAt,omitempty"`
	LastSuccessAt *time.Time `json:"lastSuccessAt,omitempty"`
}

type OperationLogRetention struct {
	Days      int
	Revision  int64
	UpdatedAt time.Time
}

type OperationLogStore interface {
	CreateOperationLog(context.Context, OperationLogInput) error
	ListOperationLogs(context.Context, OperationLogListOptions) (OperationLogPage, error)
	FindOperationLog(context.Context, string) (OperationLog, error)
	DeleteExpiredOperationLogs(context.Context, int) (int64, error)
	GetOperationLogRetention(context.Context) (OperationLogRetention, error)
	SaveOperationLogRetention(context.Context, int64, int) (OperationLogRetention, error)
}

// OperationLogService owns the best-effort recording boundary and the
// process-local recording status. The recorder deliberately uses a fresh
// bounded context so an HTTP request being canceled cannot leave the write
// without a timeout, while a database failure never changes the business
// operation's response.
type OperationLogService struct {
	store          OperationLogStore
	mu             sync.Mutex
	state          operationLogState
	writeTimeout   time.Duration
	logger         *log.Logger
	cleanupTimeout time.Duration
	cleanupLogger  *log.Logger
}

type operationLogState struct {
	failureCount  int64
	lastFailureAt *time.Time
	lastSuccessAt *time.Time
}

func NewOperationLogService(store OperationLogStore) *OperationLogService {
	return &OperationLogService{store: store, writeTimeout: 2 * time.Second, cleanupTimeout: 2 * time.Second, logger: log.Default(), cleanupLogger: log.Default()}
}

// Record writes one result row and updates the status even when persistence
// fails. Callers should ignore the returned error after the business result is
// decided; it is provided for tests and non-HTTP embedders.
func (s *OperationLogService) Record(_ context.Context, input OperationLogInput) error {
	if s == nil {
		return ErrDependencyUnavailable
	}
	if s.store == nil {
		s.markFailure(time.Now())
		s.logRecorderFailure("dependency_unavailable")
		return ErrDependencyUnavailable
	}
	if err := validateOperationLogInput(input); err != nil {
		s.markFailure(time.Now())
		s.logRecorderFailure("validation")
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.writeTimeout)
	defer cancel()
	err := s.store.CreateOperationLog(ctx, input)
	if err != nil {
		s.markFailure(time.Now())
		s.logRecorderFailure("storage")
		return err
	}
	s.markSuccess(time.Now())
	return nil
}

func (s *OperationLogService) List(ctx context.Context, options OperationLogListOptions) (OperationLogPage, error) {
	if s == nil || s.store == nil {
		return OperationLogPage{}, ErrDependencyUnavailable
	}
	if err := validateOperationLogListOptions(&options); err != nil {
		return OperationLogPage{}, err
	}
	return s.store.ListOperationLogs(ctx, options)
}

func (s *OperationLogService) Detail(ctx context.Context, id string) (OperationLog, error) {
	if s == nil || s.store == nil {
		return OperationLog{}, ErrDependencyUnavailable
	}
	if !domain.IsCanonicalUUID(id) {
		return OperationLog{}, ErrOperationLogNotFound
	}
	return s.store.FindOperationLog(ctx, id)
}

func (s *OperationLogService) RecordingStatus(now time.Time) OperationLogRecordingStatus {
	if now.IsZero() {
		now = time.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := OperationLogStatusUnknown
	switch {
	case s.state.lastFailureAt == nil && s.state.lastSuccessAt == nil:
		state = OperationLogStatusUnknown
	case s.state.lastFailureAt != nil && (s.state.lastSuccessAt == nil || s.state.lastFailureAt.After(*s.state.lastSuccessAt)):
		state = OperationLogStatusFailed
	case s.state.lastFailureAt != nil && now.Before(s.state.lastFailureAt.Add(24*time.Hour)):
		state = OperationLogStatusRecovered
	default:
		state = OperationLogStatusHealthy
	}
	return OperationLogRecordingStatus{State: state, FailureCount: s.state.failureCount, LastFailureAt: cloneTime(s.state.lastFailureAt), LastSuccessAt: cloneTime(s.state.lastSuccessAt)}
}

func (s *OperationLogService) Retention(ctx context.Context) (OperationLogRetention, error) {
	if s == nil || s.store == nil {
		return OperationLogRetention{}, ErrDependencyUnavailable
	}
	retention, err := s.store.GetOperationLogRetention(ctx)
	if errors.Is(err, ErrOperationLogRetentionNotFound) {
		return OperationLogRetention{Days: DefaultOperationLogRetentionDays}, nil
	}
	return retention, err
}

func (s *OperationLogService) SaveRetention(ctx context.Context, expectedRevision int64, days int) (OperationLogRetention, error) {
	if s == nil || s.store == nil {
		return OperationLogRetention{}, ErrDependencyUnavailable
	}
	if days < MinOperationLogRetentionDays || days > MaxOperationLogRetentionDays {
		return OperationLogRetention{}, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "retentionDays", Code: "invalid_retention_days"}}}
	}
	return s.store.SaveOperationLogRetention(ctx, expectedRevision, days)
}

func (s *OperationLogService) Cleanup(ctx context.Context) error {
	if s == nil || s.store == nil {
		return ErrDependencyUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := s.cleanupTimeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	cleanupContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	retention, err := s.Retention(cleanupContext)
	if err != nil {
		s.logCleanupFailure(err)
		return err
	}
	_, err = s.store.DeleteExpiredOperationLogs(cleanupContext, retention.Days)
	if err != nil {
		s.logCleanupFailure(err)
	}
	return err
}

func (s *OperationLogService) RunCleanup(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Hour
	}
	_ = s.Cleanup(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.Cleanup(ctx)
		}
	}
}

func (s *OperationLogService) logCleanupFailure(err error) {
	if s != nil && s.cleanupLogger != nil && err != nil {
		// Keep cleanup diagnostics categorized and free of SQL statements,
		// connection strings, and operation details.
		s.cleanupLogger.Printf("operation log cleanup failed: %s", operationLogErrorCategory(err))
	}
}

func (s *OperationLogService) logRecorderFailure(category string) {
	if s != nil && s.logger != nil {
		// Keep diagnostics useful for operators while excluding operation fields,
		// request bodies, SQL, and connection strings.
		s.logger.Printf("operation log recorder failed: %s", category)
	}
}

func operationLogErrorCategory(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, ErrOperationLogRetentionNotFound):
		return "retention_missing"
	case errors.Is(err, ErrDependencyUnavailable):
		return "dependency_unavailable"
	default:
		return "storage_error"
	}
}

func (s *OperationLogService) markFailure(at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.failureCount++
	s.state.lastFailureAt = cloneTime(&at)
}

func (s *OperationLogService) markSuccess(at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.lastSuccessAt = cloneTime(&at)
}

func validateOperationLogInput(input OperationLogInput) error {
	if input.Action == "" || input.ObjectType == "" || (input.Result != OperationLogSuccess && input.Result != OperationLogFailure) {
		return fmt.Errorf("invalid operation log input")
	}
	if input.OccurredAt.IsZero() {
		input.OccurredAt = time.Now()
	}
	if input.Details != nil {
		if _, err := json.Marshal(input.Details); err != nil {
			return fmt.Errorf("invalid operation log details: %w", err)
		}
	}
	return nil
}

func validateOperationLogListOptions(options *OperationLogListOptions) error {
	if options.Limit == 0 {
		options.Limit = 25
	}
	if options.Limit < 1 || options.Limit > 100 {
		return &domain.ValidationErrors{Items: []domain.FieldError{{Field: "limit", Code: "invalid_limit"}}}
	}
	if options.Result != "" && options.Result != OperationLogSuccess && options.Result != OperationLogFailure {
		return &domain.ValidationErrors{Items: []domain.FieldError{{Field: "result", Code: "invalid_result"}}}
	}
	return nil
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := value.UTC()
	return &copy
}
