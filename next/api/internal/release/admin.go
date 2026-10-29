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

// credentialKey fails closed when credential encryption is not configured. The same
// key seals configuration backups, under a separate AEAD label.
func credentialKey() ([]byte, error) {
	key, err := base64.RawURLEncoding.DecodeString(os.Getenv("BIND_PLANE_CREDENTIAL_ENCRYPTION_KEY"))
	if err != nil || len(key) != 32 {
		return nil, errors.New("BIND_PLANE_CREDENTIAL_ENCRYPTION_KEY must be a 32-byte URL-safe key")
	}
	return key, nil
}

func newAdminCore(db *sql.DB, identity Identity, cookie, origin string) (*AdminHandler, error) {
	key, err := credentialKey()
	if err != nil {
		return nil, err
	}
	return &AdminHandler{handler: handler{identity: identity, cookie: cookie, origin: origin}, db: db, key: key}, nil
}

func NewAdminHandler(db *sql.DB, identity Identity, cookie, origin string) (http.Handler, error) {
	a, err := newAdminCore(db, identity, cookie, origin)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/releases/admin/assets", a.list)
	mux.HandleFunc("GET /api/releases/admin/audit", a.audit)
	mux.HandleFunc("POST /api/releases/admin/credentials", a.credential)
	mux.HandleFunc("POST /api/releases/admin/profiles", a.profile)
	mux.HandleFunc("POST /api/releases/admin/switches", a.switchAsset)
	mux.HandleFunc("POST /api/releases/admin/networks", a.network)
	mux.HandleFunc("PATCH /api/releases/admin/credentials/{id}", a.editCredential)
	mux.HandleFunc("PATCH /api/releases/admin/profiles/{id}", a.editProfile)
	mux.HandleFunc("PATCH /api/releases/admin/switches/{id}", a.editSwitch)
	mux.HandleFunc("PATCH /api/releases/admin/networks/{id}", a.editNetwork)
	mux.HandleFunc("POST /api/releases/admin/groups", a.group)
	mux.HandleFunc("PATCH /api/releases/admin/groups/{id}", a.editGroup)
	mux.HandleFunc("POST /api/releases/admin/import", a.importAssets)
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
		Vendor           string `json:"vendor"`
		Model            string `json:"model"`
		Site             string `json:"site"`
		Description      string `json:"description"`
		GroupID          string `json:"group_id"`
		Protocol         string `json:"protocol"`
		Port             *int   `json:"port"`
	}
	err := decode(r, &input)
	input.Name, input.Vendor, input.Protocol = strings.TrimSpace(input.Name), defaultString(input.Vendor, "other"), defaultString(input.Protocol, "telnet")
	if err != nil || input.Name == "" || !validIPv4(input.ManagementIP) || !validID(input.CredentialID) || !validID(input.CommandProfileID) || !validSwitchMeta(input.Vendor, input.Protocol, input.Port, input.GroupID) {
		fail(w, 422, "invalid switch asset")
		return
	}
	id, err := a.createAudited(r.Context(), actor, "switch.create", input.Name,
		`INSERT INTO bp_switches(name,management_ip,credential_id,command_profile_id,vendor,model,site,description,group_id,protocol,port)
 VALUES($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,''),NULLIF($8,''),NULLIF($9,'')::uuid,$10,$11) RETURNING id::text`,
		input.Name, input.ManagementIP, input.CredentialID, input.CommandProfileID, input.Vendor, strings.TrimSpace(input.Model), strings.TrimSpace(input.Site), strings.TrimSpace(input.Description), input.GroupID, input.Protocol, input.Port)
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

var switchVendors = map[string]bool{"huawei": true, "h3c": true, "ruijie": true, "cisco": true, "other": true}

func validSwitchMeta(vendor, protocol string, port *int, groupID string) bool {
	return switchVendors[vendor] && (protocol == "telnet" || protocol == "ssh") &&
		(port == nil || *port >= 1 && *port <= 65535) && (groupID == "" || domain.IsCanonicalUUID(groupID))
}

func defaultString(value, fallback string) string {
	if value = strings.TrimSpace(value); value == "" {
		return fallback
	}
	return value
}

func (a *AdminHandler) group(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.authorize(w, r, true)
	if !ok {
		return
	}
	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := decode(r, &input); err != nil || strings.TrimSpace(input.Name) == "" {
		fail(w, 422, "group name required")
		return
	}
	name := strings.TrimSpace(input.Name)
	id, err := a.createAudited(r.Context(), actor, "group.create", name,
		`INSERT INTO bp_switch_groups(name,description) VALUES($1,NULLIF($2,'')) RETURNING id::text`, name, strings.TrimSpace(input.Description))
	if err != nil {
		fail(w, 409, "group could not be saved")
		return
	}
	write(w, 201, map[string]string{"id": id, "name": name})
}
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
	// Secrets are never listed, only whether an optional privilege secret is stored.
	queries := map[string]string{
		"credentials": `SELECT id::text,name,username,description,is_active,encrypted_secret IS NOT NULL,version FROM bp_credentials ORDER BY name`,
		"profiles":    `SELECT id::text,name,is_active,version,login_prompt_patterns::text,command_templates::text,prompt_patterns::text,pagination_rules::text,success_patterns::text,error_patterns::text,parser_rules::text FROM bp_command_profiles ORDER BY name`,
		"switches":    `SELECT id::text,name,host(management_ip),credential_id::text,command_profile_id::text,is_enabled,version,vendor,model,site,description,group_id::text,protocol,port FROM bp_switches ORDER BY name`,
		"networks":    `SELECT id::text,switch_id::text,cidr::text,is_enabled,is_validated,vlan,description,version FROM bp_networks ORDER BY cidr`,
		"groups":      `SELECT id::text,name,description,version FROM bp_switch_groups ORDER BY name`,
	}
	// Profile rules are read as text and passed through as the JSON they are.
	jsonColumns := map[string]bool{}
	for _, field := range profileFields.fields {
		jsonColumns[field.column] = field.json
	}
	for category, query := range queries {
		rows, err := a.db.QueryContext(r.Context(), query)
		if err != nil {
			fail(w, 503, "asset store unavailable")
			return
		}
		items := []map[string]any{}
		names := map[string][]string{
			"credentials": {"id", "name", "username", "description", "is_active", "has_secret", "version"},
			"profiles":    {"id", "name", "is_active", "version", "login_prompt_patterns", "command_templates", "prompt_patterns", "pagination_rules", "success_patterns", "error_patterns", "parser_rules"},
			"switches":    {"id", "name", "management_ip", "credential_id", "command_profile_id", "is_enabled", "version", "vendor", "model", "site", "description", "group_id", "protocol", "port"},
			"networks":    {"id", "switch_id", "cidr", "is_enabled", "is_validated", "vlan", "description", "version"},
			"groups":      {"id", "name", "description", "version"},
		}[category]
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
				if text, ok := values[i].(string); ok && category == "profiles" && jsonColumns[key] {
					item[key] = json.RawMessage(text)
				}
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
