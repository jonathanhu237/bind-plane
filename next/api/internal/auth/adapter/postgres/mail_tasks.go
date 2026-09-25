package postgres

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

const mailTaskStatusSQL = `(CASE
	WHEN o.sent_at IS NOT NULL THEN 'sent'
	WHEN o.dead_at IS NOT NULL OR o.canceled_at IS NOT NULL THEN 'failed'
	WHEN o.lease_token IS NOT NULL AND o.lease_expires_at > clock_timestamp() THEN 'sending'
	WHEN o.available_at > clock_timestamp() THEN 'waiting_retry'
	ELSE 'queued'
END)`

type mailTaskCursor struct {
	Version    int       `json:"v"`
	CreatedAt  time.Time `json:"createdAt"`
	ID         string    `json:"id"`
	Recipient  string    `json:"recipient,omitempty"`
	Kind       string    `json:"kind,omitempty"`
	Status     string    `json:"status,omitempty"`
	From       string    `json:"from,omitempty"`
	To         string    `json:"to,omitempty"`
	FailedOnly bool      `json:"failedOnly,omitempty"`
}

func encodeMailTaskCursor(createdAt time.Time, id string, options application.MailTaskListOptions) string {
	cursor := mailTaskCursor{Version: 1, CreatedAt: createdAt.UTC(), ID: id, Recipient: strings.ToLower(strings.TrimSpace(options.Recipient)), Kind: string(options.Kind), Status: options.Status, FailedOnly: options.FailedOnly}
	if !options.From.IsZero() {
		cursor.From = options.From.UTC().Format(time.RFC3339Nano)
	}
	if !options.To.IsZero() {
		cursor.To = options.To.UTC().Format(time.RFC3339Nano)
	}
	encoded, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func decodeMailTaskCursor(value string) (mailTaskCursor, error) {
	var cursor mailTaskCursor
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) == 0 || json.Unmarshal(decoded, &cursor) != nil || cursor.Version != 1 || cursor.CreatedAt.IsZero() || !domain.IsCanonicalUUID(cursor.ID) {
		return mailTaskCursor{}, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "cursor", Code: "invalid_cursor"}}}
	}
	return cursor, nil
}

func mailTaskCursorMatchesOptions(cursor mailTaskCursor, options application.MailTaskListOptions) bool {
	if cursor.Recipient != strings.ToLower(strings.TrimSpace(options.Recipient)) || cursor.Kind != string(options.Kind) || cursor.Status != options.Status || cursor.FailedOnly != options.FailedOnly {
		return false
	}
	format := func(value time.Time) string {
		if value.IsZero() {
			return ""
		}
		return value.UTC().Format(time.RFC3339Nano)
	}
	return cursor.From == format(options.From) && cursor.To == format(options.To)
}

func (s *Store) sealMailTaskMaterial(material application.MailTaskMaterial) ([]byte, error) {
	if s == nil || s.mailTaskSecretBox == nil {
		return nil, application.ErrDependencyUnavailable
	}
	return application.SealMailTaskMaterial(s.mailTaskSecretBox, material)
}

