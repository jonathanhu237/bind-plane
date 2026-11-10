package release

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sort"
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
		{column: "name", read: "name"}, {column: "vendor", read: "vendor"}, {column: "is_active", read: "is_active"},
		{column: "login_prompt_patterns", read: "login_prompt_patterns::text", json: true},
		{column: "command_templates", read: "command_templates::text", json: true},
		{column: "prompt_patterns", read: "prompt_patterns::text", json: true},
		{column: "pagination_rules", read: "pagination_rules::text", json: true},
		{column: "success_patterns", read: "success_patterns::text", json: true},
		{column: "error_patterns", read: "error_patterns::text", json: true},
		{column: "parser_rules", read: "parser_rules::text", json: true},
		{column: "snmp_rules", read: "snmp_rules::text", json: true},
	}}
	switchFields = assetKind{"bp_switches", "switch.update", "name", []assetField{
		{column: "name", read: "name"}, {column: "management_ip", read: "host(management_ip)"},
		{column: "credential_id", read: "credential_id::text"}, {column: "command_profile_id", read: "command_profile_id::text"},
		{column: "is_enabled", read: "is_enabled"}, {column: "vendor", read: "vendor"}, {column: "model", read: "model"},
		{column: "site", read: "site"}, {column: "description", read: "description"}, {column: "group_id", read: "group_id::text"},
		{column: "protocol", read: "protocol"}, {column: "port", read: "port"}, {column: "handles_release", read: "handles_release"},
		{column: "snmp_credential_id", read: "snmp_credential_id::text"}, {column: "snmp_port", read: "snmp_port"},
	}}
	groupFields = assetKind{"bp_switch_groups", "group.update", "name", []assetField{
		{column: "name", read: "name"}, {column: "description", read: "description"},
	}}
)

// updateAudited applies a versioned edit and records the changed fields in the same transaction.
// Any committed change bumps the version, which also invalidates pre-queries pinned to the old one.
func (a *AdminHandler) updateAudited(ctx context.Context, actor domain.Principal, kind assetKind, id string, version int, next map[string]any) (int, error) {
	return a.updateAuditedThen(ctx, actor, kind, id, version, next, nil)
}

// updateAuditedThen runs `then` inside the edit's transaction, after a change is written, with
// the changes it recorded, for values derived from the edited row (such as a schedule's next run).
func (a *AdminHandler) updateAuditedThen(ctx context.Context, actor domain.Principal, kind assetKind, id string, version int, next map[string]any, then func(context.Context, *sql.Tx, map[string]any) error) (int, error) {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	stored, changes, err := updateRow(ctx, tx, actor, kind, id, version, next)
	if err == nil && len(changes) > 0 && then != nil {
		err = then(ctx, tx, changes)
	}
	if err != nil {
		return 0, err
	}
	return stored, tx.Commit()
}

