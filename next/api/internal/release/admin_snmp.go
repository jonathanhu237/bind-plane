package release

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

// An SNMP credential lets the worker poll switches for monitoring. Like a login credential it is
// shared by many switches, and its community or passwords are sealed and never listed.
var snmpCredentialFields = assetKind{"bp_snmp_credentials", "snmp_credential.update", "name", []assetField{
	{column: "name", read: "name"}, {column: "snmp_version", read: "snmp_version"}, {column: "username", read: "username"},
	{column: "auth_protocol", read: "auth_protocol"}, {column: "priv_protocol", read: "priv_protocol"},
	{column: "description", read: "description"}, {column: "is_active", read: "is_active"},
	{column: "encrypted_community", read: "encrypted_community IS NOT NULL", secret: true},
	{column: "encrypted_auth_password", read: "encrypted_auth_password IS NOT NULL", secret: true},
	{column: "encrypted_priv_password", read: "encrypted_priv_password IS NOT NULL", secret: true},
}}

var (
	snmpAuthProtocols = map[string]bool{"md5": true, "sha": true, "sha224": true, "sha256": true, "sha384": true, "sha512": true}
	snmpPrivProtocols = map[string]bool{"des": true, "aes": true, "aes192": true, "aes256": true}
)

const (
	errSnmpName      = "SNMP credential name required"
	errSnmpCommunity = "community required for SNMP v2c"
	errSnmpUser      = "username required for SNMP v3"
	errSnmpProtocol  = "unknown SNMP version, authentication or privacy protocol"
	errSnmpPassword  = "SNMP v3 passwords must be at least 8 characters"
	errSnmpPrivacy   = "SNMP v3 privacy needs authentication"
)

// snmpSecrets is an SNMP credential as it will be stored. A secret is a new plaintext to seal,
// or empty to keep the stored one; has* says whether one is stored now.
type snmpSecrets struct {
	version, username, auth, priv              string
	community, authPassword, privPassword      string
	hasCommunity, hasAuthPassword, hasPrivPass bool
}

// check reports why the credential cannot be stored, or "". SNMP v3 passphrases shorter than
// eight characters cannot be turned into keys (RFC 3414).
func (s snmpSecrets) check() string {
	switch s.version {
	case "v2c":
		if s.community == "" && !s.hasCommunity {
			return errSnmpCommunity
		}
		return ""
	case "v3":
	default:
		return errSnmpProtocol
	}
	switch {
	case s.username == "":
		return errSnmpUser
	case s.auth != "" && !snmpAuthProtocols[s.auth] || s.priv != "" && !snmpPrivProtocols[s.priv]:
		return errSnmpProtocol
	case s.priv != "" && s.auth == "":
		return errSnmpPrivacy
	case s.auth != "" && (s.authPassword == "" && !s.hasAuthPassword || s.authPassword != "" && len(s.authPassword) < 8),
		s.priv != "" && (s.privPassword == "" && !s.hasPrivPass || s.privPassword != "" && len(s.privPassword) < 8):
		return errSnmpPassword
	}
	return ""
}

// columns are the stored values: a sealed new secret, nil for one the version or protocols no
// longer use, and nothing for one kept as it is.
func (a *AdminHandler) snmpColumns(s snmpSecrets) (map[string]any, error) {
	next := map[string]any{"snmp_version": s.version}
	sealed := func(column, plaintext string, used bool) error {
		switch {
		case !used:
			next[column] = nil
		case plaintext != "":
			value, err := a.seal(plaintext)
			if err != nil {
				return err
			}
			next[column] = value
		}
		return nil
	}
	nullable := func(value string) any {
		if value == "" {
			return nil
		}
		return value
	}
	v3 := s.version == "v3"
	next["username"], next["auth_protocol"], next["priv_protocol"] = nil, nil, nil
	if v3 {
		next["username"], next["auth_protocol"], next["priv_protocol"] = s.username, nullable(s.auth), nullable(s.priv)
	}
	if err := sealed("encrypted_community", s.community, !v3); err != nil {
		return nil, err
	}
	if err := sealed("encrypted_auth_password", s.authPassword, v3 && s.auth != ""); err != nil {
		return nil, err
	}
	if err := sealed("encrypted_priv_password", s.privPassword, v3 && s.priv != ""); err != nil {
		return nil, err
	}
	return next, nil
}

type snmpCredentialInput struct {
	Name         *string `json:"name"`
	SnmpVersion  *string `json:"snmp_version"`
	Community    string  `json:"community"` // empty keeps the stored community
	Username     *string `json:"username"`
	AuthProtocol *string `json:"auth_protocol"` // empty means no authentication
	AuthPassword string  `json:"auth_password"` // empty keeps the stored password
	PrivProtocol *string `json:"priv_protocol"` // empty means no privacy
	PrivPassword string  `json:"priv_password"` // empty keeps the stored password
	Description  *string `json:"description"`
}