func mailTaskSystemName(ctx context.Context, tx *sql.Tx) (string, error) {
	var name string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(system_name, 'Temvia') FROM auth_system_identity WHERE singleton = true`).Scan(&name); err != nil {
		if err == sql.ErrNoRows {
			return application.DefaultSystemName, nil
		}
		return "", err
	}
	if strings.TrimSpace(name) == "" {
		return application.DefaultSystemName, nil
	}
	return name, nil
}

func (s *Store) EnqueueMailTask(ctx context.Context, input application.MailTaskInput) (application.MailTask, error) {
	if s == nil || s.db == nil {
		return application.MailTask{}, application.ErrDependencyUnavailable
	}
	if err := validateMailTaskInput(input); err != nil {
		return application.MailTask{}, err
	}
	createdAt := input.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	expiresAt := input.ExpiresAt
	if expiresAt.IsZero() {
		return application.MailTask{}, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "expiresAt", Code: "required"}}}
	}
	if !expiresAt.After(createdAt) {
		return application.MailTask{}, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "expiresAt", Code: "invalid_range"}}}
	}
	locale := input.Locale
	if !locale.Valid() {
		return application.MailTask{}, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "locale", Code: "invalid_value"}}}
	}
	systemName := strings.TrimSpace(input.SystemName)
	if systemName == "" {
		systemName = application.DefaultSystemName
	}
	recipient := strings.TrimSpace(input.RecipientEmail)
	if _, err := domain.NewEmail(recipient); err != nil || strings.ContainsAny(recipient, "\r\n") {
		return application.MailTask{}, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "recipient", Code: "invalid_email"}}}
	}
	material := application.MailTaskMaterial{
		Version:        1,
		Kind:           input.Kind,
		Name:           input.RecipientName,
		Email:          recipient,
		Locale:         locale,
		SystemName:     systemName,
		CreatedAt:      createdAt,
		ExpiresAt:      expiresAt,
		ResetSelector:  append([]byte(nil), input.ResetSelector...),
		EmailSelector:  append([]byte(nil), input.EmailSelector...),
		VerifierDigest: append([]byte(nil), input.VerifierDigest...),
	}
	if input.Message != nil {
		message := *input.Message
		if message.Kind == "" {
			message.Kind = input.Kind
		}
		if message.To == "" {
			message.To = recipient
		}
		if message.Name == "" {
			message.Name = input.RecipientName
		}
		if message.Locale == "" {
			message.Locale = locale
		}
		if message.SystemName == "" {
			message.SystemName = systemName
		}
		material.Message = &message
	}
	// Every configured/generated task must carry an authenticated resend
	// projection. A missing key is a dependency failure, never a successful
	// insert with mutable account joins as a fallback.
	materialCiphertext, err := s.sealMailTaskMaterial(material)
	if err != nil {
		return application.MailTask{}, err
	}
	if len(materialCiphertext) == 0 {
		return application.MailTask{}, application.ErrDependencyUnavailable
	}

	var id string
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO auth_mail_outbox (
			kind, user_id, invitation_id, reset_selector, recipient_email,
			recipient_name, email_change_request_id, email_change_selector,
			locale, system_name, material_ciphertext, submitted_by,
			round_number, round_attempt_count, expires_at, created_at
		)
		VALUES (
			$1, NULLIF($2, '')::uuid, NULLIF($3, '')::uuid, $4, $5,
			NULLIF($6, ''), NULLIF($7, '')::uuid, $8, $9, $10, $11,
			NULLIF($12, '')::uuid, 1, 0, $13, $14
		)
		RETURNING id::text`,
		string(input.Kind), input.UserID, input.InvitationID, nullableBytes(input.ResetSelector), recipient,
		strings.TrimSpace(input.RecipientName), input.EmailChangeRequestID, nullableBytes(input.EmailSelector),
		string(locale), systemName, nullableBytes(materialCiphertext), input.SubmittedBy, expiresAt, createdAt).Scan(&id)
	if err != nil {
		return application.MailTask{}, err
	}
	return s.FindMailTask(ctx, id)
}

func nullableBytes(value []byte) []byte {
	if len(value) == 0 {
		return nil
	}
	return value
}

func validateMailTaskInput(input application.MailTaskInput) error {
	if !input.Kind.Valid() {
		return &domain.ValidationErrors{Items: []domain.FieldError{{Field: "kind", Code: "invalid_value"}}}
	}
	for field, value := range map[string]string{"userID": input.UserID, "invitationID": input.InvitationID, "emailChangeRequestID": input.EmailChangeRequestID, "submittedBy": input.SubmittedBy} {
		if value != "" && !domain.IsCanonicalUUID(value) {
			return &domain.ValidationErrors{Items: []domain.FieldError{{Field: field, Code: "invalid_id"}}}
		}
	}
	if input.Kind == application.MailPasswordReset || input.Kind == application.MailUserInvitation {
		if len(input.ResetSelector) != domain.PasswordResetSelectorBytes || len(input.VerifierDigest) != domain.PasswordResetVerifierBytes {
			return &domain.ValidationErrors{Items: []domain.FieldError{{Field: "material", Code: "invalid_value"}}}
		}
	}
	if input.Kind == application.MailEmailChangeCode {
		if len(input.EmailSelector) != domain.EmailChangeSelectorBytes || len(input.VerifierDigest) != domain.PasswordResetVerifierBytes {
			return &domain.ValidationErrors{Items: []domain.FieldError{{Field: "material", Code: "invalid_value"}}}
		}
	}
	if (input.Kind == application.MailPasswordReset || input.Kind == application.MailPasswordChanged || input.Kind == application.MailEmailChangeCode || input.Kind == application.MailEmailChanged) && input.UserID == "" {
		return &domain.ValidationErrors{Items: []domain.FieldError{{Field: "userID", Code: "required"}}}
	}
	if input.Kind == application.MailUserInvitation && input.InvitationID == "" {
		return &domain.ValidationErrors{Items: []domain.FieldError{{Field: "invitationID", Code: "required"}}}
	}
	if input.Kind == application.MailEmailChangeCode && input.EmailChangeRequestID == "" {
		return &domain.ValidationErrors{Items: []domain.FieldError{{Field: "emailChangeRequestID", Code: "required"}}}
	}
	return nil
}

