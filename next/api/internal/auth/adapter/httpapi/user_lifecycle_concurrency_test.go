package httpapi

import (
	"encoding/json"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/adapter/password"
	postgresadapter "github.com/jonathanhu237/bind-plane/next/api/internal/auth/adapter/postgres"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestUserLifecycleConcurrentHTTPIntegration(t *testing.T) {
	for _, pair := range []string{"deactivate", "delete", "remove-role"} {
		t.Run(pair, func(t *testing.T) {
			db, ctx := openHTTPIntegrationDatabase(t)
			cfg := testConfig()
			cfg.LoginGlobalCapacity = 100
			cfg.LoginEmailCapacity = 100
			cfg.LoginGlobalRefillInterval = time.Millisecond
			store := postgresadapter.NewStore(db, cfg)
			hasher, err := password.NewHasher(2)
			if err != nil {
				t.Fatal(err)
			}
			f := createOnlineHTTPFixture(t, ctx, db, hasher)
			if _, err = db.ExecContext(ctx, `INSERT INTO auth_role_permissions(role_id,permission_key) VALUES($1,'users.read'),($1,'users.write'),($1,'roles.read') ON CONFLICT DO NOTHING`, f.roleID); err != nil {
				t.Fatal(err)
			}
			if _, err = db.ExecContext(ctx, `INSERT INTO auth_user_roles(user_id,role_id) SELECT $1::uuid,id FROM auth_roles WHERE system_key='super_admin'`, f.otherID); err != nil {
				t.Fatal(err)
			}
			auth := application.NewAuthentication(store, hasher, store, store, application.CryptoRandom(), domain.DefaultPermissionCatalog())
			access := application.NewAccessManagement(store, store, domain.DefaultPermissionCatalog())
			h := NewHandlerWithAccessAndOperationLog(&setupFake{status: application.SetupComplete}, auth, cfg, nil, access, nil, nil, application.NewOperationLogService(store))
			manager := loginOnlineHTTPUser(t, h, f.managerEmail, f.managerPassword)
			target := loginOnlineHTTPUser(t, h, f.targetEmail, f.targetPassword)
			firstMethod, firstPath := http.MethodPost, "/api/users/"+f.targetID+"/deactivate"
			secondMethod, secondPath, secondBody, secondActor := http.MethodPost, "/api/users/"+f.otherID+"/deactivate", `{"authVersion":1}`, manager
			if pair == "delete" {
				firstMethod, firstPath = http.MethodDelete, "/api/users/"+f.targetID
				secondMethod, secondPath = http.MethodDelete, "/api/users/"+f.otherID
			}
			if pair == "remove-role" {
				secondMethod, secondPath, secondBody, secondActor = http.MethodPut, "/api/users/"+f.otherID+"/roles", fmt.Sprintf(`{"authVersion":1,"roleIds":[%q]}`, f.roleID), target
			}
			start := make(chan struct{})
			results := make(chan int, 2)
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-start
				results <- onlineHTTPRequest(h, firstMethod, firstPath, `{"authVersion":1}`, manager).Code
			}()
			go func() {
				defer wg.Done()
				<-start
				results <- onlineHTTPRequest(h, secondMethod, secondPath, secondBody, secondActor).Code
			}()
			close(start)
			wg.Wait()
			close(results)
			successes := 0
			for status := range results {
				if status == 200 || status == 204 {
					successes++
				} else if status != 401 && status != 403 && status != 409 {
					t.Fatalf("unexpected concurrent status: %d", status)
				}
			}
			if successes != 1 {
				t.Fatalf("successes=%d; expected exactly one protected transition", successes)
			}
			response := onlineHTTPRequest(h, http.MethodGet, "/api/users?status=active", "", manager)
			if response.Code != 200 {
				t.Fatalf("list after race: %d %s", response.Code, response.Body.String())
			}
			var page usersResponse
			if err = json.Unmarshal(response.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			available := 0
			for _, user := range page.Users {
				for _, role := range user.Roles {
					if role.System == "super_admin" {
						available++
					}
				}
			}
			if available != 1 {
				t.Fatalf("available admins=%d", available)
			}
		})
	}
}
