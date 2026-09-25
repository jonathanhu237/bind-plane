package postgres

import (
	"bytes"
	"database/sql"
	"errors"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
	"github.com/jonathanhu237/bind-plane/next/api/internal/config"
)

func newTestStore(db *sql.DB) *Store {
	store := NewStore(db)
	box, _ := application.NewMailTaskSecretBox(bytes.Repeat([]byte{0x7d}, 32))
	store.SetMailTaskSecretBox(box)
	return store
}

func TestNewStoreUsesStableMailTaskKey(t *testing.T) {
	passwordKey := bytes.Repeat([]byte{0x11}, 32)
	first := NewStore(nil, config.Config{PasswordResetTokenKey: passwordKey, EmailSettingsEncryptionKey: bytes.Repeat([]byte{0x22}, 32)})
	second := NewStore(nil, config.Config{PasswordResetTokenKey: passwordKey, EmailSettingsEncryptionKey: bytes.Repeat([]byte{0x33}, 32)})
	material := application.MailTaskMaterial{
		Version:    1,
		Kind:       application.MailPasswordChanged,
		Name:       "Ada",
		Email:      "ada@example.com",
		Locale:     domain.LocaleEnglish,
		SystemName: "Temvia",
		CreatedAt:  time.Unix(100, 0),
		ExpiresAt:  time.Unix(200, 0),
	}
	ciphertext, err := application.SealMailTaskMaterial(first.mailTaskSecretBox, material)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.OpenMailTaskMaterial(second.mailTaskSecretBox, ciphertext); err != nil {
		t.Fatalf("mail task key changed with SMTP settings key: %v", err)
	}
}

func TestStoreFailsClosedWhenConfiguredTaskKeyIsUnavailable(t *testing.T) {
	store := NewStore(nil, config.Config{PasswordResetTokenKey: bytes.Repeat([]byte{0x11}, 32)})
	store.SetMailTaskSecretBox(nil)
	_, err := store.sealMailTaskMaterial(application.MailTaskMaterial{
		Version:    1,
		Kind:       application.MailPasswordChanged,
		Name:       "Ada",
		Email:      "ada@example.com",
		Locale:     domain.LocaleEnglish,
		SystemName: "Temvia",
		CreatedAt:  time.Unix(100, 0),
		ExpiresAt:  time.Unix(200, 0),
	})
	if !errors.Is(err, application.ErrDependencyUnavailable) {
		t.Fatalf("nil configured task key error = %v", err)
	}
}

func TestExpectedMigrationVersionMatchesBundledFiles(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate test source")
	}
	files, err := filepath.Glob(filepath.Join(filepath.Dir(source), "..", "..", "..", "..", "migrations", "*.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	var highest int64
	for _, file := range files {
		prefix, _, ok := strings.Cut(filepath.Base(file), "_")
		if !ok {
			t.Fatalf("migration filename has no version prefix: %s", file)
		}
		version, err := strconv.ParseInt(prefix, 10, 64)
		if err != nil {
			t.Fatalf("migration filename has invalid version: %s", file)
		}
		if version > highest {
			highest = version
		}
	}
	if highest != ExpectedMigrationVersion {
		t.Fatalf("highest migration version = %d, expected constant = %d", highest, ExpectedMigrationVersion)
	}
	downFiles, err := filepath.Glob(filepath.Join(filepath.Dir(source), "..", "..", "..", "..", "migrations", "*.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	downByName := make(map[string]struct{}, len(downFiles))
	for _, file := range downFiles {
		downByName[strings.TrimSuffix(filepath.Base(file), ".down.sql")] = struct{}{}
	}
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".up.sql")
		if _, ok := downByName[name]; !ok {
			t.Fatalf("migration %s has no matching down file", filepath.Base(file))
		}
	}
	if len(downByName) != len(files) {
		t.Fatalf("migration up/down file count differs: %d up, %d down", len(files), len(downByName))
	}
}
