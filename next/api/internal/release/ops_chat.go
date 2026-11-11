package release

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

// chatBotKind is what differs between the bots' settings: how the secret is sealed, which
// addresses make sense, and what the audit log calls a change.
type chatBotKind struct {
	aad     string
	schemes []string // an address's schemes: WeCom's long connection, Feishu's open platform
	fields  assetKind
}

func chatBotFields(action string) assetKind {
	return assetKind{"bp_chat_bots", action, "name", []assetField{
		{column: "is_enabled", read: "is_enabled"}, {column: "app_id", read: "app_id"},
		{column: "encrypted_bot_secret", read: "encrypted_bot_secret IS NOT NULL", secret: true}, {column: "address", read: "address"},
	}}
}

var chatBotKinds = map[string]chatBotKind{
	platformWeCom:  {aad: wecomSecretAAD, schemes: []string{"wss", "ws"}, fields: chatBotFields("wecom.update")},
	platformFeishu: {aad: feishuSecretAAD, schemes: []string{"https", "http"}, fields: chatBotFields("feishu.update")},
}

// chatAccountID is a member's ID on a platform: a WeCom account, or a Feishu user ID or open_id.
var chatAccountID = regexp.MustCompile(`^[A-Za-z0-9_.@-]{1,64}$`)

const (
	errBotNotReady = "an app ID and secret are needed before the bot is turned on"
	errBotSettings = "invalid bot settings"
	errBotAccount  = "an account is letters, digits and _ . @ -, at most 64"
	errBotTaken    = "this account is already bound to another user"
)

// chatPlatform is the platform a request names, or "" (after answering 404) when it names none.
func chatPlatform(w http.ResponseWriter, r *http.Request) (string, chatBotKind) {
	platform := r.PathValue("platform")
	kind, ok := chatBotKinds[platform]
	if !ok {
		fail(w, 404, ErrNotFound.Error())
		return "", kind
	}
	return platform, kind
}

// chatBot shows a bot's settings (never its secret), how its connection stands, which member is
// which user, and the users that can be bound.
func (o *OpsHandler) chatBot(w http.ResponseWriter, r *http.Request) {
	if _, ok := o.authorize(w, r, false); !ok {
		return
	}
	platform, _ := chatPlatform(w, r)
	if platform == "" {
		return
	}
	ctx := r.Context()
	settings, err := o.rows(ctx, []string{"id", "is_enabled", "app_id", "has_secret", "address", "version", "state", "state_at", "connected_at", "last_error"},
		`SELECT id::text,is_enabled,app_id,encrypted_bot_secret IS NOT NULL,address,version,state,state_at,connected_at,last_error FROM bp_chat_bots WHERE platform=$1`, platform)
	var accounts, users []map[string]any
	if err == nil {
		accounts, err = o.rows(ctx, []string{"user_id", "name", "email", "disabled", "external_id", "created_at"},
			`SELECT a.user_id::text,u.name,u.email,u.disabled_at IS NOT NULL,a.external_id,a.created_at FROM bp_chat_accounts a
 JOIN auth_users u ON u.id=a.user_id WHERE a.platform=$1 ORDER BY u.name,u.email`, platform)
	}
	if err == nil {
		users, err = o.rows(ctx, []string{"id", "name", "email"}, `SELECT id::text,name,email FROM auth_users WHERE disabled_at IS NULL ORDER BY name,email`)
	}
	if err == nil && len(settings) != 1 {
		err = errors.New(platform + " bot settings row missing")
	}
	if err != nil {
		o.unavailable(w, err)
		return
	}
	write(w, 200, map[string]any{"settings": settings[0], "accounts": accounts, "users": users})
}

