package release

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

// The WeCom (企业微信) bot protocol: one long connection on which the bot signs in, keeps a
// heartbeat, receives members' messages and card clicks, and replies or sends. Frames are
// {cmd, headers: {req_id}, body}; acknowledgements carry the request's req_id and an errcode.
const (
	wecomSubscribe   = "aibot_subscribe"
	wecomPing        = "ping"
	wecomReply       = "aibot_respond_msg"
	wecomWelcome     = "aibot_respond_welcome_msg"
	wecomUpdateCard  = "aibot_respond_update_msg"
	wecomSend        = "aibot_send_msg"
	wecomMessage     = "aibot_msg_callback"
	wecomEvent       = "aibot_event_callback"
	wecomReplacedEvt = "disconnected_event"
)

var (
	errWecomSettingsChanged = errors.New("settings changed")
	// Another connection with the same bot made WeCom drop this one; reconnecting would only take
	// it back, so the bot waits for a change or a long pause.
	errWecomReplaced = errors.New("another connection took over this bot; the sandbox and production each need their own bot")
)

// wecomAuthError is WeCom refusing the bot ID or secret.
type wecomAuthError struct {
	code    int
	message string
}

func (e wecomAuthError) Error() string {
	return fmt.Sprintf("WeCom refused the bot ID or secret (errcode %d: %s)", e.code, e.message)
}

// wecomHandshake sends the WebSocket handshake headers spelled as WeCom requires. Its server
// matches header names case-sensitively and answers 404 to Go's canonical "Sec-Websocket-Key";
// Go writes a header name as stored, so moving the values to the exact spelling is enough.
type wecomHandshake struct{ next http.RoundTripper }

var wecomHeaderNames = map[string]string{
	"Sec-Websocket-Key": "Sec-WebSocket-Key", "Sec-Websocket-Version": "Sec-WebSocket-Version",
	"Sec-Websocket-Extensions": "Sec-WebSocket-Extensions", "Sec-Websocket-Protocol": "Sec-WebSocket-Protocol",
}

func (t wecomHandshake) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	for canonical, exact := range wecomHeaderNames {
		if values, ok := r.Header[canonical]; ok {
			delete(r.Header, canonical)
			r.Header[exact] = values
		}
	}
	return t.next.RoundTrip(r)
}

var wecomDial = &websocket.DialOptions{HTTPClient: &http.Client{Transport: wecomHandshake{next: http.DefaultTransport}}}

// PrincipalFinder loads a user with their roles, as the bot acts for the member's bound account.
type PrincipalFinder interface {
	FindPrincipalByID(context.Context, string) (domain.Principal, error)
}

type wecomFrame struct {
	Cmd     string          `json:"cmd,omitempty"`
	Headers wecomHeaders    `json:"headers"`
	Body    json.RawMessage `json:"body,omitempty"`
	ErrCode *int            `json:"errcode,omitempty"`
	ErrMsg  string          `json:"errmsg,omitempty"`
}
type wecomHeaders struct {
	ReqID string `json:"req_id"`
}

// wecomCallback is a message or event pushed to the bot.
type wecomCallback struct {
	ChatType string `json:"chattype"`
	ChatID   string `json:"chatid"`
	From     struct {
		UserID string `json:"userid"`
	} `json:"from"`
	MsgType string `json:"msgtype"`
	Text    struct {
		Content string `json:"content"`
	} `json:"text"`
	Event struct {
		EventType string `json:"eventtype"`
		// A card click names the card and button inside template_card_event; the SDK's types put
		// them on the event itself, which is read when the nested object is missing.
		Card struct {
			EventKey string `json:"event_key"`
			TaskID   string `json:"task_id"`
		} `json:"template_card_event"`
		EventKey string `json:"event_key"`
		TaskID   string `json:"task_id"`
	} `json:"event"`
}

// cardClick is the card and button a template card event is about.
func (c wecomCallback) cardClick() (taskID, key string) {
	if c.Event.Card.TaskID != "" {
		return c.Event.Card.TaskID, c.Event.Card.EventKey
	}
	return c.Event.TaskID, c.Event.EventKey
}

// chat is where a reply or a later result goes: the member's ID in a single chat, the group's ID
// in a group.
func (c wecomCallback) chat() (kind, id string) {
	if c.ChatType == "group" && c.ChatID != "" {
		return "group", c.ChatID
	}
	return "single", c.From.UserID
}

type wecomSettings struct {
	enabled            bool
	botID, sealed, url string
	version            int
}

