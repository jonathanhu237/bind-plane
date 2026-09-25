package application

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

const (
	MailTaskStatusQueued       = "queued"
	MailTaskStatusSending      = "sending"
	MailTaskStatusWaitingRetry = "waiting_retry"
	MailTaskStatusSent         = "sent"
	MailTaskStatusFailed       = "failed"

	MailTaskOutcomeSent   = "sent"
	MailTaskOutcomeFailed = "failed"

	MailTaskDefaultRetryCount    = 9
	MailTaskMinRetryCount        = 0
	MailTaskMaxRetryCount        = 100
	MailTaskDefaultRetentionDays = 30
	MailTaskMinRetentionDays     = 1
	MailTaskMaxRetentionDays     = 3650

	MailTaskDefaultPageSize = 25
	MailTaskMaxPageSize     = 100
	MailTaskMaxBatchSize    = 100
)

// MailKind identifies the business purpose of a durable mail task. The values
// are part of the API contract; a task's purpose never changes after creation.
func (k MailKind) Valid() bool {
	switch k {
	case MailPasswordReset, MailPasswordChanged, MailUserInvitation, MailEmailChangeCode, MailEmailChanged, MailTest:
		return true
	default:
		return false
	}
}

func (k MailKind) IsCredentialMail() bool {
	switch k {
	case MailPasswordReset, MailUserInvitation, MailEmailChangeCode:
		return true
	default:
		return false
	}
}

func (k MailKind) IsNotificationMail() bool {
	switch k {
	case MailPasswordChanged, MailEmailChanged, MailTest:
		return true
	default:
		return false
	}
}

type MailTaskAttempt struct {
	ID         string
	Round      int
	Attempt    int
	Outcome    string
	ErrorCode  string
	OccurredAt time.Time
}

type MailTask struct {
	ID                string
	Kind              MailKind
	RecipientEmail    string
	RecipientName     string
	Locale            domain.Locale
	Status            string
	CreatedAt         time.Time
	AvailableAt       time.Time
	FinishedAt        time.Time
	AttemptCount      int
	Round             int
	RoundAttemptCount int
	LastErrorCode     string
	Attempts          []MailTaskAttempt
}

type MailTaskListOptions struct {
	Cursor     string
	Recipient  string
	Kind       MailKind
	Status     string
	From       time.Time
	To         time.Time
	FailedOnly bool
	Limit      int
}

type MailTaskPage struct {
	Items      []MailTask
	NextCursor string
}

// MailTaskInput is the write-side projection used by business workflows and
// the settings test-mail path. Message is optional for credential-bearing
// business messages: those messages are reconstructed from the encrypted
// stable material, never from mutable account or invitation rows.
type MailTaskInput struct {
	Kind                 MailKind
	UserID               string
	InvitationID         string
	EmailChangeRequestID string
	RecipientEmail       string
	RecipientName        string
	Locale               domain.Locale
	SystemName           string
	ChangedAt            time.Time
	CreatedAt            time.Time
	ExpiresAt            time.Time
	SubmittedBy          string
	Message              *OutgoingMail
	ResetSelector        []byte
	EmailSelector        []byte
	VerifierDigest       []byte
}

// MailTaskMaterial is encrypted before persistence. It contains either a
// complete rendered message (test messages use this form) or the immutable
// inputs needed to render a business message. It is never returned by an API
// or copied to operation history.
type MailTaskMaterial struct {
	Version        int           `json:"version"`
	Kind           MailKind      `json:"kind"`
	Name           string        `json:"name,omitempty"`
	Email          string        `json:"email,omitempty"`
	Locale         domain.Locale `json:"locale,omitempty"`
	SystemName     string        `json:"systemName,omitempty"`
	CreatedAt      time.Time     `json:"createdAt"`
	ExpiresAt      time.Time     `json:"expiresAt"`
	ResetSelector  []byte        `json:"resetSelector,omitempty"`
	EmailSelector  []byte        `json:"emailSelector,omitempty"`
	VerifierDigest []byte        `json:"verifierDigest,omitempty"`
	Message        *OutgoingMail `json:"message,omitempty"`
}

