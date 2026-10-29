package release

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"reflect"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

var errStaleAsset = errors.New("asset changed since it was loaded; reload and try again")

// assetField is an editable column. Names are code constants and never come from a request.
type assetField struct {
	column, read string // read selects the stored value for comparison
	json, secret bool   // secret values never enter the audit record
}
type assetKind struct {
	table, action, label string // label selects the audit object name
	fields               []assetField
}

var (
	credentialFields = assetKind{"bp_credentials", "credential.update", "name", []assetField{
		{column: "name", read: "name"}, {column: "username", read: "username"},
		{column: "description", read: "description"}, {column: "is_active", read: "is_active"},
		{column: "encrypted_password", read: "encrypted_password IS NOT NULL", secret: true},
		{column: "encrypted_secret", read: "encrypted_secret IS NOT NULL", secret: true},
	}}
	profileFields = assetKind{"bp_command_profiles", "profile.update", "name", []assetField{
		{column: "name", read: "name"}, {column: "is_active", read: "is_active"},
		{column: "login_prompt_patterns", read: "login_prompt_patterns::text", json: true},
		{column: "command_templates", read: "command_templates::text", json: true},
		{column: "prompt_patterns", read: "prompt_patterns::text", json: true},
		{column: "pagination_rules", read: "pagination_rules::text", json: true},
		{column: "success_patterns", read: "success_patterns::text", json: true},
		{column: "error_patterns", read: "error_patterns::text", json: true},
		{column: "parser_rules", read: "parser_rules::text", json: true},
	}}
	switchFields = assetKind{"bp_switches", "switch.update", "name", []assetField{
		{column: "name", read: "name"}, {column: "management_ip", read: "host(management_ip)"},
		{column: "credential_id", read: "credential_id::text"}, {column: "command_profile_id", read: "command_profile_id::text"},
		{column: "is_enabled", read: "is_enabled"}, {column: "vendor", read: "vendor"}, {column: "model", read: "model"},
		{column: "site", read: "site"}, {column: "description", read: "description"}, {column: "group_id", read: "group_id::text"},
		{column: "protocol", read: "protocol"}, {column: "port", read: "port"},
	}}
	groupFields = assetKind{"bp_switch_groups", "group.update", "name", []assetField{
		{column: "name", read: "name"}, {column: "description", read: "description"},
	}}
	networkFields = assetKind{"bp_networks", "network.update", "cidr::text", []assetField{
		{column: "switch_id", read: "switch_id::text"}, {column: "cidr", read: "cidr::text"},
		{column: "is_enabled", read: "is_enabled"}, {column: "is_validated", read: "is_validated"},
		{column: "vlan", read: "vlan"}, {column: "description", read: "description"},
	}}
)

// updateAudited applies a versioned edit and records the changed fields in the same transaction.
// Any committed change bumps the version, which also invalidates pre-queries pinned to the old one.
func (a *AdminHandler) updateAudited(ctx context.Context, actor domain.Principal, kind assetKind, id string, version int, next map[string]any) (int, error) {
	return a.updateAuditedThen(ctx, actor, kind, id, version, next, nil)
}

