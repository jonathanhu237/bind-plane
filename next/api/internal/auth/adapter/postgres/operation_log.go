package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
)

func (s *Store) CreateOperationLog(ctx context.Context, input application.OperationLogInput) error {
	details := input.Details
	if details == nil {
		details = map[string]any{}
	}
	detailJSON, err := json.Marshal(details)
	if err != nil {
		return err
	}
	occurredAt := input.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	actorID := nullableString(input.ActorID)
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO auth_operation_logs
			(actor_user_id, actor_name, actor_email, actor_kind, actor_label, action, object_type, object_id, result,
			 occurred_at, source_ip, attempted_account, details)
		VALUES (NULLIF($1, '')::uuid, NULLIF($2, ''), NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), $6, $7, NULLIF($8, ''), $9,
		        $10, NULLIF($11, ''), NULLIF($12, ''), $13::jsonb)`,
		actorID, input.ActorName, input.ActorEmail, input.ActorKind, input.ActorLabel, input.Action, input.ObjectType, input.ObjectID, input.Result,
		occurredAt, input.SourceIP, input.AttemptedAccount, detailJSON)
	return err
}

// Deleted identity lookup never joins by email: a reused email is a new user.
const operationLogSelect = `SELECT l.id::text,l.actor_user_id::text,COALESCE(l.actor_name,ad.name,''),COALESCE(l.actor_email,ad.email,''),
 COALESCE(l.actor_kind,''),COALESCE(l.actor_label,''),l.action,l.object_type,COALESCE(l.object_id,''),l.result,l.occurred_at,
 COALESCE(l.source_ip,''),COALESCE(l.attempted_account,''),
 CASE WHEN td.user_id IS NOT NULL AND NOT(l.details ? 'target' OR l.details ? 'before' OR l.details ? 'after')
 THEN l.details || jsonb_build_object('target',jsonb_build_object('id',td.user_id,'name',td.name,'email',td.email)) ELSE l.details END,
 ad.user_id IS NOT NULL,td.user_id IS NOT NULL
 FROM auth_operation_logs l
 LEFT JOIN auth_deleted_user_identities ad ON ad.user_id=l.actor_user_id
 LEFT JOIN auth_deleted_user_identities td ON td.user_id::text=l.object_id AND l.object_type IN ('user','account','password')`

func (s *Store) ListOperationLogs(ctx context.Context, options application.OperationLogListOptions) (application.OperationLogPage, error) {
	args := make([]any, 0, 10)
	clauses := make([]string, 0, 10)
	arg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	if !options.From.IsZero() {
		clauses = append(clauses, "l.occurred_at >= "+arg(options.From))
	}
	if !options.To.IsZero() {
		clauses = append(clauses, "l.occurred_at < "+arg(options.To))
	}
	if options.ActorID != "" {
		clauses = append(clauses, "l.actor_user_id = "+arg(options.ActorID)+"::uuid")
	}
	if options.Action != "" {
		clauses = append(clauses, "l.action = "+arg(options.Action))
	}
	if options.ObjectType != "" {
		clauses = append(clauses, "l.object_type = "+arg(options.ObjectType))
	}
	if options.ObjectID != "" {
		clauses = append(clauses, "l.object_id = "+arg(options.ObjectID))
	}
	if options.Result != "" {
		clauses = append(clauses, "l.result = "+arg(options.Result))
	}
	if options.Cursor != "" {
		cursorArg := arg(options.Cursor)
		clauses = append(clauses, `(l.occurred_at, l.id) < (SELECT occurred_at, id FROM auth_operation_logs WHERE id = `+cursorArg+`::uuid)`)
	}
	query := operationLogSelect
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	limit := options.Limit
	if limit == 0 {
		limit = 25
	}
	query += " ORDER BY l.occurred_at DESC, l.id DESC LIMIT " + arg(limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return application.OperationLogPage{}, err
	}
	defer rows.Close()
	items := make([]application.OperationLog, 0, limit)
	for rows.Next() {
		var item application.OperationLog
		var actorID sql.NullString
		var detailJSON []byte
		if err := rows.Scan(&item.ID, &actorID, &item.ActorName, &item.ActorEmail, &item.ActorKind, &item.ActorLabel, &item.Action, &item.ObjectType, &item.ObjectID, &item.Result, &item.OccurredAt, &item.SourceIP, &item.AttemptedAccount, &detailJSON, &item.ActorDeleted, &item.ObjectDeleted); err != nil {
			return application.OperationLogPage{}, err
		}
		item.ActorID = actorID.String
		item.Details = decodeOperationDetails(detailJSON)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return application.OperationLogPage{}, err
	}
	page := application.OperationLogPage{Items: items}
	if len(items) > limit {
		page.NextCursor = items[limit-1].ID
		page.Items = items[:limit]
	}
	return page, nil
}

func (s *Store) FindOperationLog(ctx context.Context, id string) (application.OperationLog, error) {
	var item application.OperationLog
	var actorID sql.NullString
	var detailJSON []byte
	err := s.db.QueryRowContext(ctx, operationLogSelect+` WHERE l.id = $1::uuid`, id).
		Scan(&item.ID, &actorID, &item.ActorName, &item.ActorEmail, &item.ActorKind, &item.ActorLabel, &item.Action, &item.ObjectType, &item.ObjectID, &item.Result, &item.OccurredAt, &item.SourceIP, &item.AttemptedAccount, &detailJSON, &item.ActorDeleted, &item.ObjectDeleted)
	if errors.Is(err, sql.ErrNoRows) {
		return application.OperationLog{}, application.ErrOperationLogNotFound
	}
	if err != nil {
		return application.OperationLog{}, err
	}
	item.ActorID = actorID.String
	item.Details = decodeOperationDetails(detailJSON)
	return item, nil
}

func (s *Store) DeleteExpiredOperationLogs(ctx context.Context, days int) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM auth_operation_logs WHERE occurred_at < clock_timestamp() - ($1 * INTERVAL '1 day')`, days)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) GetOperationLogRetention(ctx context.Context) (application.OperationLogRetention, error) {
	var retention application.OperationLogRetention
	err := s.db.QueryRowContext(ctx, `SELECT retention_days, revision, updated_at FROM auth_operation_log_settings WHERE singleton = true`).Scan(&retention.Days, &retention.Revision, &retention.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return application.OperationLogRetention{}, application.ErrOperationLogRetentionNotFound
	}
	return retention, err
}

