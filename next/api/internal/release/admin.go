package release

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"os"
	"strings"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

type AdminHandler struct {
	handler
	db  *sql.DB
	key []byte
}

// NewAdminHandler fails closed when credential encryption is not configured.
func NewAdminHandler(db *sql.DB, identity Identity, cookie, origin string) (http.Handler, error) {
	encoded := os.Getenv("BIND_PLANE_CREDENTIAL_ENCRYPTION_KEY")
	key, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(key) != 32 {
		return nil, errors.New("BIND_PLANE_CREDENTIAL_ENCRYPTION_KEY must be a 32-byte URL-safe key")
	}
	a := &AdminHandler{handler: handler{identity: identity, cookie: cookie, origin: origin}, db: db, key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/releases/admin/assets", a.list)
	mux.HandleFunc("GET /api/releases/admin/audit", a.audit)
	mux.HandleFunc("POST /api/releases/admin/credentials", a.credential)
	mux.HandleFunc("POST /api/releases/admin/profiles", a.profile)
	mux.HandleFunc("POST /api/releases/admin/switches", a.switchAsset)
	mux.HandleFunc("POST /api/releases/admin/networks", a.network)
	return mux, nil
}
func (a *AdminHandler) authorize(w http.ResponseWriter, r *http.Request, mutate bool) (domain.Principal, bool) {
	principal, admin, ok := a.principal(w, r, mutate)
	if !ok {
		return principal, false
	}
	if !admin {
		fail(w, 403, "admin role required")
		return principal, false
	}
	return principal, true
}

func (a *AdminHandler) createAudited(ctx context.Context, actor domain.Principal, action, name, statement string, args ...any) (string, error) {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var id string
	if err = tx.QueryRowContext(ctx, statement, args...).Scan(&id); err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO bp_asset_events(actor_id,actor_email,action,object_id,object_name) VALUES ($1,$2,$3,$4,$5)`, actor.User.ID, actor.User.Email, action, id, name)
	if err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return id, nil
}
func (a *AdminHandler) seal(value string) (string, error) {
	block, err := aes.NewCipher(a.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(value), nil)
	return "v1:" + base64.RawURLEncoding.EncodeToString(ciphertext), nil
}
func (a *AdminHandler) credential(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.authorize(w, r, true)
	if !ok {
		return
	}
	var input struct{ Name, Username, Password, Secret string }
	if err := decode(r, &input); err != nil || strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Username) == "" || input.Password == "" {
		fail(w, 422, "credential name, username and password required")
		return
	}
	password, err := a.seal(input.Password)
	if err != nil {
		fail(w, 503, "encryption unavailable")
		return
	}
	var secret any
	if input.Secret != "" {
		secret, err = a.seal(input.Secret)
		if err != nil {
			fail(w, 503, "encryption unavailable")
			return
		}
	}
	id, err := a.createAudited(r.Context(), actor, "credential.create", input.Name,
		`INSERT INTO bp_credentials(name,username,encrypted_password,encrypted_secret) VALUES ($1,$2,$3,$4) RETURNING id::text`, input.Name, input.Username, password, secret)
	if err != nil {
		fail(w, 409, "credential could not be saved")
		return
	}
	write(w, 201, map[string]string{"id": id, "name": input.Name})
}
func (a *AdminHandler) profile(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.authorize(w, r, true)
	if !ok {
		return
	}
	var input struct {
		Name                string          `json:"name"`
		LoginPromptPatterns json.RawMessage `json:"login_prompt_patterns"`
		CommandTemplates    json.RawMessage `json:"command_templates"`
		PromptPatterns      json.RawMessage `json:"prompt_patterns"`
		PaginationRules     json.RawMessage `json:"pagination_rules"`
		SuccessPatterns     json.RawMessage `json:"success_patterns"`
		ErrorPatterns       json.RawMessage `json:"error_patterns"`
		ParserRules         json.RawMessage `json:"parser_rules"`
	}
	if err := decode(r, &input); err != nil || strings.TrimSpace(input.Name) == "" {
		fail(w, 422, "invalid profile")
		return
	}
	// Profiles remain device-specific data, but never accept invalid JSON types.
	for _, v := range []json.RawMessage{input.LoginPromptPatterns, input.CommandTemplates, input.PromptPatterns, input.PaginationRules, input.ParserRules} {
		if len(v) == 0 || v[0] != '{' {
			fail(w, 422, "profile rules must be objects")
			return
		}
	}
	for _, v := range []json.RawMessage{input.SuccessPatterns, input.ErrorPatterns} {
		if len(v) == 0 || v[0] != '[' {
			fail(w, 422, "patterns must be arrays")
			return
		}
	}
	id, err := a.createAudited(r.Context(), actor, "profile.create", input.Name,
		`INSERT INTO bp_command_profiles(name,login_prompt_patterns,command_templates,prompt_patterns,pagination_rules,success_patterns,error_patterns,parser_rules)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id::text`, input.Name, string(input.LoginPromptPatterns), string(input.CommandTemplates), string(input.PromptPatterns), string(input.PaginationRules), string(input.SuccessPatterns), string(input.ErrorPatterns), string(input.ParserRules))
	if err != nil {
		fail(w, 409, "profile could not be saved")
		return
	}
	write(w, 201, map[string]string{"id": id, "name": input.Name})
}
func (a *AdminHandler) switchAsset(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.authorize(w, r, true)
	if !ok {
		return
	}
	var input struct {
		Name             string `json:"name"`
		ManagementIP     string `json:"management_ip"`
		CredentialID     string `json:"credential_id"`
		CommandProfileID string `json:"command_profile_id"`
	}
	if err := decode(r, &input); err != nil || strings.TrimSpace(input.Name) == "" || !validIPv4(input.ManagementIP) || !validID(input.CredentialID) || !validID(input.CommandProfileID) {
		fail(w, 422, "invalid switch asset")
		return
	}
	id, err := a.createAudited(r.Context(), actor, "switch.create", input.Name,
		`INSERT INTO bp_switches(name,management_ip,credential_id,command_profile_id) VALUES($1,$2,$3,$4) RETURNING id::text`, input.Name, input.ManagementIP, input.CredentialID, input.CommandProfileID)
	if err != nil {
		fail(w, 409, "switch could not be saved")
		return
	}
	write(w, 201, map[string]string{"id": id, "name": input.Name})
}
func validIPv4(value string) bool {
	ip, err := netip.ParseAddr(value)
	return err == nil && ip.Is4() && ip.String() == value
}
func validID(value string) bool { return len(value) == 36 && strings.Count(value, "-") == 4 }
func (a *AdminHandler) network(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.authorize(w, r, true)
	if !ok {
		return
	}
	var input struct {
		SwitchID  string `json:"switch_id"`
		CIDR      string `json:"cidr"`
		Validated bool   `json:"is_validated"`
	}
	if err := decode(r, &input); err != nil || !validID(input.SwitchID) {
		fail(w, 422, "invalid network")
		return
	}
	prefix, err := netip.ParsePrefix(input.CIDR)
	if err != nil || !prefix.Addr().Is4() || prefix.Masked() != prefix {
		fail(w, 422, "canonical IPv4 CIDR required")
		return
	}
	id, err := a.createAudited(r.Context(), actor, "network.create", input.CIDR,
		`INSERT INTO bp_networks(switch_id,cidr,is_validated) VALUES($1,$2,$3) RETURNING id::text`, input.SwitchID, input.CIDR, input.Validated)
	if err != nil {
		fail(w, 409, "network could not be saved")
		return
	}
	write(w, 201, map[string]string{"id": id, "cidr": input.CIDR})
}
func (a *AdminHandler) list(w http.ResponseWriter, r *http.Request) {
	_, ok := a.authorize(w, r, false)
	if !ok {
		return
	}
	result := map[string]any{}
	queries := map[string]string{
		"credentials": `SELECT id::text,name,username,is_active FROM bp_credentials ORDER BY name`,
		"profiles":    `SELECT id::text,name,is_active FROM bp_command_profiles ORDER BY name`,
		"switches":    `SELECT id::text,name,management_ip::text,credential_id::text,command_profile_id::text,is_enabled FROM bp_switches ORDER BY name`,
		"networks":    `SELECT id::text,switch_id::text,cidr::text,is_enabled,is_validated FROM bp_networks ORDER BY cidr`,
	}
	for category, query := range queries {
		rows, err := a.db.QueryContext(r.Context(), query)
		if err != nil {
			fail(w, 503, "asset store unavailable")
			return
		}
		items := []map[string]any{}
		names := map[string][]string{"credentials": {"id", "name", "username", "is_active"}, "profiles": {"id", "name", "is_active"}, "switches": {"id", "name", "management_ip", "credential_id", "command_profile_id", "is_enabled"}, "networks": {"id", "switch_id", "cidr", "is_enabled", "is_validated"}}[category]
		for rows.Next() {
			values := make([]any, len(names))
			ptrs := make([]any, len(names))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err = rows.Scan(ptrs...); err != nil {
				break
			}
			item := map[string]any{}
			for i, key := range names {
				item[key] = values[i]
			}
			items = append(items, item)
		}
		if err == nil {
			err = rows.Err()
		}
		_ = rows.Close()
		if err != nil {
			fail(w, 503, "asset store unavailable")
			return
		}
		result[category] = items
	}
	write(w, 200, result)
}