// updateRow applies one edit inside tx and records the changed fields, which it returns; a row
// already as requested records nothing. A version of 0 skips the staleness check, for changes
// that set one value on many rows.
func updateRow(ctx context.Context, tx *sql.Tx, actor domain.Principal, kind assetKind, id string, version int, next map[string]any) (int, map[string]any, error) {
	var stored int
	var label string
	reads := make([]string, len(kind.fields))
	current := make([]any, len(kind.fields))
	targets := []any{&stored, &label}
	for i, field := range kind.fields {
		reads[i] = field.read
		targets = append(targets, &current[i])
	}
	err := tx.QueryRowContext(ctx, `SELECT version,`+kind.label+`,`+strings.Join(reads, ",")+` FROM `+kind.table+` WHERE id=$1 FOR UPDATE`, id).Scan(targets...)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil, ErrNotFound
		}
		return 0, nil, err
	}
	if version != 0 && stored != version {
		return 0, nil, errStaleAsset
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
		return stored, changes, nil
	}
	err = tx.QueryRowContext(ctx, `UPDATE `+kind.table+` SET `+strings.Join(set, ",")+`,version=version+1 WHERE id=$1 RETURNING version,`+kind.label, args...).Scan(&stored, &label)
	if err != nil {
		return 0, nil, err
	}
	audit, err := json.Marshal(changes)
	if err != nil {
		return 0, nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO bp_asset_events(actor_id,actor_email,actor_name,action,object_id,object_name,changes) VALUES ($1,$2,$7,$3,$4,$5,$6)`, actor.User.ID, actor.User.Email, kind.action, id, label, string(audit), actor.User.Name)
	if err != nil {
		return 0, nil, err
	}
	return stored, changes, nil
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

func (a *AdminHandler) applyEditThen(w http.ResponseWriter, r *http.Request, actor domain.Principal, kind assetKind, version int, next map[string]any, then func(context.Context, *sql.Tx, map[string]any) error) {
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
	case isConstraint(err, profileVendorConstraint) && kind.table == profileFields.table:
		fail(w, 422, errProfileInUse)
	case isConstraint(err, profileVendorConstraint):
		fail(w, 422, errProfileVendor)
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
		Vendor              *string         `json:"vendor"`
		IsActive            *bool           `json:"is_active"`
		LoginPromptPatterns json.RawMessage `json:"login_prompt_patterns"`
		CommandTemplates    json.RawMessage `json:"command_templates"`
		PromptPatterns      json.RawMessage `json:"prompt_patterns"`
		PaginationRules     json.RawMessage `json:"pagination_rules"`
		SuccessPatterns     json.RawMessage `json:"success_patterns"`
		ErrorPatterns       json.RawMessage `json:"error_patterns"`
		ParserRules         json.RawMessage `json:"parser_rules"`
		SnmpRules           json.RawMessage `json:"snmp_rules"`
	}
	next := map[string]any{}
	if err := decode(r, &input); err != nil || !requiredText(next, "name", input.Name) || input.Vendor != nil && !switchVendors[*input.Vendor] {
		fail(w, 422, "invalid profile")
		return
	}
	if input.Vendor != nil {
		next["vendor"] = *input.Vendor
	}
	optionalBool(next, "is_active", input.IsActive)
	for _, rule := range []struct {
		column string
		value  json.RawMessage
		open   byte
	}{
		{"login_prompt_patterns", input.LoginPromptPatterns, '{'}, {"command_templates", input.CommandTemplates, '{'},
		{"prompt_patterns", input.PromptPatterns, '{'}, {"pagination_rules", input.PaginationRules, '{'},
		{"parser_rules", input.ParserRules, '{'}, {"snmp_rules", input.SnmpRules, '{'},
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
		HandlesRelease   *bool   `json:"handles_release"`
		SnmpCredentialID *string `json:"snmp_credential_id"` // empty stops monitoring the switch
		SnmpPort         *int    `json:"snmp_port"`          // 0 restores the standard port, 161
	}
	next := map[string]any{}
	if err := decode(r, &input); err != nil || !requiredText(next, "name", input.Name) ||
		input.Vendor != nil && !switchVendors[*input.Vendor] || input.Protocol != nil && *input.Protocol != "telnet" && *input.Protocol != "ssh" ||
		input.Port != nil && (*input.Port < 0 || *input.Port > 65535) || input.GroupID != nil && *input.GroupID != "" && !domain.IsCanonicalUUID(*input.GroupID) ||
		input.SnmpCredentialID != nil && *input.SnmpCredentialID != "" && !domain.IsCanonicalUUID(*input.SnmpCredentialID) ||
		input.SnmpPort != nil && (*input.SnmpPort < 0 || *input.SnmpPort > 65535) {
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
	optionalText(next, "snmp_credential_id", input.SnmpCredentialID)
	if input.SnmpPort != nil {
		next["snmp_port"] = nil
		if *input.SnmpPort > 0 {
			next["snmp_port"] = *input.SnmpPort
		}
	}
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
	optionalBool(next, "handles_release", input.HandlesRelease)
	id := r.PathValue("id")
	a.applyEditThen(w, r, actor, switchFields, input.Version, next, func(ctx context.Context, tx *sql.Tx, changes map[string]any) error {
		return switchChanged(ctx, tx, id, changes)
	})
}

// networkReadColumns change how a switch is reached, so they can change what a read finds.
var networkReadColumns = []string{"management_ip", "credential_id", "command_profile_id", "protocol", "port", "is_enabled"}

// releaseRoleChanged follows an edit of a switch. One that took up handling releases, or a release
// switch now reached differently, has its networks read at once: the scheduler queues every
// release switch never read. One that gave up handling releases stops owning networks.
func releaseRoleChanged(ctx context.Context, tx *sql.Tx, id string, changes map[string]any) error {
	change, roleChanged := changes["handles_release"].(map[string]any)
	if !roleChanged {
		for _, column := range networkReadColumns {
			if _, ok := changes[column]; ok {
				_, err := tx.ExecContext(ctx, `UPDATE bp_switches SET networks_checked_at=NULL WHERE id=$1 AND handles_release`, id)
				return err
			}
		}
		return nil
	}
	if change["to"] == false {
		if _, err := tx.ExecContext(ctx, `DELETE FROM bp_networks WHERE switch_id=$1`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE bp_alerts SET status='resolved',resolved_at=now() WHERE dedupe_key='network_read_failed:'||$1 AND status<>'resolved'`, id); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `UPDATE bp_switches SET networks_checked_at=NULL,networks_succeeded_at=NULL,networks_error=NULL WHERE id=$1`, id)
	return err
}