func (s *Store) SaveOperationLogRetention(ctx context.Context, expectedRevision int64, days int) (application.OperationLogRetention, error) {
	var saved application.OperationLogRetention
	if expectedRevision == 0 {
		err := s.db.QueryRowContext(ctx, `
			INSERT INTO auth_operation_log_settings (singleton, retention_days, revision)
			VALUES (true, $1, 1)
			ON CONFLICT (singleton) DO NOTHING
			RETURNING retention_days, revision, updated_at`, days).
			Scan(&saved.Days, &saved.Revision, &saved.UpdatedAt)
		if errors.Is(err, sql.ErrNoRows) {
			return application.OperationLogRetention{}, application.ErrStaleRevision
		}
		return saved, err
	}
	err := s.db.QueryRowContext(ctx, `
		UPDATE auth_operation_log_settings
		SET retention_days = $1, revision = revision + 1, updated_at = clock_timestamp()
		WHERE singleton = true AND revision = $2
		RETURNING retention_days, revision, updated_at`, days, expectedRevision).
		Scan(&saved.Days, &saved.Revision, &saved.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return application.OperationLogRetention{}, application.ErrStaleRevision
	}
	return saved, err
}

func decodeOperationDetails(value []byte) map[string]any {
	if len(value) == 0 {
		return map[string]any{}
	}
	var details map[string]any
	if err := json.Unmarshal(value, &details); err != nil || details == nil {
		return map[string]any{}
	}
	return details
}

func nullableString(value string) string { return value }

var _ application.OperationLogStore = (*Store)(nil)