// MailTaskStore is the application boundary for management operations. A
// PostgreSQL implementation performs authorization-sensitive state changes
// atomically under row locks; the service only coordinates principal checks
// and bounded input validation.
type MailTaskStore interface {
	ListMailTasks(context.Context, MailTaskListOptions) (MailTaskPage, error)
	FindMailTask(context.Context, string) (MailTask, error)
	RetryMailTask(context.Context, string) (MailTask, error)
	DeleteMailTask(context.Context, string) error
	EnqueueMailTask(context.Context, MailTaskInput) (MailTask, error)
}

// MailTaskOwnerStore is an intentionally narrow status seam for the settings
// page. It allows the actor who submitted a test task to observe that task's
// small safe projection without granting list/detail access to all mail.
type MailTaskOwnerStore interface {
	FindMailTaskForSubmitter(context.Context, string, string) (MailTask, error)
}

// MailTaskRetryPolicyProvider supplies the current global retry setting. The
// dispatcher reads it at failure time, so editing settings affects future
// retries without reviving terminal tasks or versioning individual tasks.
type MailTaskRetryPolicyProvider interface {
	CurrentMailRetryCount(context.Context) (int, error)
}

type MailTaskRetentionProvider interface {
	CurrentMailRetentionDays(context.Context) (int, error)
}

type MailTaskBatchItem struct {
	ID     string
	Result string // succeeded, skipped, or failed
	Code   string
}

type MailTaskBatchResult struct {
	Succeeded int
	Skipped   int
	Failed    int
	Items     []MailTaskBatchItem
}

// MailTaskManagement owns the independent mail-task RBAC capability. Write
// does not implicitly grant read; callers which need both permissions must
// receive both grants in role configuration.
type MailTaskManagement struct {
	store      MailTaskStore
	principals PrincipalStore
	catalog    domain.PermissionCatalog
}

func NewMailTaskManagement(store MailTaskStore, principals PrincipalStore, catalogs ...domain.PermissionCatalog) *MailTaskManagement {
	catalog := domain.DefaultPermissionCatalog()
	if len(catalogs) > 0 && len(catalogs[0].Definitions()) > 0 {
		catalog = catalogs[0]
	}
	return &MailTaskManagement{store: store, principals: principals, catalog: catalog}
}

func (m *MailTaskManagement) principal(ctx context.Context, actorID string) (domain.Principal, error) {
	if m == nil || m.principals == nil || actorID == "" {
		return domain.Principal{}, ErrDependencyUnavailable
	}
	principal, err := m.principals.FindPrincipalByID(ctx, actorID)
	if err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			return domain.Principal{}, ErrUnauthenticated
		}
		return domain.Principal{}, dependencyError(err)
	}
	if err := ensurePrincipalIdentity(actorID, principal); err != nil {
		return domain.Principal{}, err
	}
	return normalizePrincipal(m.catalog, principal)
}

func (m *MailTaskManagement) require(ctx context.Context, actorID string, permission domain.PermissionKey) error {
	if m == nil || m.store == nil || !m.catalog.Has(permission) {
		return ErrForbidden
	}
	principal, err := m.principal(ctx, actorID)
	if err != nil {
		return err
	}
	if !principal.Has(permission) {
		return ErrForbidden
	}
	return nil
}

func (m *MailTaskManagement) List(ctx context.Context, actorID string, options MailTaskListOptions) (MailTaskPage, error) {
	if err := m.require(ctx, actorID, domain.PermissionMailTasksRead); err != nil {
		return MailTaskPage{}, err
	}
	options, err := normalizeMailTaskListOptions(options)
	if err != nil {
		return MailTaskPage{}, err
	}
	page, err := m.store.ListMailTasks(ctx, options)
	if err != nil {
		return MailTaskPage{}, normalizeMailTaskError(err)
	}
	return page, nil
}

func (m *MailTaskManagement) Detail(ctx context.Context, actorID, taskID string) (MailTask, error) {
	if err := m.require(ctx, actorID, domain.PermissionMailTasksRead); err != nil {
		return MailTask{}, err
	}
	if !domain.IsCanonicalUUID(taskID) {
		return MailTask{}, ErrMailTaskNotFound
	}
	task, err := m.store.FindMailTask(ctx, taskID)
	if err != nil {
		return MailTask{}, normalizeMailTaskError(err)
	}
	return task, nil
}

