package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestSystemIdentityIntegrationPersistsOneNameAndHasNoLegacyColumn(t *testing.T) {
	dsn := integrationDSN(t)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	store := newTestStore(db)
	management := application.NewSystemIdentityManagement(store)
	original, originalErr := store.GetSystemIdentity(ctx)
	if originalErr != nil && !errors.Is(originalErr, application.ErrSystemIdentityNotConfigured) {
		_ = db.Close()
		t.Fatal(originalErr)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if originalErr == nil {
			if _, err := store.SaveSystemIdentity(cleanupCtx, mustCurrentRevision(cleanupCtx, t, db), original); err != nil {
				t.Errorf("restore system identity: %v", err)
			}
		} else if _, err := db.ExecContext(cleanupCtx, `DELETE FROM auth_system_identity WHERE singleton = true`); err != nil {
			t.Errorf("remove system identity: %v", err)
		}
		_ = db.Close()
	})

	var legacyColumn bool
	if err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = 'auth_system_identity'
			  AND column_name = 'english_system_name'
		)`).Scan(&legacyColumn); err != nil {
		t.Fatal(err)
	}
	if legacyColumn {
		t.Fatal("system identity schema still contains the removed English name column")
	}

	current := original
	if errors.Is(originalErr, application.ErrSystemIdentityNotConfigured) {
		current = application.SystemIdentityRecord{Revision: 0}
	}
	saved, err := management.SaveSystemIdentity(ctx, application.SystemIdentityInput{
		SystemName: "  品牌 Admin <主站> & Co  ",
		IconAction: application.SystemIconPreserve,
		Revision:   current.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.SystemName != "品牌 Admin <主站> & Co" || saved.DisplayName() != saved.SystemName {
		t.Fatalf("saved identity = %#v", saved)
	}
	loaded, err := management.GetSystemIdentity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SystemName != saved.SystemName || loaded.DisplayName() != saved.SystemName {
		t.Fatalf("loaded identity = %#v, saved = %#v", loaded, saved)
	}
}

func mustCurrentRevision(ctx context.Context, t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var revision int64
	if err := db.QueryRowContext(ctx, `SELECT revision FROM auth_system_identity WHERE singleton = true`).Scan(&revision); err != nil {
		t.Errorf("read current system identity revision: %v", err)
		return 0
	}
	return revision
}