// editChatBot changes a bot's settings. A blank secret keeps the stored one; the connector picks
// the change up within seconds and reconnects with it.
func (o *OpsHandler) editChatBot(w http.ResponseWriter, r *http.Request) {
	actor, ok := o.authorize(w, r, true)
	if !ok {
		return
	}
	platform, kind := chatPlatform(w, r)
	if platform == "" {
		return
	}
	var input struct {
		Version   int     `json:"version"`
		IsEnabled *bool   `json:"is_enabled"`
		AppID     *string `json:"app_id"`
		Secret    *string `json:"secret"`
		Address   *string `json:"address"`
	}
	next := map[string]any{}
	if err := decode(r, &input); err != nil || input.Version < 1 {
		fail(w, 422, errBotSettings)
		return
	}
	if input.AppID != nil {
		id := strings.TrimSpace(*input.AppID)
		if len(id) > 128 || strings.ContainsAny(id, " \t\r\n") {
			fail(w, 422, errBotSettings)
			return
		}
		next["app_id"] = id
	}
	if input.Secret != nil && strings.TrimSpace(*input.Secret) != "" {
		sealed, err := sealLabelled(o.key, kind.aad, strings.TrimSpace(*input.Secret))
		if err != nil {
			fail(w, 503, "encryption unavailable")
			return
		}
		next["encrypted_bot_secret"] = sealed
	}
	if input.Address != nil {
		address := strings.TrimRight(strings.TrimSpace(*input.Address), "/")
		parsed, err := url.Parse(address)
		if err != nil || len(address) > 512 || parsed.Host == "" || !slices.Contains(kind.schemes, parsed.Scheme) {
			fail(w, 422, errBotSettings)
			return
		}
		next["address"] = address
	}
	optionalBool(next, "is_enabled", input.IsEnabled)
	var id string
	if err := o.db.QueryRowContext(r.Context(), `SELECT id::text FROM bp_chat_bots WHERE platform=$1`, platform).Scan(&id); err != nil {
		o.unavailable(w, err)
		return
	}
	stored, err := o.updateAudited(r.Context(), actor, kind.fields, id, input.Version, next)
	switch {
	case errors.Is(err, errStaleAsset):
		fail(w, 409, err.Error())
	case isConstraint(err, "bp_chat_bots_ready"):
		fail(w, 422, errBotNotReady)
	case err != nil:
		o.unavailable(w, err)
	default:
		write(w, 200, map[string]any{"version": stored})
	}
}

// bindChatAccount records which member of the platform a user is, or with no account (DELETE)
// removes it.
func (o *OpsHandler) bindChatAccount(w http.ResponseWriter, r *http.Request) {
	actor, ok := o.authorize(w, r, true)
	if !ok {
		return
	}
	platform, _ := chatPlatform(w, r)
	if platform == "" {
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
			ExternalID string `json:"external_id"`
		}
		if err := decode(r, &input); err != nil || !chatAccountID.MatchString(strings.TrimSpace(input.ExternalID)) {
			fail(w, 422, errBotAccount)
			return
		}
		account = strings.TrimSpace(input.ExternalID)
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
	err = tx.QueryRowContext(ctx, `SELECT u.name,a.external_id FROM auth_users u LEFT JOIN bp_chat_accounts a ON a.user_id=u.id AND a.platform=$2 WHERE u.id=$1 FOR UPDATE OF u`, userID, platform).Scan(&name, &before)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, ErrNotFound.Error())
		return
	}
	if err == nil && account == "" {
		_, err = tx.ExecContext(ctx, `DELETE FROM bp_chat_accounts WHERE platform=$1 AND user_id=$2`, platform, userID)
	} else if err == nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO bp_chat_accounts(platform,user_id,external_id) VALUES ($1,$2,$3)
 ON CONFLICT (platform,user_id) DO UPDATE SET external_id=EXCLUDED.external_id`, platform, userID, account)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		fail(w, 409, errBotTaken)
		return
	}
	if err == nil && (before.String != account || before.Valid != (account != "")) {
		var to, from any
		if account != "" {
			to = account
		}
		if before.Valid {
			from = before.String
		}
		changes, _ := json.Marshal(map[string]any{"external_id": map[string]any{"from": from, "to": to}})
		_, err = tx.ExecContext(ctx, `INSERT INTO bp_asset_events(actor_id,actor_email,actor_name,action,object_id,object_name,changes) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			actor.User.ID, actor.User.Email, actor.User.Name, platform+".account", userID, name, string(changes))
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		o.unavailable(w, err)
		return
	}
	write(w, 200, map[string]any{"user_id": userID, "external_id": account})
}
