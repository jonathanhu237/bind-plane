package release

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestBusinessRolesAreProtectedWhileOtherRolesUpdate(t *testing.T) {
	dsn := os.Getenv("BIND_PLANE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated test database required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for _, statement := range []string{
		`UPDATE auth_roles SET description='changed' WHERE name_canonical='operator'`,
		`DELETE FROM auth_roles WHERE name_canonical='admin'`,
	} {
		if _, err = db.ExecContext(ctx, statement); err == nil || !strings.Contains(err.Error(), "cannot be changed") {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	// The guard must not turn updates of any other role into silent no-ops.
	var id, description string
	if err = db.QueryRowContext(ctx, `INSERT INTO auth_roles(name,name_canonical,description) VALUES ('Trigger probe','trigger probe','before') RETURNING id::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = db.ExecContext(ctx, `DELETE FROM auth_roles WHERE id=$1`, id) }()
	if err = db.QueryRowContext(ctx, `UPDATE auth_roles SET description='after' WHERE id=$1 RETURNING description`, id).Scan(&description); err != nil || description != "after" {
		t.Fatalf("custom role update stored %q: %v", description, err)
	}
}