// bulkSwitches sets one value (group, site, credential, SNMP credential, enabled or release role) on
// many switches in one transaction. Each switch it changes gets its own audit record, as if edited
// alone. A disabled credential is refused, since no switch can release, back up or be polled with it.
func (a *AdminHandler) bulkSwitches(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.authorize(w, r, true)
	if !ok {
		return
	}
	var input struct {
		IDs            []string `json:"ids"`
		GroupID        *string  `json:"group_id"` // empty removes the group
		Site           *string  `json:"site"`     // empty clears the site
		CredentialID   *string  `json:"credential_id"`
		SnmpCredential *string  `json:"snmp_credential_id"` // empty stops monitoring
		IsEnabled      *bool    `json:"is_enabled"`
		HandlesRelease *bool    `json:"handles_release"`
	}
	err := decodeLimit(r, &input, 1<<20)
	next := map[string]any{}
	optionalText(next, "group_id", input.GroupID)
	optionalText(next, "site", input.Site)
	optionalText(next, "credential_id", input.CredentialID)
	optionalText(next, "snmp_credential_id", input.SnmpCredential)
	optionalBool(next, "is_enabled", input.IsEnabled)
	optionalBool(next, "handles_release", input.HandlesRelease)
	ids := map[string]bool{}
	for _, id := range input.IDs {
		ids[id] = true
	}
	valid := err == nil && len(next) == 1 && len(ids) > 0 && len(ids) <= importMaxRows &&
		(input.GroupID == nil || *input.GroupID == "" || domain.IsCanonicalUUID(*input.GroupID)) &&
		(input.CredentialID == nil || domain.IsCanonicalUUID(*input.CredentialID)) &&
		(input.SnmpCredential == nil || *input.SnmpCredential == "" || domain.IsCanonicalUUID(*input.SnmpCredential))
	sorted := make([]string, 0, len(ids))
	for id := range ids {
		valid = valid && domain.IsCanonicalUUID(id)
		sorted = append(sorted, id)
	}
	if !valid {
		fail(w, 422, "switches and exactly one change (group, site, credential, SNMP credential, enabled or release role) required")
		return
	}
	sort.Strings(sorted) // one lock order, so two bulk changes never deadlock
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 503, "asset store unavailable")
		return
	}
	defer tx.Rollback()
	// Held until commit, so the credential cannot be disabled halfway through.
	for table, id := range map[string]*string{"bp_credentials": input.CredentialID, "bp_snmp_credentials": input.SnmpCredential} {
		if id == nil || *id == "" {
			continue
		}
		var active bool
		err = tx.QueryRowContext(r.Context(), `SELECT is_active FROM `+table+` WHERE id=$1 FOR SHARE`, *id).Scan(&active)
		switch {
		case errors.Is(err, sql.ErrNoRows) || err == nil && !active:
			fail(w, 409, "credential is disabled or gone")
			return
		case err != nil:
			fail(w, 503, "asset store unavailable")
			return
		}
	}
	changed, missing, polled := 0, 0, false
	var pgErr *pgconn.PgError
	for _, id := range sorted {
		_, changes, err := updateRow(r.Context(), tx, actor, switchFields, id, 0, next)
		if err == nil {
			err = releaseRoleChanged(r.Context(), tx, id, changes)
		}
		if err == nil {
			var touched bool
			touched, err = pollSoon(r.Context(), tx, id, changes)
			polled = polled || touched
		}
		switch {
		case errors.Is(err, ErrNotFound):
			missing++
		case errors.As(err, &pgErr) && strings.HasPrefix(pgErr.Code, "23"):
			fail(w, 409, "change conflicts with existing assets") // the group is gone
			return
		case err != nil:
			fail(w, 503, "asset store unavailable")
			return
		case len(changes) > 0:
			changed++
		}
	}
	if polled {
		err = forgetUnmonitored(r.Context(), tx)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		fail(w, 503, "asset store unavailable")
		return
	}
	write(w, 200, map[string]int{"changed": changed, "unchanged": len(sorted) - changed - missing, "missing": missing})
}

