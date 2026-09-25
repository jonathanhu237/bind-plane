package password

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
)

func TestHasher(t *testing.T) {
	hasher, err := NewHasher(1)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := hasher.Hash(context.Background(), "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=65536,t=3,p=4$") {
		t.Fatalf("unexpected PHC string: %s", hash)
	}
	if ok, err := hasher.Verify(context.Background(), hash, "correct horse battery staple"); err != nil || !ok {
		t.Fatalf("Verify(correct) = %t, %v", ok, err)
	}
	if ok, err := hasher.Verify(context.Background(), hash, "wrong password"); err != nil || ok {
		t.Fatalf("Verify(wrong) = %t, %v", ok, err)
	}
	if hash2, err := hasher.Hash(context.Background(), "correct horse battery staple"); err != nil || hash2 == hash {
		t.Fatalf("independent salt hash = %q, %v", hash2, err)
	}
}

func TestHasherRejectsCancellationAndSaturationBeforeAllocation(t *testing.T) {
	hasher, err := NewHasher(1)
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := hasher.Hash(canceled, "correct horse battery staple"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Hash(canceled) error = %v, want context canceled", err)
	}
	hasher.sem <- struct{}{}
	defer func() { <-hasher.sem }()
	if _, err := hasher.Hash(context.Background(), "correct horse battery staple"); !errors.Is(err, application.ErrPasswordHashBusy) {
		t.Fatalf("Hash(saturated) error = %v, want busy", err)
	}
}

func TestParserBounds(t *testing.T) {
	for _, value := range []string{"", strings.Repeat("x", phcMaxLen+1), "$argon2id$v=19$m=1,t=1,p=1$abc$abc", "$argon2i$v=19$m=65536,t=3,p=4$abc$abc"} {
		if _, err := parsePHC(value); err == nil {
			t.Errorf("parsePHC(%q) accepted invalid value", value)
		}
	}
}

func BenchmarkHasher(b *testing.B) {
	hasher, err := NewHasher(1)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := hasher.Hash(context.Background(), "correct horse battery staple"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVerifier(b *testing.B) {
	hasher, err := NewHasher(1)
	if err != nil {
		b.Fatal(err)
	}
	hash, err := hasher.Hash(context.Background(), "correct horse battery staple")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if ok, err := hasher.Verify(context.Background(), hash, "correct horse battery staple"); err != nil || !ok {
			b.Fatalf("Verify() = %t, %v", ok, err)
		}
	}
}
