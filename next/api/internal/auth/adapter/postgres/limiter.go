package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
)

const (
	rateLimitKindGlobal    = "global"
	rateLimitKindIP        = "ip"
	rateLimitKindEmail     = "email"
	rateLimitKindActor     = "actor"
	rateLimitKindRecipient = "recipient"
)

var globalRateLimitDigest = make([]byte, sha256.Size)

type rateLimitBucket struct {
	tokens       int64
	lastRefilled time.Time
}

type rateLimitSpec struct {
	kind     string
	digest   []byte
	capacity int
	refill   time.Duration
}

// Allow is the original global-plus-email login seam. It remains available to
// small embedders and tests; the HTTP application uses AllowLogin so a source
// bucket is checked before the account lookup and password hash.
func (s *Store) Allow(ctx context.Context, canonicalEmail string) (bool, error) {
	return s.allowBusinessBuckets(ctx, "login", []rateLimitSpec{
		s.globalSpec(s.globalCapacity, s.globalRefill),
		emailSpec(canonicalEmail, s.emailCapacity, s.emailRefill),
	})
}

// AllowLogin consumes one source attempt when the request reaches the login
// boundary. The global and email buckets are decremented only together after
// the source bucket is available. Thus an email denial cannot drain the shared
// resource bucket, while source abuse still consumes source capacity.
func (s *Store) AllowLogin(ctx context.Context, sourceIP, canonicalEmail string) (bool, error) {
	return s.allowAttemptAndBuckets(
		ctx,
		"login",
		ipSpec(sourceIP, s.loginIPCapacity, s.loginIPRefill),
		[]rateLimitSpec{
			s.globalSpec(s.globalCapacity, s.globalRefill),
			emailSpec(canonicalEmail, s.emailCapacity, s.emailRefill),
		},
	)
}

func (s *Store) AllowPasswordReset(ctx context.Context, canonicalEmail string) (bool, error) {
	return s.allowBusinessBuckets(ctx, "password-reset", []rateLimitSpec{
		s.globalSpec(s.resetGlobalCapacity, s.resetGlobalRefill),
		emailSpec(canonicalEmail, s.resetEmailCapacity, s.resetEmailRefill),
	})
}

func (s *Store) AllowPasswordResetFromSource(ctx context.Context, sourceIP, canonicalEmail string) (bool, error) {
	return s.allowAttemptAndBuckets(
		ctx,
		"password-reset",
		ipSpec(sourceIP, s.resetIPCapacity, s.resetIPRefill),
		[]rateLimitSpec{
			s.globalSpec(s.resetGlobalCapacity, s.resetGlobalRefill),
			emailSpec(canonicalEmail, s.resetEmailCapacity, s.resetEmailRefill),
		},
	)
}

func (s *Store) AllowPasswordResetComplete(ctx context.Context, sourceIP string) (bool, error) {
	return s.allowBusinessBuckets(ctx, "password-reset-complete", []rateLimitSpec{
		ipSpec(sourceIP, s.resetCompleteIPCapacity, s.resetCompleteIPRefill),
	})
}

func (s *Store) AllowInvitationAccept(ctx context.Context, sourceIP string) (bool, error) {
	return s.allowBusinessBuckets(ctx, "invitation-accept", []rateLimitSpec{
		ipSpec(sourceIP, s.invitationAcceptIPCapacity, s.invitationAcceptIPRefill),
	})
}

func (s *Store) AllowSetup(ctx context.Context, sourceIP string) (bool, error) {
	return s.allowBusinessBuckets(ctx, "setup", []rateLimitSpec{
		ipSpec(sourceIP, s.setupIPCapacity, s.setupIPRefill),
	})
}

// AllowInvitationSend is shared by invitation creation and resend/renewal.
// Actor and recipient are separate buckets, so rotating recipients cannot
// bypass the actor quota and rotating actors cannot bypass one recipient's
// delivery protection.
func (s *Store) AllowInvitationSend(ctx context.Context, actorID, canonicalEmail string) (bool, error) {
	return s.allowBusinessBuckets(ctx, "invitation-send", []rateLimitSpec{
		actorSpec(actorID, s.invitationActorCapacity, s.invitationActorRefill),
		recipientSpec(canonicalEmail, s.invitationRecipientCapacity, s.invitationRecipientRefill),
	})
}