// WeComBot keeps the bot's connection while admins have it turned on, and lets bound members
// release addresses from a chat through the same store the release console uses.
type WeComBot struct {
	db         *sql.DB
	key        []byte
	store      Store
	principals PrincipalFinder
	loc        *time.Location
	publicURL  string // where card notices lead: the release console

	// How often the settings are checked, results sent and heartbeats beaten; tests shorten them.
	SettingsEvery, NotifyEvery, HeartbeatEvery time.Duration
	// How long a card's buttons stay usable.
	CardTTL time.Duration

	writeMu sync.Mutex
	conn    *websocket.Conn

	pendingMu sync.Mutex
	pending   map[string]wecomPending // req_id of a result sent, until WeCom acknowledges it
}

type wecomPending struct {
	jobID string
	sent  time.Time
}

func NewWeComBot(db *sql.DB, store Store, principals PrincipalFinder, loc *time.Location, publicURL string) (*WeComBot, error) {
	key, err := credentialKey()
	if err != nil {
		return nil, err
	}
	return &WeComBot{db: db, key: key, store: store, principals: principals, loc: loc, publicURL: publicURL,
		SettingsEvery: 5 * time.Second, NotifyEvery: 3 * time.Second, HeartbeatEvery: 30 * time.Second, CardTTL: 30 * time.Minute,
		pending: map[string]wecomPending{}}, nil
}

func wecomReqID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s_%d_%s", prefix, time.Now().UnixMilli(), hex.EncodeToString(b))
}

func (b *WeComBot) loadSettings(ctx context.Context) (wecomSettings, error) {
	var s wecomSettings
	var sealed sql.NullString
	err := b.db.QueryRowContext(ctx, `SELECT is_enabled,bot_id,encrypted_bot_secret,ws_url,version FROM bp_wecom_settings`).Scan(&s.enabled, &s.botID, &sealed, &s.url, &s.version)
	s.sealed = sealed.String
	return s, err
}

// setState records how the connection stands for the WeCom page; a failure keeps its reason.
func (b *WeComBot) setState(ctx context.Context, state, reason string) {
	if _, err := b.db.ExecContext(ctx, `UPDATE bp_wecom_settings SET state=$1,state_at=now(),last_error=NULLIF($2,''),
 connected_at=CASE WHEN $1='connected' THEN now() ELSE connected_at END WHERE state<>$1 OR last_error IS DISTINCT FROM NULLIF($2,'')`, state, reason); err != nil && ctx.Err() == nil {
		log.Printf("wecom: record state: %v", err)
	}
}

// Run keeps the bot connected while it is turned on, reconnecting after failures with growing
// pauses, and reconnecting at once with new settings.
func (b *WeComBot) Run(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		s, err := b.loadSettings(ctx)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("wecom: load settings: %v", err)
			}
			b.waitChange(ctx, -1, b.SettingsEvery)
			continue
		}
		if !s.enabled {
			b.setState(ctx, "disabled", "")
			b.waitChange(ctx, s.version, 0)
			continue
		}
		err = b.session(ctx, s)
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, errWecomSettingsChanged) {
			backoff = time.Second
			continue
		}
		b.setState(ctx, "failed", err.Error())
		var auth wecomAuthError
		switch {
		case errors.As(err, &auth):
			b.waitChange(ctx, s.version, 5*time.Minute)
		case errors.Is(err, errWecomReplaced):
			b.waitChange(ctx, s.version, 10*time.Minute)
		default:
			b.waitChange(ctx, s.version, backoff)
			backoff = min(backoff*2, time.Minute)
		}
	}
}

// waitChange returns once the settings version differs from version, after limit (0 waits for
// the change alone), or when ctx ends.
func (b *WeComBot) waitChange(ctx context.Context, version int, limit time.Duration) {
	var deadline <-chan time.Time
	if limit > 0 {
		timer := time.NewTimer(limit)
		defer timer.Stop()
		deadline = timer.C
	}
	tick := time.NewTicker(b.SettingsEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline:
			return
		case <-tick.C:
			if s, err := b.loadSettings(ctx); err == nil && s.version != version {
				return
			}
		}
	}
}

func (b *WeComBot) send(ctx context.Context, cmd, reqID string, body any) error {
	data, err := json.Marshal(map[string]any{"cmd": cmd, "headers": wecomHeaders{ReqID: reqID}, "body": body})
	if err != nil {
		return err
	}
	b.writeMu.Lock()
	defer b.writeMu.Unlock()
	if b.conn == nil {
		return errors.New("not connected")
	}
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return b.conn.Write(writeCtx, websocket.MessageText, data)
}