// deleteGroup removes a group its switches no longer need. A schedule or compliance rule that
// targets it would silently widen to every switch, so those must be changed first.
func (a *AdminHandler) deleteGroup(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.authorize(w, r, true)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if !domain.IsCanonicalUUID(id) {
		fail(w, 404, ErrNotFound.Error())
		return
	}
	ctx := r.Context()
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		fail(w, 503, "asset store unavailable")
		return
	}
	defer tx.Rollback()
	var name string
	var users int
	err = tx.QueryRowContext(ctx, `SELECT name,(SELECT count(*) FROM bp_schedules WHERE group_id=g.id)+(SELECT count(*) FROM bp_compliance_rules WHERE group_id=g.id)
 FROM bp_switch_groups g WHERE id=$1 FOR UPDATE`, id).Scan(&name, &users)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, ErrNotFound.Error())
		return
	}
	if err == nil && users > 0 {
		fail(w, 409, "group is used by schedules or compliance rules")
		return
	}
	var members []string
	if err == nil {
		members, err = groupMembers(ctx, tx, id)
	}
	for _, member := range members {
		if err != nil {
			break
		}
		_, _, err = updateRow(ctx, tx, actor, switchFields, member, 0, map[string]any{"group_id": nil})
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `DELETE FROM bp_switch_groups WHERE id=$1`, id)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO bp_asset_events(actor_id,actor_email,actor_name,action,object_id,object_name,changes) VALUES ($1,$2,$5,'group.delete',$3,$4,jsonb_build_object('switches',$6::int))`,
			actor.User.ID, actor.User.Email, id, name, actor.User.Name, len(members))
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		fail(w, 503, "asset store unavailable")
		return
	}
	write(w, 200, map[string]any{"id": id, "ungrouped": len(members)})
}

func groupMembers(ctx context.Context, tx *sql.Tx, groupID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id::text FROM bp_switches WHERE group_id=$1 ORDER BY id`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
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