func (m *MailTaskManagement) Retry(ctx context.Context, actorID, taskID string) (MailTask, error) {
	if err := m.require(ctx, actorID, domain.PermissionMailTasksWrite); err != nil {
		return MailTask{}, err
	}
	if !domain.IsCanonicalUUID(taskID) {
		return MailTask{}, ErrMailTaskNotFound
	}
	task, err := m.store.RetryMailTask(ctx, taskID)
	if err != nil {
		return MailTask{}, normalizeMailTaskError(err)
	}
	return task, nil
}

func (m *MailTaskManagement) Delete(ctx context.Context, actorID, taskID string) error {
	if err := m.require(ctx, actorID, domain.PermissionMailTasksWrite); err != nil {
		return err
	}
	if !domain.IsCanonicalUUID(taskID) {
		return ErrMailTaskNotFound
	}
	return normalizeMailTaskError(m.store.DeleteMailTask(ctx, taskID))
}

func (m *MailTaskManagement) BulkRetry(ctx context.Context, actorID string, taskIDs []string) (MailTaskBatchResult, error) {
	if err := m.require(ctx, actorID, domain.PermissionMailTasksWrite); err != nil {
		return MailTaskBatchResult{}, err
	}
	ids, err := validateMailTaskBatch(taskIDs)
	if err != nil {
		return MailTaskBatchResult{}, err
	}
	result := MailTaskBatchResult{Items: make([]MailTaskBatchItem, 0, len(ids))}
	for _, id := range ids {
		_, operationErr := m.store.RetryMailTask(ctx, id)
		item := MailTaskBatchItem{ID: id, Result: "succeeded"}
		if operationErr != nil {
			operationErr = normalizeMailTaskError(operationErr)
			item.Result = "skipped"
			item.Code = mailTaskBatchCode(operationErr)
			if errors.Is(operationErr, ErrDependencyUnavailable) || errors.Is(operationErr, ErrMailTaskDependency) {
				item.Result = "failed"
				result.Failed++
			} else {
				result.Skipped++
			}
		} else {
			result.Succeeded++
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func (m *MailTaskManagement) BulkDelete(ctx context.Context, actorID string, taskIDs []string) (MailTaskBatchResult, error) {
	if err := m.require(ctx, actorID, domain.PermissionMailTasksWrite); err != nil {
		return MailTaskBatchResult{}, err
	}
	ids, err := validateMailTaskBatch(taskIDs)
	if err != nil {
		return MailTaskBatchResult{}, err
	}
	result := MailTaskBatchResult{Items: make([]MailTaskBatchItem, 0, len(ids))}
	for _, id := range ids {
		operationErr := m.store.DeleteMailTask(ctx, id)
		item := MailTaskBatchItem{ID: id, Result: "succeeded"}
		if operationErr != nil {
			operationErr = normalizeMailTaskError(operationErr)
			item.Result = "skipped"
			item.Code = mailTaskBatchCode(operationErr)
			if errors.Is(operationErr, ErrDependencyUnavailable) || errors.Is(operationErr, ErrMailTaskDependency) {
				item.Result = "failed"
				result.Failed++
			} else {
				result.Skipped++
			}
		} else {
			result.Succeeded++
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func (m *MailTaskManagement) OwnStatus(ctx context.Context, actorID, taskID string) (MailTask, error) {
	if m == nil || m.store == nil {
		return MailTask{}, ErrDependencyUnavailable
	}
	if _, err := m.principal(ctx, actorID); err != nil {
		return MailTask{}, err
	}
	if !domain.IsCanonicalUUID(taskID) {
		return MailTask{}, ErrMailTaskNotFound
	}
	ownerStore, ok := m.store.(MailTaskOwnerStore)
	if !ok {
		return MailTask{}, ErrDependencyUnavailable
	}
	task, err := ownerStore.FindMailTaskForSubmitter(ctx, taskID, actorID)
	if err != nil {
		return MailTask{}, normalizeMailTaskError(err)
	}
	if task.Kind != MailTest {
		return MailTask{}, ErrMailTaskNotFound
	}
	return task, nil
}

func normalizeMailTaskListOptions(options MailTaskListOptions) (MailTaskListOptions, error) {
	if options.Limit == 0 {
		options.Limit = MailTaskDefaultPageSize
	}
	if options.Limit < 1 || options.Limit > MailTaskMaxPageSize {
		return MailTaskListOptions{}, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "limit", Code: "invalid_limit"}}}
	}
	options.Cursor = strings.TrimSpace(options.Cursor)
	if len(options.Cursor) > 512 {
		return MailTaskListOptions{}, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "cursor", Code: "invalid_cursor"}}}
	}
	options.Recipient = strings.TrimSpace(options.Recipient)
	if options.Recipient != "" {
		email, err := domain.NewEmail(options.Recipient)
		if err != nil || strings.ContainsAny(options.Recipient, "\r\n") {
			return MailTaskListOptions{}, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "recipient", Code: "invalid_email"}}}
		}
		// Recipient filtering is case-insensitive in PostgreSQL; bind the
		// cursor to the same canonical value so equivalent filter spellings
		// cannot produce pagination drift.
		options.Recipient = email.Canonical
	}
	if options.Kind != "" && !options.Kind.Valid() {
		return MailTaskListOptions{}, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "kind", Code: "invalid_value"}}}
	}
	if options.Status != "" {
		switch options.Status {
		case MailTaskStatusQueued, MailTaskStatusSending, MailTaskStatusWaitingRetry, MailTaskStatusSent, MailTaskStatusFailed:
		default:
			return MailTaskListOptions{}, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "status", Code: "invalid_value"}}}
		}
	}
	if !options.From.IsZero() && !options.To.IsZero() && !options.From.Before(options.To) {
		return MailTaskListOptions{}, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "to", Code: "invalid_time_range"}}}
	}
	return options, nil
}