// session signs in with s and serves the connection until it fails, the settings change or ctx ends.
func (b *WeComBot) session(ctx context.Context, s wecomSettings) error {
	secret, err := openLabelled(b.key, wecomSecretAAD, s.sealed)
	if err != nil {
		return errors.New("the stored secret cannot be decrypted; enter it again")
	}
	b.setState(ctx, "connecting", "")
	dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	conn, _, err := websocket.Dial(dialCtx, s.url, wecomDial)
	cancel()
	if err != nil {
		return fmt.Errorf("cannot reach WeCom: %v", err)
	}
	conn.SetReadLimit(4 << 20)
	defer conn.CloseNow()
	sessCtx, stop := context.WithCancel(ctx)
	defer stop()
	b.writeMu.Lock()
	b.conn = conn
	b.writeMu.Unlock()
	defer func() {
		b.writeMu.Lock()
		b.conn = nil
		b.writeMu.Unlock()
	}()

	frames, readErr := make(chan wecomFrame, 16), make(chan error, 1)
	go func() {
		for {
			_, data, err := conn.Read(sessCtx)
			if err != nil {
				readErr <- err
				return
			}
			var frame wecomFrame
			if json.Unmarshal(data, &frame) != nil {
				continue
			}
			select {
			case frames <- frame:
			case <-sessCtx.Done():
				return
			}
		}
	}()
	if err = b.send(sessCtx, wecomSubscribe, wecomReqID(wecomSubscribe), map[string]string{"bot_id": s.botID, "secret": secret}); err != nil {
		return fmt.Errorf("cannot reach WeCom: %v", err)
	}
	signIn := time.NewTimer(15 * time.Second)
	defer signIn.Stop()
	heartbeat, settings, notify := time.NewTicker(b.HeartbeatEvery), time.NewTicker(b.SettingsEvery), time.NewTicker(b.NotifyEvery)
	defer heartbeat.Stop()
	defer settings.Stop()
	defer notify.Stop()
	signedIn, missed := false, 0
	for {
		select {
		case <-ctx.Done():
			conn.Close(websocket.StatusNormalClosure, "")
			return ctx.Err()
		case err := <-readErr:
			return fmt.Errorf("WeCom closed the connection: %v", err)
		case <-signIn.C:
			if !signedIn {
				return errors.New("WeCom did not answer the sign-in")
			}
		case frame := <-frames:
			reqID := frame.Headers.ReqID
			switch {
			case frame.Cmd == "" && strings.HasPrefix(reqID, wecomSubscribe):
				if frame.ErrCode != nil && *frame.ErrCode != 0 {
					return wecomAuthError{code: *frame.ErrCode, message: frame.ErrMsg}
				}
				signedIn = true
				b.setState(ctx, "connected", "")
			case frame.Cmd == "" && strings.HasPrefix(reqID, wecomPing):
				missed = 0
			case frame.Cmd == "":
				b.acknowledged(ctx, frame)
			case frame.Cmd == wecomMessage:
				go b.onMessage(sessCtx, frame)
			case frame.Cmd == wecomEvent:
				var cb wecomCallback
				_ = json.Unmarshal(frame.Body, &cb)
				if cb.Event.EventType == wecomReplacedEvt {
					return errWecomReplaced
				}
				go b.onEvent(sessCtx, frame, cb)
			}
		case <-heartbeat.C:
			if !signedIn {
				continue
			}
			if missed >= 2 {
				return errors.New("WeCom stopped answering the heartbeat")
			}
			missed++
			if err := b.send(sessCtx, wecomPing, wecomReqID(wecomPing), nil); err != nil {
				return fmt.Errorf("WeCom closed the connection: %v", err)
			}
		case <-settings.C:
			if now, err := b.loadSettings(ctx); err == nil && now.version != s.version {
				conn.Close(websocket.StatusNormalClosure, "settings changed")
				return errWecomSettingsChanged
			}
		case <-notify.C:
			if signedIn {
				b.notifyResults(sessCtx)
			}
		}
	}
}

// acknowledged settles a sent result: WeCom took it (errcode 0), so it is not sent again.
func (b *WeComBot) acknowledged(ctx context.Context, frame wecomFrame) {
	b.pendingMu.Lock()
	sent, ok := b.pending[frame.Headers.ReqID]
	delete(b.pending, frame.Headers.ReqID)
	b.pendingMu.Unlock()
	if frame.ErrCode != nil && *frame.ErrCode != 0 {
		log.Printf("wecom: %s refused: errcode %d: %s", frame.Headers.ReqID, *frame.ErrCode, frame.ErrMsg)
		return
	}
	if ok {
		if _, err := b.db.ExecContext(ctx, `UPDATE bp_wecom_jobs SET notified_at=now() WHERE job_id=$1`, sent.jobID); err != nil && ctx.Err() == nil {
			log.Printf("wecom: record result sent: %v", err)
		}
	}
}
