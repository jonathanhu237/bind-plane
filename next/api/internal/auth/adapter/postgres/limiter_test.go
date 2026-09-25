package postgres

import (
	"testing"
	"time"
)

func TestRefillRateLimitBucketClipsTokensWhenCapacityShrinks(t *testing.T) {
	last := time.Unix(100, 0)
	tokens, gotLast := refillRateLimitBucket(10, last, 3, time.Minute, last.Add(time.Second))
	if tokens != 3 || !gotLast.Equal(last) {
		t.Fatalf("clipped bucket = %d at %s, want 3 at %s", tokens, gotLast, last)
	}
}

func TestIPIdentityNormalizesIPv4MappedAddresses(t *testing.T) {
	mapped := ipSpec("::ffff:192.0.2.7", 1, time.Minute)
	plain := ipSpec("192.0.2.7", 1, time.Minute)
	if string(mapped.digest) != string(plain.digest) {
		t.Fatal("IPv4-mapped and plain addresses use different limiter identities")
	}
}

func TestBucketTTLUsesFiniteRecoveryWindow(t *testing.T) {
	if got, want := bucketTTL(3, time.Minute), 4*time.Minute; got != want {
		t.Fatalf("bucket TTL = %s, want %s", got, want)
	}
}