func validateMailTaskBatch(ids []string) ([]string, error) {
	if len(ids) == 0 || len(ids) > MailTaskMaxBatchSize {
		return nil, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "ids", Code: "invalid_batch"}}}
	}
	seen := make(map[string]struct{}, len(ids))
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		if !domain.IsCanonicalUUID(id) {
			return nil, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "ids", Code: "invalid_id"}}}
		}
		if _, exists := seen[id]; exists {
			return nil, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "ids", Code: "duplicate_id"}}}
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result, nil
}

func mailTaskBatchCode(err error) string {
	switch {
	case errors.Is(err, ErrMailTaskSending):
		return "sending"
	case errors.Is(err, ErrMailTaskNotRetryable):
		return "not_failed"
	case errors.Is(err, ErrMailTaskNotFound):
		return "not_found"
	case errors.Is(err, ErrMailTaskDependency), errors.Is(err, ErrDependencyUnavailable):
		return "dependency_unavailable"
	default:
		return "operation_failed"
	}
}

func normalizeMailTaskError(err error) error {
	if err == nil {
		return nil
	}
	for _, known := range []error{
		ErrMailTaskNotFound, ErrMailTaskSending, ErrMailTaskNotRetryable, ErrMailTaskDependency,
		ErrMailTaskNotConfigured, ErrMailSettingsNotSaved, ErrForbidden, ErrUnauthenticated,
		ErrDependencyUnavailable, ErrStaleRevision,
	} {
		if errors.Is(err, known) {
			return err
		}
	}
	return dependencyError(err)
}

// NewMailTaskSecretBox derives a purpose-specific encryption key from the
// stable required deployment secret (the generated server supplies the
// password-reset authority). A separate purpose prevents ciphertexts from
// being mistaken for SMTP settings or token authorities when the same master
// secret is used to bootstrap a fresh template project.
func NewMailTaskSecretBox(master []byte) (*AESGCMSecretBox, error) {
	if len(master) != 32 {
		return nil, ErrDependencyUnavailable
	}
	return newAESGCMSecretBoxWithKey(derivePurposeKey(master, "temvia-mail-task-material-v1"))
}

