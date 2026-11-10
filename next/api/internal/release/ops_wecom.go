package release

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

// wecomFields are the WeCom bot's settings an admin edits; the connector writes the state columns.
var wecomFields = assetKind{"bp_wecom_settings", "wecom.update", "name", []assetField{
	{column: "is_enabled", read: "is_enabled"}, {column: "bot_id", read: "bot_id"},
	{column: "encrypted_bot_secret", read: "encrypted_bot_secret IS NOT NULL", secret: true}, {column: "ws_url", read: "ws_url"},
}}

// wecomUserID is a WeCom member's account in the corporate directory.
var wecomUserID = regexp.MustCompile(`^[A-Za-z0-9_.@-]{1,64}$`)

const (
	errWecomNotReady = "a bot ID and secret are needed before WeCom is turned on"
	errWecomSettings = "invalid WeCom settings"
	errWecomAccount  = "a WeCom account is letters, digits and _ . @ -, at most 64"
	errWecomTaken    = "this WeCom account is already bound to another user"
)

// wecom shows the bot's settings (never its secret), how its connection stands, which WeCom member
// is which user, and the users that can be bound.
func (o *OpsHandler) wecom(w http.ResponseWriter, r *http.Request) {
	if _, ok := o.authorize(w, r, false); !ok {
		return
	}
	ctx := r.Context()
	settings, err := o.rows(ctx, []string{"id", "is_enabled", "bot_id", "has_secret", "ws_url", "version", "state", "state_at", "connected_at", "last_error"},
		`SELECT id::text,is_enabled,bot_id,encrypted_bot_secret IS NOT NULL,ws_url,version,state,state_at,connected_at,last_error FROM bp_wecom_settings`)
	var accounts, users []map[string]any
	if err == nil {
		accounts, err = o.rows(ctx, []string{"user_id", "name", "email", "disabled", "wecom_userid", "created_at"},
			`SELECT a.user_id::text,u.name,u.email,u.disabled_at IS NOT NULL,a.wecom_userid,a.created_at FROM bp_wecom_accounts a
 JOIN auth_users u ON u.id=a.user_id ORDER BY u.name,u.email`)
	}
	if err == nil {
		users, err = o.rows(ctx, []string{"id", "name", "email"}, `SELECT id::text,name,email FROM auth_users WHERE disabled_at IS NULL ORDER BY name,email`)
	}
	if err == nil && len(settings) != 1 {
		err = errors.New("WeCom settings row missing")
	}
	if err != nil {
		o.unavailable(w, err)
		return
	}
	write(w, 200, map[string]any{"settings": settings[0], "accounts": accounts, "users": users})
}

// editWecom changes the bot's settings. A blank secret keeps the stored one; the connector picks
// the change up within seconds and reconnects with it.
func (o *OpsHandler) editWecom(w http.ResponseWriter, r *http.Request) {
	actor, ok := o.authorize(w, r, true)
	if !ok {
		return
	}
	var input struct {
		Version   int     `json:"version"`
		IsEnabled *bool   `json:"is_enabled"`
		BotID     *string `json:"bot_id"`
		Secret    *string `json:"secret"`
		WsURL     *string `json:"ws_url"`
	}
	next := map[string]any{}
	if err := decode(r, &input); err != nil || input.Version < 1 {
		fail(w, 422, errWecomSettings)
		return
	}
	if input.BotID != nil {
		id := strings.TrimSpace(*input.BotID)
		if len(id) > 128 || strings.ContainsAny(id, " \t\r\n") {
			fail(w, 422, errWecomSettings)
			return
		}
		next["bot_id"] = id
	}
	if input.Secret != nil && strings.TrimSpace(*input.Secret) != "" {
		sealed, err := sealLabelled(o.key, wecomSecretAAD, strings.TrimSpace(*input.Secret))
		if err != nil {
			fail(w, 503, "encryption unavailable")
			return
		}
		next["encrypted_bot_secret"] = sealed
	}
	if input.WsURL != nil {
		address := strings.TrimSpace(*input.WsURL)
		parsed, err := url.Parse(address)
		if err != nil || len(address) > 512 || parsed.Host == "" || parsed.Scheme != "wss" && parsed.Scheme != "ws" {
			fail(w, 422, errWecomSettings)
			return
		}
		next["ws_url"] = address
	}
	optionalBool(next, "is_enabled", input.IsEnabled)
	var id string
	if err := o.db.QueryRowContext(r.Context(), `SELECT id::text FROM bp_wecom_settings`).Scan(&id); err != nil {
		o.unavailable(w, err)
		return
	}
	stored, err := o.updateAudited(r.Context(), actor, wecomFields, id, input.Version, next)
	switch {
	case errors.Is(err, errStaleAsset):
		fail(w, 409, err.Error())
	case isConstraint(err, "bp_wecom_settings_ready"):
		fail(w, 422, errWecomNotReady)
	case err != nil:
		o.unavailable(w, err)
	default:
		write(w, 200, map[string]any{"version": stored})
	}
}

// bindWecom records which WeCom member a user is, or with no account (DELETE) removes it.
func (o *OpsHandler) bindWecom(w http.ResponseWriter, r *http.Request) {
	actor, ok := o.authorize(w, r, true)
	if !ok {
		return
	}
	userID := r.PathValue("id")
	if !domain.IsCanonicalUUID(userID) {
		fail(w, 404, ErrNotFound.Error())
		return
	}
	var account string
	if r.Method == http.MethodPut {
		var input struct {
			WecomUserID string `json:"wecom_userid"`
		}
		if err := decode(r, &input); err != nil || !wecomUserID.MatchString(strings.TrimSpace(input.WecomUserID)) {
			fail(w, 422, errWecomAccount)
			return
		}
		account = strings.TrimSpace(input.WecomUserID)
	}
	ctx := r.Context()
	tx, err := o.db.BeginTx(ctx, nil)
	if err != nil {
		o.unavailable(w, err)
		return
	}
	defer tx.Rollback()
	var name string
	var before sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT u.name,a.wecom_userid FROM auth_users u LEFT JOIN bp_wecom_accounts a ON a.user_id=u.id WHERE u.id=$1 FOR UPDATE OF u`, userID).Scan(&name, &before)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, ErrNotFound.Error())
		return
	}
	if err == nil && account == "" {
		_, err = tx.ExecContext(ctx, `DELETE FROM bp_wecom_accounts WHERE user_id=$1`, userID)
	} else if err == nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO bp_wecom_accounts(user_id,wecom_userid) VALUES ($1,$2)
 ON CONFLICT (user_id) DO UPDATE SET wecom_userid=EXCLUDED.wecom_userid`, userID, account)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		fail(w, 409, errWecomTaken)
		return
	}
	if err == nil && (before.String != account || before.Valid != (account != "")) {
		var to any
		if account != "" {
			to = account
		}
		var from any
		if before.Valid {
			from = before.String
		}
		changes, _ := json.Marshal(map[string]any{"wecom_userid": map[string]any{"from": from, "to": to}})
		_, err = tx.ExecContext(ctx, `INSERT INTO bp_asset_events(actor_id,actor_email,actor_name,action,object_id,object_name,changes) VALUES ($1,$2,$3,'wecom.account',$4,$5,$6)`,
			actor.User.ID, actor.User.Email, actor.User.Name, userID, name, string(changes))
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		o.unavailable(w, err)
		return
	}
	write(w, 200, map[string]any{"user_id": userID, "wecom_userid": account})
}