// updateAuditedThen runs `then` inside the edit's transaction, after a change is written,
// for values derived from the edited row (such as a schedule's next run).
func (a *AdminHandler) updateAuditedThen(ctx context.Context, actor domain.Principal, kind assetKind, id string, version int, next map[string]any, then func(context.Context, *sql.Tx) error) (int, error) {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var stored int
	var label string
	reads := make([]string, len(kind.fields))
	current := make([]any, len(kind.fields))
	targets := []any{&stored, &label}
	for i, field := range kind.fields {
		reads[i] = field.read
		targets = append(targets, &current[i])
	}
	err = tx.QueryRowContext(ctx, `SELECT version,`+kind.label+`,`+strings.Join(reads, ",")+` FROM `+kind.table+` WHERE id=$1 FOR UPDATE`, id).Scan(targets...)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	if stored != version {
		return 0, errStaleAsset
	}
	set, args, changes := []string{}, []any{id}, map[string]any{}
	for i, field := range kind.fields {
		value, ok := next[field.column]
		if !ok {
			continue
		}
		switch {
		case field.secret && value == nil && current[i] == false:
			continue
		case field.secret && value == nil:
			changes[field.column] = "cleared"
		case field.secret:
			changes[field.column] = "replaced"
		default:
			from, to := auditValue(current[i], field.json), auditValue(value, field.json)
			if reflect.DeepEqual(from, to) {
				continue
			}
			changes[field.column] = map[string]any{"from": from, "to": to}
		}
		args = append(args, value)
		set = append(set, fmt.Sprintf("%s=$%d", field.column, len(args)))
	}
	if len(set) == 0 {
		return stored, tx.Commit()
	}
	err = tx.QueryRowContext(ctx, `UPDATE `+kind.table+` SET `+strings.Join(set, ",")+`,version=version+1 WHERE id=$1 RETURNING version,`+kind.label, args...).Scan(&stored, &label)
	if err != nil {
		return 0, err
	}
	audit, err := json.Marshal(changes)
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO bp_asset_events(actor_id,actor_email,action,object_id,object_name,changes) VALUES ($1,$2,$3,$4,$5,$6)`, actor.User.ID, actor.User.Email, kind.action, id, label, string(audit))
	if err == nil && then != nil {
		err = then(ctx, tx)
	}
	if err != nil {
		return 0, err
	}
	return stored, tx.Commit()
}

// auditValue normalizes stored and requested values so formatting differences are not changes.
func auditValue(value any, isJSON bool) any {
	if n, ok := value.(int); ok { // requests decode ints; PostgreSQL integers scan as int64
		return int64(n)
	}
	text, ok := value.(string)
	if !isJSON || !ok {
		return value
	}
	var parsed any
	if json.Unmarshal([]byte(text), &parsed) != nil {
		return text
	}
	return parsed
}

func (a *AdminHandler) applyEdit(w http.ResponseWriter, r *http.Request, actor domain.Principal, kind assetKind, version int, next map[string]any) {
	a.applyEditThen(w, r, actor, kind, version, next, nil)
}

func (a *AdminHandler) applyEditThen(w http.ResponseWriter, r *http.Request, actor domain.Principal, kind assetKind, version int, next map[string]any, then func(context.Context, *sql.Tx) error) {
	id := r.PathValue("id")
	if !domain.IsCanonicalUUID(id) {
		fail(w, 404, ErrNotFound.Error())
		return
	}
	if version < 1 {
		fail(w, 422, "current version required")
		return
	}
	stored, err := a.updateAuditedThen(r.Context(), actor, kind, id, version, next, then)
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, ErrNotFound):
		fail(w, 404, err.Error())
	case errors.Is(err, errStaleAsset):
		fail(w, 409, err.Error())
	case errors.As(err, &pgErr) && strings.HasPrefix(pgErr.Code, "23"):
		// Duplicate name/address or a reference to a missing credential, profile or switch.
		fail(w, 409, "change conflicts with existing assets")
	case err != nil:
		fail(w, 503, "asset store unavailable")
	default:
		write(w, 200, map[string]any{"id": id, "version": stored})
	}
}

// requiredText trims a provided field and rejects blanks; absent fields stay absent.
func requiredText(next map[string]any, column string, value *string) bool {
	if value == nil {
		return true
	}
	trimmed := strings.TrimSpace(*value)
	next[column] = trimmed
	return trimmed != ""
}

func optionalText(next map[string]any, column string, value *string) {
	if value == nil {
		return
	}
	if trimmed := strings.TrimSpace(*value); trimmed != "" {
		next[column] = trimmed
	} else {
		next[column] = nil
	}
}

func optionalBool(next map[string]any, column string, value *bool) {
	if value != nil {
		next[column] = *value
	}
}

func canonicalIPv4Prefix(value string) bool {
	prefix, err := netip.ParsePrefix(value)
	return err == nil && prefix.Addr().Is4() && prefix.Masked() == prefix && prefix.String() == value
}

func (a *AdminHandler) editCredential(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.authorize(w, r, true)
	if !ok {
		return
	}
	var input struct {
		Version     int     `json:"version"`
		Name        *string `json:"name"`
		Username    *string `json:"username"`
		Description *string `json:"description"`
		IsActive    *bool   `json:"is_active"`
		Password    string  `json:"password"` // empty keeps the stored password
		Secret      string  `json:"secret"`   // empty keeps the stored secret
		ClearSecret bool    `json:"clear_secret"`
	}
	next := map[string]any{}
	if err := decode(r, &input); err != nil || !requiredText(next, "name", input.Name) || !requiredText(next, "username", input.Username) || input.ClearSecret && input.Secret != "" {
		fail(w, 422, "invalid credential")
		return
	}
	optionalText(next, "description", input.Description)
	optionalBool(next, "is_active", input.IsActive)
	for column, plaintext := range map[string]string{"encrypted_password": input.Password, "encrypted_secret": input.Secret} {
		if plaintext == "" {
			continue
		}
		sealed, err := a.seal(plaintext)
		if err != nil {
			fail(w, 503, "encryption unavailable")
			return
		}
		next[column] = sealed
	}
	if input.ClearSecret {
		next["encrypted_secret"] = nil
	}
	a.applyEdit(w, r, actor, credentialFields, input.Version, next)
}

func (a *AdminHandler) editProfile(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.authorize(w, r, true)
	if !ok {
		return
	}
	var input struct {
		Version             int             `json:"version"`
		Name                *string         `json:"name"`
		IsActive            *bool           `json:"is_active"`
		LoginPromptPatterns json.RawMessage `json:"login_prompt_patterns"`
		CommandTemplates    json.RawMessage `json:"command_templates"`
		PromptPatterns      json.RawMessage `json:"prompt_patterns"`
		PaginationRules     json.RawMessage `json:"pagination_rules"`
		SuccessPatterns     json.RawMessage `json:"success_patterns"`
		ErrorPatterns       json.RawMessage `json:"error_patterns"`
		ParserRules         json.RawMessage `json:"parser_rules"`
	}
	next := map[string]any{}
	if err := decode(r, &input); err != nil || !requiredText(next, "name", input.Name) {
		fail(w, 422, "invalid profile")
		return
	}
	optionalBool(next, "is_active", input.IsActive)
	for _, rule := range []struct {
		column string
		value  json.RawMessage
		open   byte
	}{
		{"login_prompt_patterns", input.LoginPromptPatterns, '{'}, {"command_templates", input.CommandTemplates, '{'},
		{"prompt_patterns", input.PromptPatterns, '{'}, {"pagination_rules", input.PaginationRules, '{'},
		{"parser_rules", input.ParserRules, '{'},
		{"success_patterns", input.SuccessPatterns, '['}, {"error_patterns", input.ErrorPatterns, '['},
	} {
		if rule.value == nil {
			continue
		}
		if rule.value[0] != rule.open {
			fail(w, 422, "profile rules must be objects and patterns must be arrays")
			return
		}
		next[rule.column] = string(rule.value)
	}
	a.applyEdit(w, r, actor, profileFields, input.Version, next)
}

func (a *AdminHandler) editSwitch(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.authorize(w, r, true)
	if !ok {
		return
	}
	var input struct {
		Version          int     `json:"version"`
		Name             *string `json:"name"`
		ManagementIP     *string `json:"management_ip"`
		CredentialID     *string `json:"credential_id"`
		CommandProfileID *string `json:"command_profile_id"`
		IsEnabled        *bool   `json:"is_enabled"`
		Vendor           *string `json:"vendor"`
		Model            *string `json:"model"`
		Site             *string `json:"site"`
		Description      *string `json:"description"`
		GroupID          *string `json:"group_id"` // empty removes the group
		Protocol         *string `json:"protocol"`
		Port             *int    `json:"port"` // 0 restores the protocol's default port
	}
	next := map[string]any{}
	if err := decode(r, &input); err != nil || !requiredText(next, "name", input.Name) ||
		input.Vendor != nil && !switchVendors[*input.Vendor] || input.Protocol != nil && *input.Protocol != "telnet" && *input.Protocol != "ssh" ||
		input.Port != nil && (*input.Port < 0 || *input.Port > 65535) || input.GroupID != nil && *input.GroupID != "" && !domain.IsCanonicalUUID(*input.GroupID) {
		fail(w, 422, "invalid switch asset")
		return
	}
	for column, value := range map[string]*string{"vendor": input.Vendor, "protocol": input.Protocol} {
		if value != nil {
			next[column] = *value
		}
	}
	optionalText(next, "model", input.Model)
	optionalText(next, "site", input.Site)
	optionalText(next, "description", input.Description)
	optionalText(next, "group_id", input.GroupID)
	if input.Port != nil {
		next["port"] = nil
		if *input.Port > 0 {
			next["port"] = *input.Port
		}
	}
	for column, value := range map[string]*string{"management_ip": input.ManagementIP, "credential_id": input.CredentialID, "command_profile_id": input.CommandProfileID} {
		if value == nil {
			continue
		}
		if column == "management_ip" && !validIPv4(*value) || column != "management_ip" && !domain.IsCanonicalUUID(*value) {
			fail(w, 422, "invalid switch asset")
			return
		}
		next[column] = *value
	}
	optionalBool(next, "is_enabled", input.IsEnabled)
	a.applyEdit(w, r, actor, switchFields, input.Version, next)
}

func (a *AdminHandler) editNetwork(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.authorize(w, r, true)
	if !ok {
		return
	}
	var input struct {
		Version     int     `json:"version"`
		SwitchID    *string `json:"switch_id"`
		CIDR        *string `json:"cidr"`
		IsEnabled   *bool   `json:"is_enabled"`
		IsValidated *bool   `json:"is_validated"`
		VLAN        *string `json:"vlan"`
		Description *string `json:"description"`
	}
	if err := decode(r, &input); err != nil || input.SwitchID != nil && !domain.IsCanonicalUUID(*input.SwitchID) {
		fail(w, 422, "invalid network")
		return
	}
	if input.CIDR != nil && !canonicalIPv4Prefix(*input.CIDR) {
		fail(w, 422, "canonical IPv4 CIDR required")
		return
	}
	next := map[string]any{}
	if input.SwitchID != nil {
		next["switch_id"] = *input.SwitchID
	}
	if input.CIDR != nil {
		next["cidr"] = *input.CIDR
	}
	optionalBool(next, "is_enabled", input.IsEnabled)
	optionalBool(next, "is_validated", input.IsValidated)
	optionalText(next, "vlan", input.VLAN)
	optionalText(next, "description", input.Description)
	a.applyEdit(w, r, actor, networkFields, input.Version, next)
}

func (a *AdminHandler) editGroup(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.authorize(w, r, true)
	if !ok {
		return
	}
	var input struct {
		Version     int     `json:"version"`
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	next := map[string]any{}
	if err := decode(r, &input); err != nil || !requiredText(next, "name", input.Name) {
		fail(w, 422, "invalid group")
		return
	}
	optionalText(next, "description", input.Description)
	a.applyEdit(w, r, actor, groupFields, input.Version, next)
}