func (s *Store) AllowTestEmail(ctx context.Context, actorID, canonicalEmail string) (bool, error) {
	return s.allowBusinessBuckets(ctx, "test-email", []rateLimitSpec{
		s.globalSpec(s.testMailGlobalCapacity, s.testMailGlobalRefill),
		actorSpec(actorID, s.testMailActorCapacity, s.testMailActorRefill),
		recipientSpec(canonicalEmail, s.testMailRecipientCapacity, s.testMailRecipientRefill),
	})
}

// AllowEmailChange charges both the authenticated account and destination
// address. This prevents switching destinations to evade an actor quota and
// switching accounts to evade protection for one recipient.
func (s *Store) AllowEmailChange(ctx context.Context, actorID, canonicalEmail string) (bool, error) {
	return s.allowBusinessBuckets(ctx, "email-change", []rateLimitSpec{
		actorSpec(actorID, s.emailChangeActorCapacity, s.emailChangeActorRefill),
		recipientSpec(canonicalEmail, s.emailChangeRecipientCapacity, s.emailChangeRecipientRefill),
	})
}

func (s *Store) allowAttemptAndBuckets(ctx context.Context, namespace string, attempt rateLimitSpec, business []rateLimitSpec) (bool, error) {
	if err := validateRateLimitSpecs(append([]rateLimitSpec{attempt}, business...)); err != nil {
		return false, err
	}
	operationCtx, cancel := s.stateContext(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(operationCtx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	// Every source-aware operation acquires its source row before its shared
	// business rows. Requests in the same namespace use the same order, and the
	// business-only namespaces use the sorted order below, preventing cycles
	// when two actors target one another's recipients.
	attemptBucket, err := ensureRateLimitBucket(operationCtx, tx, namespace, attempt.kind, attempt.digest, attempt.capacity, bucketTTL(attempt.capacity, attempt.refill))
	if err != nil {
		return false, err
	}
	var now time.Time
	if err := tx.QueryRowContext(operationCtx, `SELECT date_trunc('milliseconds', clock_timestamp())`).Scan(&now); err != nil {
		return false, err
	}
	attemptBucket.tokens, attemptBucket.lastRefilled = refillRateLimitBucket(attemptBucket.tokens, attemptBucket.lastRefilled, attempt.capacity, attempt.refill, now)
	if attemptBucket.tokens < 1 {
		// Do not even create or lock business buckets for a source that is
		// already exhausted. This keeps the source gate cheap and ensures a
		// rejected attempt has no business-bucket side effects.
		if err := updateRateLimitBucket(operationCtx, tx, namespace, attempt.kind, attempt.digest, attemptBucket, bucketTTL(attempt.capacity, attempt.refill)); err != nil {
			return false, err
		}
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return false, nil
	}

	businessBuckets, err := ensureBuckets(operationCtx, tx, namespace, business)
	if err != nil {
		return false, err
	}
	for index := range businessBuckets {
		bucket := &businessBuckets[index]
		bucket.bucket.tokens, bucket.bucket.lastRefilled = refillRateLimitBucket(bucket.bucket.tokens, bucket.bucket.lastRefilled, bucket.spec.capacity, bucket.spec.refill, now)
	}

	allowed := true
	for _, bucket := range businessBuckets {
		if bucket.bucket.tokens < 1 {
			allowed = false
			break
		}
	}
	// A reached source attempt is charged even when an email/global business
	// bucket denies the operation. The business buckets are only charged as an
	// atomic accepted operation.
	attemptBucket.tokens--
	if err := updateRateLimitBucket(operationCtx, tx, namespace, attempt.kind, attempt.digest, attemptBucket, bucketTTL(attempt.capacity, attempt.refill)); err != nil {
		return false, err
	}
	for index := range businessBuckets {
		bucket := businessBuckets[index]
		if allowed {
			bucket.bucket.tokens--
		}
		if err := updateRateLimitBucket(operationCtx, tx, namespace, bucket.spec.kind, bucket.spec.digest, bucket.bucket, bucketTTL(bucket.spec.capacity, bucket.spec.refill)); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return allowed, nil
}

func (s *Store) allowBusinessBuckets(ctx context.Context, namespace string, specs []rateLimitSpec) (bool, error) {
	if err := validateRateLimitSpecs(specs); err != nil {
		return false, err
	}
	operationCtx, cancel := s.stateContext(ctx)
	defer cancel()
	tx, err := s.db.BeginTx(operationCtx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	buckets, err := ensureBuckets(operationCtx, tx, namespace, specs)
	if err != nil {
		return false, err
	}
	var now time.Time
	if err := tx.QueryRowContext(operationCtx, `SELECT date_trunc('milliseconds', clock_timestamp())`).Scan(&now); err != nil {
		return false, err
	}
	for index := range buckets {
		bucket := &buckets[index]
		bucket.bucket.tokens, bucket.bucket.lastRefilled = refillRateLimitBucket(bucket.bucket.tokens, bucket.bucket.lastRefilled, bucket.spec.capacity, bucket.spec.refill, now)
	}
	allowed := true
	for _, bucket := range buckets {
		if bucket.bucket.tokens < 1 {
			allowed = false
			break
		}
	}
	for index := range buckets {
		bucket := buckets[index]
		if allowed {
			bucket.bucket.tokens--
		}
		if err := updateRateLimitBucket(operationCtx, tx, namespace, bucket.spec.kind, bucket.spec.digest, bucket.bucket, bucketTTL(bucket.spec.capacity, bucket.spec.refill)); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return allowed, nil
}

type refilledBucket struct {
	spec   rateLimitSpec
	bucket rateLimitBucket
}

func ensureBuckets(ctx context.Context, tx *sql.Tx, namespace string, specs []rateLimitSpec) ([]refilledBucket, error) {
	ordered := append([]rateLimitSpec(nil), specs...)
	sort.Slice(ordered, func(i, j int) bool {
		leftRank, rightRank := rateLimitKindRank(ordered[i].kind), rateLimitKindRank(ordered[j].kind)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return bytes.Compare(ordered[i].digest, ordered[j].digest) < 0
	})
	result := make([]refilledBucket, 0, len(ordered))
	for _, spec := range ordered {
		bucket, err := ensureRateLimitBucket(ctx, tx, namespace, spec.kind, spec.digest, spec.capacity, bucketTTL(spec.capacity, spec.refill))
		if err != nil {
			return nil, err
		}
		result = append(result, refilledBucket{spec: spec, bucket: bucket})
	}
	return result, nil
}

func rateLimitKindRank(kind string) int {
	// Keep the historical global-before-email order. ResetEmail deletes an
	// email row independently; taking global first prevents a limiter request
	// that is waiting on global from holding email and deadlocking that reset.
	switch kind {
	case rateLimitKindGlobal:
		return 0
	case rateLimitKindIP:
		return 1
	case rateLimitKindEmail:
		return 2
	case rateLimitKindActor:
		return 3
	case rateLimitKindRecipient:
		return 4
	default:
		return 5
	}
}

func validateRateLimitSpecs(specs []rateLimitSpec) error {
	if len(specs) == 0 {
		return fmt.Errorf("rate-limit operation has no buckets")
	}
	for _, spec := range specs {
		if spec.kind == "" || len(spec.digest) != sha256.Size || spec.capacity <= 0 || spec.refill <= 0 {
			return fmt.Errorf("invalid rate-limit settings")
		}
		if spec.capacity >= int((time.Duration(1<<63-1) / spec.refill)) {
			return fmt.Errorf("rate-limit retention overflows")
		}
	}
	return nil
}

func (s *Store) globalSpec(capacity int, refill time.Duration) rateLimitSpec {
	return rateLimitSpec{kind: rateLimitKindGlobal, digest: append([]byte(nil), globalRateLimitDigest...), capacity: capacity, refill: refill}
}

func ipSpec(value string, capacity int, refill time.Duration) rateLimitSpec {
	return identitySpec(rateLimitKindIP, normalizeIPIdentity(value), capacity, refill)
}

func emailSpec(value string, capacity int, refill time.Duration) rateLimitSpec {
	return identitySpec(rateLimitKindEmail, value, capacity, refill)
}

func actorSpec(value string, capacity int, refill time.Duration) rateLimitSpec {
	return identitySpec(rateLimitKindActor, value, capacity, refill)
}

func recipientSpec(value string, capacity int, refill time.Duration) rateLimitSpec {
	return identitySpec(rateLimitKindRecipient, value, capacity, refill)
}

func identitySpec(kind, value string, capacity int, refill time.Duration) rateLimitSpec {
	return rateLimitSpec{kind: kind, digest: rateLimitDigest(normalizeLimiterIdentity(value)), capacity: capacity, refill: refill}
}

func normalizeLimiterIdentity(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "unknown"
	}
	return value
}

func normalizeIPIdentity(value string) string {
	parsed := net.ParseIP(strings.TrimSpace(value))
	if parsed == nil {
		return "unknown"
	}
	if ipv4 := parsed.To4(); ipv4 != nil {
		return ipv4.String()
	}
	return parsed.String()
}

func (s *Store) ResetEmail(ctx context.Context, canonicalEmail string) error {
	operationCtx, cancel := s.stateContext(ctx)
	defer cancel()
	_, err := s.db.ExecContext(operationCtx, `
		DELETE FROM auth_rate_limit_buckets
		WHERE namespace = 'login' AND bucket_kind = 'email' AND bucket_digest = $1::bytea`, rateLimitDigest(normalizeLimiterIdentity(canonicalEmail)))
	return err
}

func ensureRateLimitBucket(ctx context.Context, tx *sql.Tx, namespace, kind string, digest []byte, capacity int, ttl time.Duration) (rateLimitBucket, error) {
	var bucket rateLimitBucket
	// DO UPDATE is intentional: unlike DO NOTHING, it locks an existing
	// conflicting row and keeps that lock until the surrounding transaction
	// commits. The no-op update makes creation and locking one operation, so a
	// reset or cleanup cannot delete the row before the caller consumes it.
	err := tx.QueryRowContext(ctx, `
		WITH db_clock AS (SELECT date_trunc('milliseconds', clock_timestamp()) AS now)
		INSERT INTO auth_rate_limit_buckets (
			namespace, bucket_kind, bucket_digest, tokens, last_refill_at, expires_at
		)
		SELECT $1, $2, $3::bytea, $4::bigint, now,
			now + ($5::bigint * INTERVAL '1 millisecond')
		FROM db_clock
		ON CONFLICT (namespace, bucket_kind, bucket_digest) DO UPDATE
		SET tokens = auth_rate_limit_buckets.tokens
		RETURNING tokens, last_refill_at`, namespace, kind, digest, capacity, durationMillis(ttl)).Scan(&bucket.tokens, &bucket.lastRefilled)
	if err != nil {
		return rateLimitBucket{}, err
	}
	bucket.lastRefilled = bucket.lastRefilled.Truncate(time.Millisecond)
	return bucket, nil
}

func updateRateLimitBucket(ctx context.Context, tx *sql.Tx, namespace, kind string, digest []byte, bucket rateLimitBucket, ttl time.Duration) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE auth_rate_limit_buckets
		SET tokens = $4::bigint,
			last_refill_at = $5::timestamptz,
			expires_at = $5::timestamptz + ($6::bigint * INTERVAL '1 millisecond')
		WHERE namespace = $1 AND bucket_kind = $2 AND bucket_digest = $3::bytea`, namespace, kind, digest, bucket.tokens, bucket.lastRefilled, durationMillis(ttl))
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected != 1 {
		return fmt.Errorf("rate-limit bucket disappeared during update")
	}
	return nil
}

func refillRateLimitBucket(tokens int64, last time.Time, capacity int, interval time.Duration, now time.Time) (int64, time.Time) {
	// A capacity change must take effect on the next check even when no full
	// refill interval has elapsed. This conservatively clips old excess tokens
	// rather than letting a relaxed or tightened deployment keep stale capacity.
	if tokens > int64(capacity) {
		tokens = int64(capacity)
	}
	if interval <= 0 || !now.After(last) {
		return tokens, last
	}
	elapsed := now.Sub(last)
	if elapsed < interval {
		return tokens, last
	}
	additions := int64(elapsed / interval)
	if tokens+additions > int64(capacity) {
		tokens = int64(capacity)
	} else {
		tokens += additions
	}
	return tokens, last.Add(time.Duration(additions) * interval)
}

func rateLimitDigest(identity string) []byte {
	digest := sha256.Sum256([]byte(identity))
	return digest[:]
}

func bucketTTL(capacity int, refill time.Duration) time.Duration {
	if capacity <= 0 || refill <= 0 {
		return time.Second
	}
	maxDuration := time.Duration(1<<63 - 1)
	maxIntervals := int64(maxDuration / refill)
	if int64(capacity) >= maxIntervals {
		return maxDuration
	}
	return time.Duration(int64(capacity)+1) * refill
}

var (
	_ application.LoginLimiter                    = (*Store)(nil)
	_ application.SourceAwareLoginLimiter         = (*Store)(nil)
	_ application.PasswordResetLimiter            = (*Store)(nil)
	_ application.SourceAwarePasswordResetLimiter = (*Store)(nil)
	_ application.SetupLimiter                    = (*Store)(nil)
	_ application.InvitationAcceptLimiter         = (*Store)(nil)
	_ application.InvitationSendLimiter           = (*Store)(nil)
	_ application.TestEmailLimiter                = (*Store)(nil)
	_ application.EmailChangeLimiter              = (*Store)(nil)
)