func trimmedText(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func (a *AdminHandler) snmpCredential(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.authorize(w, r, true)
	if !ok {
		return
	}
	var input snmpCredentialInput
	if err := decode(r, &input); err != nil {
		fail(w, 422, "invalid SNMP credential")
		return
	}
	name := trimmedText(input.Name)
	secrets := snmpSecrets{version: trimmedText(input.SnmpVersion), username: trimmedText(input.Username), auth: trimmedText(input.AuthProtocol), priv: trimmedText(input.PrivProtocol),
		community: input.Community, authPassword: input.AuthPassword, privPassword: input.PrivPassword}
	problem := secrets.check()
	if name == "" {
		problem = errSnmpName
	}
	if problem != "" {
		fail(w, 422, problem)
		return
	}
	columns, err := a.snmpColumns(secrets)
	if err != nil {
		fail(w, 503, "encryption unavailable")
		return
	}
	id, err := a.createAudited(r.Context(), actor, "snmp_credential.create", name,
		`INSERT INTO bp_snmp_credentials(name,snmp_version,username,auth_protocol,priv_protocol,encrypted_community,encrypted_auth_password,encrypted_priv_password,description)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,'')) RETURNING id::text`,
		name, columns["snmp_version"], columns["username"], columns["auth_protocol"], columns["priv_protocol"],
		columns["encrypted_community"], columns["encrypted_auth_password"], columns["encrypted_priv_password"], trimmedText(input.Description))
	if err != nil {
		fail(w, 409, "SNMP credential could not be saved")
		return
	}
	write(w, 201, map[string]string{"id": id, "name": name})
}

// editSnmpCredential merges the edit with what is stored: an empty secret keeps the stored one
// while the version and protocols still use it, and one they no longer use is removed.
func (a *AdminHandler) editSnmpCredential(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.authorize(w, r, true)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if !domain.IsCanonicalUUID(id) {
		fail(w, 404, ErrNotFound.Error())
		return
	}
	var input struct {
		snmpCredentialInput
		Version  int   `json:"version"`
		IsActive *bool `json:"is_active"`
	}
	next := map[string]any{}
	if err := decode(r, &input); err != nil || !requiredText(next, "name", input.Name) {
		fail(w, 422, errSnmpName)
		return
	}
	var stored snmpSecrets
	var username, auth, priv sql.NullString
	err := a.db.QueryRowContext(r.Context(), `SELECT snmp_version,username,auth_protocol,priv_protocol,encrypted_community IS NOT NULL,
 encrypted_auth_password IS NOT NULL,encrypted_priv_password IS NOT NULL FROM bp_snmp_credentials WHERE id=$1`, id).
		Scan(&stored.version, &username, &auth, &priv, &stored.hasCommunity, &stored.hasAuthPassword, &stored.hasPrivPass)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, ErrNotFound.Error())
		return
	}
	if err != nil {
		fail(w, 503, "asset store unavailable")
		return
	}
	merged := snmpSecrets{version: stored.version, username: username.String, auth: auth.String, priv: priv.String,
		community: input.Community, authPassword: input.AuthPassword, privPassword: input.PrivPassword}
	if input.SnmpVersion != nil {
		merged.version = trimmedText(input.SnmpVersion)
	}
	for target, value := range map[*string]*string{&merged.username: input.Username, &merged.auth: input.AuthProtocol, &merged.priv: input.PrivProtocol} {
		if value != nil {
			*target = trimmedText(value)
		}
	}
	// A secret carries over only to the same kind of use: a community to v2c, passwords to v3.
	merged.hasCommunity = stored.hasCommunity && stored.version == "v2c"
	merged.hasAuthPassword = stored.hasAuthPassword && stored.version == "v3"
	merged.hasPrivPass = stored.hasPrivPass && stored.version == "v3"
	if problem := merged.check(); problem != "" {
		fail(w, 422, problem)
		return
	}
	columns, err := a.snmpColumns(merged)
	if err != nil {
		fail(w, 503, "encryption unavailable")
		return
	}
	for column, value := range columns {
		next[column] = value
	}
	optionalText(next, "description", input.Description)
	optionalBool(next, "is_active", input.IsActive)
	a.applyEditThen(w, r, actor, snmpCredentialFields, input.Version, next, func(ctx context.Context, tx *sql.Tx, changes map[string]any) error {
		if _, ok := changes["is_active"]; ok {
			return forgetUnmonitored(ctx, tx)
		}
		return nil
	})
}
