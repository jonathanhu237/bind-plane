package postgres

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

const stateCleanupBatchSize = 100

type rateLimitCleanupPolicy struct {
	namespace string
	kind      string
	capacity  int
	refill    time.Duration
}

// rateLimitCleanupPolicies mirrors the policies used by the limiter. Cleanup
// must use the currently configured retention derived from last_refill_at, not
// only the deadline written by an older configuration. Otherwise lengthening
// an interval could delete a depleted bucket and let it recover immediately.
func (s *Store) rateLimitCleanupPolicies() []rateLimitCleanupPolicy {
	return []rateLimitCleanupPolicy{
		{namespace: "login", kind: rateLimitKindGlobal, capacity: s.globalCapacity, refill: s.globalRefill},
		{namespace: "login", kind: rateLimitKindIP, capacity: s.loginIPCapacity, refill: s.loginIPRefill},
		{namespace: "login", kind: rateLimitKindEmail, capacity: s.emailCapacity, refill: s.emailRefill},
		{namespace: "password-reset", kind: rateLimitKindGlobal, capacity: s.resetGlobalCapacity, refill: s.resetGlobalRefill},
		{namespace: "password-reset", kind: rateLimitKindIP, capacity: s.resetIPCapacity, refill: s.resetIPRefill},
		{namespace: "password-reset", kind: rateLimitKindEmail, capacity: s.resetEmailCapacity, refill: s.resetEmailRefill},
		{namespace: "password-reset-complete", kind: rateLimitKindIP, capacity: s.resetCompleteIPCapacity, refill: s.resetCompleteIPRefill},
		{namespace: "invitation-accept", kind: rateLimitKindIP, capacity: s.invitationAcceptIPCapacity, refill: s.invitationAcceptIPRefill},
		{namespace: "setup", kind: rateLimitKindIP, capacity: s.setupIPCapacity, refill: s.setupIPRefill},
		{namespace: "invitation-send", kind: rateLimitKindActor, capacity: s.invitationActorCapacity, refill: s.invitationActorRefill},
		{namespace: "invitation-send", kind: rateLimitKindRecipient, capacity: s.invitationRecipientCapacity, refill: s.invitationRecipientRefill},
		{namespace: "test-email", kind: rateLimitKindGlobal, capacity: s.testMailGlobalCapacity, refill: s.testMailGlobalRefill},
		{namespace: "test-email", kind: rateLimitKindActor, capacity: s.testMailActorCapacity, refill: s.testMailActorRefill},
		{namespace: "test-email", kind: rateLimitKindRecipient, capacity: s.testMailRecipientCapacity, refill: s.testMailRecipientRefill},
	}
}

// CleanupExpiredRateLimitBuckets removes only rows whose finite retention
// deadline has passed. A request that has locked a bucket is either allowed to
// finish first or causes the cleanup row to be skipped.
func (s *Store) CleanupExpiredRateLimitBuckets(ctx context.Context, batchSize int) (int64, error) {
	if batchSize <= 0 {
		batchSize = stateCleanupBatchSize
	}
	operationCtx, cancel := s.stateContext(ctx)
	defer cancel()
	policies := s.rateLimitCleanupPolicies()
	placeholders := make([]string, 0, len(policies))
	args := make([]any, 0, 1+len(policies)*3)
	args = append(args, batchSize)
	for index, policy := range policies {
		base := 2 + index*3
		placeholders = append(placeholders, fmt.Sprintf("($%d::text, $%d::text, $%d::bigint)", base, base+1, base+2))
		args = append(args, policy.namespace, policy.kind, durationMillis(bucketTTL(policy.capacity, policy.refill)))
	}
	query := fmt.Sprintf(`
		WITH policies(namespace, bucket_kind, retention_ms) AS (
			VALUES %s
		), expired AS (
			SELECT buckets.ctid
			FROM auth_rate_limit_buckets AS buckets
			LEFT JOIN policies
				ON policies.namespace = buckets.namespace
				AND policies.bucket_kind = buckets.bucket_kind
			WHERE COALESCE(
				buckets.last_refill_at + (policies.retention_ms * INTERVAL '1 millisecond'),
				buckets.expires_at
			) <= clock_timestamp()
			ORDER BY buckets.expires_at
			LIMIT $1
			FOR UPDATE OF buckets SKIP LOCKED
		)
		DELETE FROM auth_rate_limit_buckets AS buckets
		USING expired
		WHERE buckets.ctid = expired.ctid`, strings.Join(placeholders, ", "))
	result, err := s.db.ExecContext(operationCtx, query, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// RunStateCleanup starts the existing-style in-process cleanup loop. Cleanup
// affects storage size only; request-time validity checks remain authoritative.
func (s *Store) RunStateCleanup(ctx context.Context, interval time.Duration) {
	if ctx == nil {
		ctx = context.Background()
	}
	if interval <= 0 {
		interval = time.Minute
	}
	cleanup := func() {
		if _, err := s.CleanupExpiredSessions(ctx, stateCleanupBatchSize); err != nil && ctx.Err() == nil {
			log.Printf("session cleanup failed: %s", stateCleanupErrorCategory(err))
		}
		if _, err := s.CleanupExpiredRateLimitBuckets(ctx, stateCleanupBatchSize); err != nil && ctx.Err() == nil {
			log.Printf("rate-limit cleanup failed: %s", stateCleanupErrorCategory(err))
		}
	}
	cleanup()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanup()
		}
	}
}

func stateCleanupErrorCategory(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		return "storage_error"
	}
}