func newAESGCMSecretBoxWithKey(key []byte) (*AESGCMSecretBox, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	// Keep the actual AEAD construction in settings.go, while using this small
	// helper to make purpose-separated task boxes explicit at the API seam.
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &AESGCMSecretBox{aead: aead}, nil
}

func validateMailTaskMaterial(material MailTaskMaterial) error {
	if material.Version != 1 || !material.Kind.Valid() || !material.Locale.Valid() || material.ExpiresAt.IsZero() || material.CreatedAt.IsZero() || !material.ExpiresAt.After(material.CreatedAt) {
		return fmt.Errorf("invalid mail task material")
	}
	if _, err := domain.NewEmail(material.Email); err != nil || strings.ContainsAny(material.Email, "\r\n") {
		return fmt.Errorf("invalid mail task material")
	}
	if strings.ContainsAny(material.Name, "\r\n") || strings.ContainsAny(material.SystemName, "\r\n") {
		return fmt.Errorf("invalid mail task material")
	}
	switch material.Kind {
	case MailPasswordReset, MailUserInvitation:
		if material.Message != nil || len(material.ResetSelector) != domain.PasswordResetSelectorBytes || len(material.EmailSelector) != 0 || len(material.VerifierDigest) != domain.PasswordResetVerifierBytes {
			return fmt.Errorf("invalid mail task material")
		}
	case MailEmailChangeCode:
		if material.Message != nil || len(material.ResetSelector) != 0 || len(material.EmailSelector) != domain.EmailChangeSelectorBytes || len(material.VerifierDigest) != domain.PasswordResetVerifierBytes {
			return fmt.Errorf("invalid mail task material")
		}
	case MailPasswordChanged, MailEmailChanged:
		if material.Message != nil || len(material.ResetSelector) != 0 || len(material.EmailSelector) != 0 || len(material.VerifierDigest) != 0 {
			return fmt.Errorf("invalid mail task material")
		}
	case MailTest:
		if material.Message == nil || len(material.ResetSelector) != 0 || len(material.EmailSelector) != 0 || len(material.VerifierDigest) != 0 {
			return fmt.Errorf("invalid mail task material")
		}
	}
	if material.Message != nil {
		message := material.Message
		if message.To == "" || message.To != material.Email || message.Kind != material.Kind || !message.Locale.Valid() ||
			strings.ContainsAny(message.To, "\r\n") || strings.ContainsAny(message.Name, "\r\n") ||
			strings.ContainsAny(message.SystemName, "\r\n") || strings.ContainsAny(message.Subject, "\r\n") ||
			strings.ContainsAny(message.MessageID, "\r\n") {
			return fmt.Errorf("invalid mail task message")
		}
	}
	return nil
}

// SealMailTaskMaterial serializes and encrypts only the stable resend
// projection. The caller must retain the returned bytes in the task store;
// plaintext material is intentionally not part of any management response.
func SealMailTaskMaterial(box SecretBox, material MailTaskMaterial) ([]byte, error) {
	if box == nil {
		return nil, ErrDependencyUnavailable
	}
	if err := validateMailTaskMaterial(material); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(material)
	if err != nil {
		return nil, err
	}
	return box.Encrypt(encoded)
}

// OpenMailTaskMaterial is used solely by the delivery worker. It validates the
// authenticated plaintext before it is used to construct an SMTP message.
func OpenMailTaskMaterial(box SecretBox, ciphertext []byte) (MailTaskMaterial, error) {
	if box == nil || len(ciphertext) == 0 {
		return MailTaskMaterial{}, ErrDependencyUnavailable
	}
	encoded, err := box.Decrypt(ciphertext)
	if err != nil {
		return MailTaskMaterial{}, ErrDependencyUnavailable
	}
	var material MailTaskMaterial
	if err := json.Unmarshal(encoded, &material); err != nil {
		return MailTaskMaterial{}, ErrDependencyUnavailable
	}
	if err := validateMailTaskMaterial(material); err != nil {
		return MailTaskMaterial{}, ErrDependencyUnavailable
	}
	return material, nil
}