func (s *Store) ListMailTasks(ctx context.Context, options application.MailTaskListOptions) (application.MailTaskPage, error) {
	if s == nil || s.db == nil {
		return application.MailTaskPage{}, application.ErrDependencyUnavailable
	}
	if options.Limit == 0 {
		options.Limit = application.MailTaskDefaultPageSize
	}
	if options.Limit < 1 || options.Limit > application.MailTaskMaxPageSize {
		return application.MailTaskPage{}, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "limit", Code: "invalid_limit"}}}
	}
	where := []string{"TRUE"}
	args := make([]any, 0, 12)
	arg := func(value any) string {
		args = append(args, value)
		return "$" + strconvItoa(len(args))
	}
	if options.Recipient != "" {
		where = append(where, "lower(o.recipient_email) = "+arg(strings.ToLower(strings.TrimSpace(options.Recipient))))
	}
	if options.Kind != "" {
		where = append(where, "o.kind = "+arg(string(options.Kind)))
	}
	if options.From.IsZero() == false {
		where = append(where, "o.created_at >= "+arg(options.From))
	}
	if options.To.IsZero() == false {
		where = append(where, "o.created_at < "+arg(options.To))
	}
	statusExpr := mailTaskStatusSQL
	if options.Status != "" {
		where = append(where, mailTaskStatusFilter(options.Status))
	}
	if options.FailedOnly {
		where = append(where, mailTaskStatusFilter(application.MailTaskStatusFailed))
	}
	if options.Cursor != "" {
		cursor, err := decodeMailTaskCursor(options.Cursor)
		if err != nil || !mailTaskCursorMatchesOptions(cursor, options) {
			return application.MailTaskPage{}, &domain.ValidationErrors{Items: []domain.FieldError{{Field: "cursor", Code: "invalid_cursor"}}}
		}
		where = append(where, "(o.created_at, o.id) < ("+arg(cursor.CreatedAt)+", "+arg(cursor.ID)+"::uuid)")
	}
	limitArg := arg(options.Limit + 1)
	query := `SELECT o.id::text, o.kind, COALESCE(o.recipient_email, ''), COALESCE(o.recipient_name, ''),
		o.locale, ` + statusExpr + `, o.created_at, o.available_at,
		COALESCE(o.finished_at, o.sent_at, o.dead_at, o.canceled_at), o.attempt_count,
		o.round_number, o.round_attempt_count, COALESCE(o.last_error_code, '')
		FROM auth_mail_outbox AS o WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY o.created_at DESC, o.id DESC LIMIT ` + limitArg
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return application.MailTaskPage{}, err
	}
	defer rows.Close()
	items := make([]application.MailTask, 0, options.Limit)
	for rows.Next() {
		task, err := scanMailTask(rows)
		if err != nil {
			return application.MailTaskPage{}, err
		}
		items = append(items, task)
	}
	if err := rows.Err(); err != nil {
		return application.MailTaskPage{}, err
	}
	page := application.MailTaskPage{Items: items}
	if len(items) > options.Limit {
		last := items[options.Limit-1]
		page.Items = items[:options.Limit]
		page.NextCursor = encodeMailTaskCursor(last.CreatedAt, last.ID, options)
	}
	return page, nil
}

func mailTaskStatusFilter(status string) string {
	switch status {
	case application.MailTaskStatusQueued:
		return "o.sent_at IS NULL AND o.dead_at IS NULL AND o.canceled_at IS NULL AND (o.lease_token IS NULL OR o.lease_expires_at <= clock_timestamp()) AND o.available_at <= clock_timestamp()"
	case application.MailTaskStatusSending:
		return "o.sent_at IS NULL AND o.dead_at IS NULL AND o.canceled_at IS NULL AND o.lease_token IS NOT NULL AND o.lease_expires_at > clock_timestamp()"
	case application.MailTaskStatusWaitingRetry:
		return "o.sent_at IS NULL AND o.dead_at IS NULL AND o.canceled_at IS NULL AND (o.lease_token IS NULL OR o.lease_expires_at <= clock_timestamp()) AND o.available_at > clock_timestamp()"
	case application.MailTaskStatusSent:
		return "o.sent_at IS NOT NULL"
	case application.MailTaskStatusFailed:
		return "(o.dead_at IS NOT NULL OR o.canceled_at IS NOT NULL)"
	default:
		// The application layer validates status values. Keep direct store
		// callers safe by returning a predicate that cannot expose rows.
		return "FALSE"
	}
}

func strconvItoa(value int) string {
	// This tiny helper keeps query construction local without pulling a second
	// formatting path into every call site.
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var digits [20]byte
	i := len(digits)
	for value > 0 {
		i--
		digits[i] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}

func (s *Store) FindMailTask(ctx context.Context, id string) (application.MailTask, error) {
	return s.findMailTask(ctx, `o.id = $1::uuid`, id, true)
}

func (s *Store) FindMailTaskForSubmitter(ctx context.Context, id, submitter string) (application.MailTask, error) {
	if !domain.IsCanonicalUUID(submitter) {
		return application.MailTask{}, application.ErrMailTaskNotFound
	}
	// The settings page only needs a small owner-scoped status projection; the
	// full attempt history remains available through the independent task-read
	// permission and detail endpoint.
	return s.findMailTask(ctx, `o.id = $1::uuid AND o.submitted_by = $2::uuid`, id, false, submitter)
}

func (s *Store) findMailTask(ctx context.Context, predicate string, id string, includeAttempts bool, extra ...any) (application.MailTask, error) {
	if s == nil || s.db == nil {
		return application.MailTask{}, application.ErrDependencyUnavailable
	}
	args := []any{id}
	args = append(args, extra...)
	query := `SELECT o.id::text, o.kind, COALESCE(o.recipient_email, ''), COALESCE(o.recipient_name, ''),
		o.locale, ` + mailTaskStatusSQL + `, o.created_at, o.available_at,
		COALESCE(o.finished_at, o.sent_at, o.dead_at, o.canceled_at), o.attempt_count,
		o.round_number, o.round_attempt_count, COALESCE(o.last_error_code, '')
		FROM auth_mail_outbox AS o WHERE ` + predicate
	task, err := scanMailTask(s.db.QueryRowContext(ctx, query, args...))
	if err == sql.ErrNoRows {
		return application.MailTask{}, application.ErrMailTaskNotFound
	}
	if err != nil {
		return application.MailTask{}, err
	}
	if includeAttempts {
		rows, err := s.db.QueryContext(ctx, `SELECT id::text, round_number, attempt_number, outcome, COALESCE(error_code, ''), occurred_at FROM auth_mail_task_attempts WHERE task_id = $1::uuid ORDER BY occurred_at, id`, task.ID)
		if err != nil {
			return application.MailTask{}, err
		}
		defer rows.Close()
		for rows.Next() {
			var attempt application.MailTaskAttempt
			if err := rows.Scan(&attempt.ID, &attempt.Round, &attempt.Attempt, &attempt.Outcome, &attempt.ErrorCode, &attempt.OccurredAt); err != nil {
				return application.MailTask{}, err
			}
			task.Attempts = append(task.Attempts, attempt)
		}
		if err := rows.Err(); err != nil {
			return application.MailTask{}, err
		}
	}
	return task, nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanMailTask(row rowScanner) (application.MailTask, error) {
	var task application.MailTask
	var kind, locale, status string
	var finished sql.NullTime
	if err := row.Scan(&task.ID, &kind, &task.RecipientEmail, &task.RecipientName, &locale, &status, &task.CreatedAt, &task.AvailableAt, &finished, &task.AttemptCount, &task.Round, &task.RoundAttemptCount, &task.LastErrorCode); err != nil {
		return application.MailTask{}, err
	}
	task.Kind = application.MailKind(kind)
	task.Locale = domain.Locale(locale)
	task.Status = status
	if finished.Valid {
		task.FinishedAt = finished.Time
	}
	return task, nil
}

func (s *Store) RetryMailTask(ctx context.Context, id string) (application.MailTask, error) {
	if s == nil || s.db == nil {
		return application.MailTask{}, application.ErrDependencyUnavailable
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return application.MailTask{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var sentAt, canceledAt, deadAt sql.NullTime
	var leaseActive bool
	err = tx.QueryRowContext(ctx, `SELECT sent_at, canceled_at, dead_at, COALESCE(lease_expires_at > clock_timestamp(), false) AND sent_at IS NULL AND canceled_at IS NULL AND dead_at IS NULL FROM auth_mail_outbox WHERE id = $1::uuid FOR UPDATE`, id).Scan(&sentAt, &canceledAt, &deadAt, &leaseActive)
	if err == sql.ErrNoRows {
		return application.MailTask{}, application.ErrMailTaskNotFound
	}
	if err != nil {
		return application.MailTask{}, err
	}
	if leaseActive {
		return application.MailTask{}, application.ErrMailTaskSending
	}
	if !canceledAt.Valid && !deadAt.Valid {
		return application.MailTask{}, application.ErrMailTaskNotRetryable
	}
	if _, err := tx.ExecContext(ctx, `UPDATE auth_mail_outbox SET canceled_at = NULL, dead_at = NULL, available_at = clock_timestamp(), lease_token = NULL, lease_expires_at = NULL, last_error_code = NULL, finished_at = NULL, round_number = round_number + 1, round_attempt_count = 0 WHERE id = $1::uuid`, id); err != nil {
		return application.MailTask{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.MailTask{}, err
	}
	return s.FindMailTask(ctx, id)
}

func (s *Store) DeleteMailTask(ctx context.Context, id string) error {
	if s == nil || s.db == nil {
		return application.ErrDependencyUnavailable
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var sentAt, canceledAt, deadAt sql.NullTime
	var leaseActive bool
	err = tx.QueryRowContext(ctx, `SELECT sent_at, canceled_at, dead_at, COALESCE(lease_expires_at > clock_timestamp(), false) AND sent_at IS NULL AND canceled_at IS NULL AND dead_at IS NULL FROM auth_mail_outbox WHERE id = $1::uuid FOR UPDATE`, id).Scan(&sentAt, &canceledAt, &deadAt, &leaseActive)
	if err == sql.ErrNoRows {
		return application.ErrMailTaskNotFound
	}
	if err != nil {
		return err
	}
	if leaseActive {
		return application.ErrMailTaskSending
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_mail_outbox WHERE id = $1::uuid`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CurrentMailRetryCount(ctx context.Context) (int, error) {
	if s == nil || s.db == nil {
		return 0, application.ErrDependencyUnavailable
	}
	var value sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT auto_retry_count FROM auth_email_settings WHERE singleton = true`).Scan(&value); err != nil {
		if err == sql.ErrNoRows {
			return application.DefaultAutoRetryCount, nil
		}
		return 0, err
	}
	if !value.Valid || value.Int64 < application.MailTaskMinRetryCount || value.Int64 > application.MailTaskMaxRetryCount {
		return application.DefaultAutoRetryCount, nil
	}
	return int(value.Int64), nil
}

func (s *Store) CurrentMailRetentionDays(ctx context.Context) (int, error) {
	if s == nil || s.db == nil {
		return 0, application.ErrDependencyUnavailable
	}
	var value sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT retention_days FROM auth_email_settings WHERE singleton = true`).Scan(&value); err != nil {
		if err == sql.ErrNoRows {
			return application.DefaultMailRetentionDays, nil
		}
		return 0, err
	}
	if !value.Valid || value.Int64 < application.MailTaskMinRetentionDays || value.Int64 > application.MailTaskMaxRetentionDays {
		return application.DefaultMailRetentionDays, nil
	}
	return int(value.Int64), nil
}

var _ application.MailTaskStore = (*Store)(nil)
var _ application.MailTaskRetryPolicyProvider = (*Store)(nil)
var _ application.MailTaskRetentionProvider = (*Store)(nil)
