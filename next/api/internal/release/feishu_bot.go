package release

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// The Feishu (飞书) bot. Like the WeCom one it keeps a long connection out, so the server needs no
// public address, but in Feishu's protocol (read from Feishu's official SDKs): the open platform
// hands out a connection address for the app's ID and secret, frames are protobuf, members'
// messages and card clicks arrive as events, and replies and results go out through the open
// platform's REST API. A click's answer goes back on the connection, and replaces the card.

const (
	feishuEndpointPath = "/callback/ws/endpoint"
	feishuMessageEvent = "im.message.receive_v1"
	feishuCardEvent    = "card.action.trigger"
)

// feishuAuthError is Feishu refusing the App ID or secret.
type feishuAuthError struct {
	code    int
	message string
}

func (e feishuAuthError) Error() string {
	return fmt.Sprintf("Feishu refused the App ID or secret (code %d: %s)", e.code, e.message)
}

// feishuClientConfig is what the open platform tells a connection; PingInterval is in seconds.
type feishuClientConfig struct {
	PingInterval int `json:"PingInterval"`
}

// FeishuBot keeps the app's connection while admins have it turned on, and lets bound members
// release addresses from a chat through the same store the release console uses.
type FeishuBot struct {
	chatCore
	key    []byte
	client *http.Client

	writeMu sync.Mutex
	conn    *websocket.Conn

	// The settings and secret of the session running, and the app's access token for the REST API.
	sessionMu  sync.Mutex
	settings   botSettings
	secret     string
	token      string
	tokenUntil time.Time

	partsMu sync.Mutex
	parts   map[string]*feishuParts // a payload Feishu split across frames, by message_id
	seen    map[string]time.Time    // events handled, as Feishu may deliver one again
}

type feishuParts struct {
	chunks [][]byte
	at     time.Time
}

func NewFeishuBot(db *sql.DB, store Store, principals PrincipalFinder, loc *time.Location, publicURL string) (*FeishuBot, error) {
	key, err := credentialKey()
	if err != nil {
		return nil, err
	}
	return &FeishuBot{chatCore: newChatCore(db, store, principals, loc, publicURL), key: key, client: &http.Client{Timeout: 15 * time.Second},
		parts: map[string]*feishuParts{}, seen: map[string]time.Time{}}, nil
}

// Run keeps the bot connected while it is turned on; Feishu refusing the App ID or secret waits
// for new settings or a long pause.
func (b *FeishuBot) Run(ctx context.Context) {
	b.runBot(ctx, platformFeishu, b.session, func(err error) time.Duration {
		var auth feishuAuthError
		if errors.As(err, &auth) {
			return 5 * time.Minute
		}
		return 0
	})
}

// endpoint asks the open platform for a connection address with the app's ID and secret.
func (b *FeishuBot) endpoint(ctx context.Context, s botSettings, secret string) (string, feishuClientConfig, error) {
	var out struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			URL          string             `json:"URL"`
			ClientConfig feishuClientConfig `json:"ClientConfig"`
		} `json:"data"`
	}
	body, _ := json.Marshal(map[string]string{"AppID": s.appID, "AppSecret": secret})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.address, "/")+feishuEndpointPath, bytes.NewReader(body))
	if err != nil {
		return "", out.Data.ClientConfig, fmt.Errorf("cannot reach Feishu: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("locale", "zh")
	resp, err := b.client.Do(req)
	if err != nil {
		return "", out.Data.ClientConfig, fmt.Errorf("cannot reach Feishu: %v", err)
	}
	defer resp.Body.Close()
	decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out)
	switch {
	case resp.StatusCode != http.StatusOK:
		return "", out.Data.ClientConfig, fmt.Errorf("cannot reach Feishu: HTTP %d", resp.StatusCode)
	case decodeErr != nil:
		return "", out.Data.ClientConfig, errors.New("cannot reach Feishu: its answer could not be read")
	case out.Code == 1 || out.Code == 1000040343:
		return "", out.Data.ClientConfig, fmt.Errorf("Feishu is busy (code %d)", out.Code)
	case out.Code != 0:
		return "", out.Data.ClientConfig, feishuAuthError{code: out.Code, message: out.Msg}
	case out.Data.URL == "":
		return "", out.Data.ClientConfig, errors.New("Feishu gave no connection address")
	}
	return out.Data.URL, out.Data.ClientConfig, nil
}

// session connects with s and serves the connection until it fails, the settings change or ctx ends.
func (b *FeishuBot) session(ctx context.Context, s botSettings) error {
	secret, err := openLabelled(b.key, feishuSecretAAD, s.sealed)
	if err != nil {
		return errors.New("the stored secret cannot be decrypted; enter it again")
	}
	b.setState(ctx, platformFeishu, "connecting", "")
	address, conf, err := b.endpoint(ctx, s, secret)
	if err != nil {
		return err
	}
	target, err := url.Parse(address)
	if err != nil || target.Host == "" {
		return errors.New("Feishu gave no connection address")
	}
	dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	conn, resp, err := websocket.Dial(dialCtx, address, nil)
	cancel()
	if err != nil {
		// The address carries the connection's ticket, so only its host may appear in a reason.
		if resp != nil && resp.Header.Get("Handshake-Status") != "" {
			return fmt.Errorf("Feishu refused the connection (%s: %s)", resp.Header.Get("Handshake-Status"), resp.Header.Get("Handshake-Msg"))
		}
		return fmt.Errorf("cannot reach Feishu: %s", strings.ReplaceAll(err.Error(), address, target.Host))
	}
	conn.SetReadLimit(4 << 20)
	defer conn.CloseNow()
	service, _ := strconv.ParseInt(target.Query().Get("service_id"), 10, 32)
	ping := time.Duration(conf.PingInterval) * time.Second
	if ping <= 0 {
		ping = 2 * time.Minute
	}
	sessCtx, stop := context.WithCancel(ctx)
	defer stop()
	b.sessionMu.Lock()
	b.settings, b.secret, b.token = s, secret, ""
	b.sessionMu.Unlock()
	b.writeMu.Lock()
	b.conn = conn
	b.writeMu.Unlock()
	defer func() {
		b.writeMu.Lock()
		b.conn = nil
		b.writeMu.Unlock()
	}()
	b.setState(ctx, platformFeishu, "connected", "")

	frames, readErr := make(chan feishuFrame, 16), make(chan error, 1)
	go func() {
		for {
			_, data, err := conn.Read(sessCtx)
			if err != nil {
				readErr <- err
				return
			}
			frame, err := unmarshalFeishuFrame(data)
			if err != nil {
				continue
			}
			select {
			case frames <- frame:
			case <-sessCtx.Done():
				return
			}
		}
	}()
	heartbeat := time.NewTimer(0)
	defer heartbeat.Stop()
	settings, notify := time.NewTicker(b.SettingsEvery), time.NewTicker(b.NotifyEvery)
	defer settings.Stop()
	defer notify.Stop()
	lastHeard := time.Now()
	for {
		select {
		case <-ctx.Done():
			conn.Close(websocket.StatusNormalClosure, "")
			return ctx.Err()
		case err := <-readErr:
			return fmt.Errorf("Feishu closed the connection: %v", err)
		case frame := <-frames:
			lastHeard = time.Now()
			if frame.method == feishuControl {
				var next feishuClientConfig
				if frame.header("type") == "pong" && json.Unmarshal(frame.payload, &next) == nil && next.PingInterval > 0 {
					ping = time.Duration(next.PingInterval) * time.Second
				}
				continue
			}
			go b.onData(sessCtx, frame)
		case <-heartbeat.C:
			if time.Since(lastHeard) > 2*ping+5*time.Second {
				return errors.New("Feishu stopped answering the heartbeat")
			}
			if err := b.write(sessCtx, feishuFrame{service: int32(service), method: feishuControl, headers: [][2]string{{"type", "ping"}}}); err != nil {
				return fmt.Errorf("Feishu closed the connection: %v", err)
			}
			heartbeat.Reset(ping)
		case <-settings.C:
			if b.settingsChanged(ctx, platformFeishu, s.version) {
				conn.Close(websocket.StatusNormalClosure, "settings changed")
				return errBotSettingsChanged
			}
		case <-notify.C:
			b.notifyResults(sessCtx)
		}
	}
}

func (b *FeishuBot) write(ctx context.Context, frame feishuFrame) error {
	b.writeMu.Lock()
	defer b.writeMu.Unlock()
	if b.conn == nil {
		return errors.New("not connected")
	}
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return b.conn.Write(writeCtx, websocket.MessageBinary, frame.marshal())
}

// combine collects a payload Feishu split across frames; it is nil until every part is in.
func (b *FeishuBot) combine(messageID string, sum, seq int, chunk []byte) []byte {
	b.partsMu.Lock()
	defer b.partsMu.Unlock()
	for id, parts := range b.parts {
		if time.Since(parts.at) > 10*time.Second {
			delete(b.parts, id)
		}
	}
	if seq < 0 || seq >= sum || sum > 64 {
		return nil
	}
	parts := b.parts[messageID]
	if parts == nil || len(parts.chunks) != sum {
		parts = &feishuParts{chunks: make([][]byte, sum), at: time.Now()}
		b.parts[messageID] = parts
	}
	parts.chunks[seq] = chunk
	var whole []byte
	for _, part := range parts.chunks {
		if part == nil {
			return nil
		}
		whole = append(whole, part...)
	}
	delete(b.parts, messageID)
	return whole
}

// firstTime says whether an event is new; one delivered again is answered but not handled twice.
func (b *FeishuBot) firstTime(eventID string) bool {
	b.partsMu.Lock()
	defer b.partsMu.Unlock()
	for id, at := range b.seen {
		if time.Since(at) > 10*time.Minute {
			delete(b.seen, id)
		}
	}
	if eventID == "" {
		return true
	}
	if _, ok := b.seen[eventID]; ok {
		return false
	}
	b.seen[eventID] = time.Now()
	return true
}

// onData handles an event frame and answers it on the connection, as Feishu expects of every
// event: a message at once, before the reply goes out; a click with what replaces the card.
func (b *FeishuBot) onData(ctx context.Context, frame feishuFrame) {
	started := time.Now()
	payload := frame.payload
	if sum, _ := strconv.Atoi(frame.header("sum")); sum > 1 {
		seq, _ := strconv.Atoi(frame.header("seq"))
		if payload = b.combine(frame.header("message_id"), sum, seq, payload); payload == nil {
			return
		}
	}
	if kind := frame.header("type"); kind != "event" && kind != "card" {
		return
	}
	var envelope struct {
		Header struct {
			EventID   string `json:"event_id"`
			EventType string `json:"event_type"`
		} `json:"header"`
		Event json.RawMessage `json:"event"`
	}
	if json.Unmarshal(payload, &envelope) != nil {
		b.respond(ctx, frame, started, nil)
		return
	}
	switch envelope.Header.EventType {
	case feishuCardEvent:
		b.respond(ctx, frame, started, b.onClick(ctx, envelope.Event))
	case feishuMessageEvent:
		b.respond(ctx, frame, started, nil)
		if b.firstTime(envelope.Header.EventID) {
			b.onMessage(ctx, envelope.Event)
		}
	default:
		b.respond(ctx, frame, started, nil)
	}
}

// respond answers a data frame with the same frame carrying the result: HTTP-like code 200 and,
// for a click, the response as base64 JSON.
func (b *FeishuBot) respond(ctx context.Context, frame feishuFrame, started time.Time, data any) {
	out := map[string]any{"code": http.StatusOK, "headers": nil, "data": nil}
	if data != nil {
		if encoded, err := json.Marshal(data); err == nil {
			out["data"] = encoded
		}
	}
	frame.headers = append(append([][2]string(nil), frame.headers...), [2]string{"biz_rt", strconv.FormatInt(time.Since(started).Milliseconds(), 10)})
	frame.payload, _ = json.Marshal(out)
	if err := b.write(ctx, frame); err != nil && ctx.Err() == nil {
		log.Printf("feishu: answer event: %v", err)
	}
}

// feishuAccounts are a member's IDs a binding may name: the user ID admins see in Feishu's admin
// console when the app may read it, and the open_id it always gets.
func feishuAccounts(userID, openID string) []string {
	if userID != "" && userID != openID {
		return []string{userID, openID}
	}
	return []string{openID}
}

func (b *FeishuBot) onMessage(ctx context.Context, raw json.RawMessage) {
	var ev struct {
		Sender struct {
			SenderID struct {
				UserID string `json:"user_id"`
				OpenID string `json:"open_id"`
			} `json:"sender_id"`
			SenderType string `json:"sender_type"`
		} `json:"sender"`
		Message struct {
			MessageID   string `json:"message_id"`
			ChatID      string `json:"chat_id"`
			ChatType    string `json:"chat_type"`
			MessageType string `json:"message_type"`
			Content     string `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(raw, &ev) != nil || ev.Sender.SenderType != "user" || ev.Message.MessageID == "" || ev.Message.ChatID == "" {
		return
	}
	var content struct {
		Text string `json:"text"`
	}
	isText := ev.Message.MessageType == "text" && json.Unmarshal([]byte(ev.Message.Content), &content) == nil
	place := chatPlace{kind: "single", id: ev.Message.ChatID}
	if ev.Message.ChatType == "group" {
		place.kind = "group"
	}
	id := ev.Sender.SenderID
	message := b.handleMessage(ctx, platformFeishu, chatIncoming{text: content.Text, isText: isText, sender: id.OpenID, accounts: feishuAccounts(id.UserID, id.OpenID), place: place})
	if err := b.api(ctx, "/open-apis/im/v1/messages/"+url.PathEscape(ev.Message.MessageID)+"/reply",
		map[string]any{"msg_type": "interactive", "content": b.cardContent(message)}); err != nil && ctx.Err() == nil {
		log.Printf("feishu: reply: %v", err)
	}
}

// onClick answers a card click with the card that replaces it, or with a toast for the member
// who clicked alone.
func (b *FeishuBot) onClick(ctx context.Context, raw json.RawMessage) any {
	var ev struct {
		Operator struct {
			UserID string `json:"user_id"`
			OpenID string `json:"open_id"`
		} `json:"operator"`
		Action struct {
			Value map[string]any `json:"value"`
		} `json:"action"`
	}
	if json.Unmarshal(raw, &ev) != nil {
		return map[string]any{}
	}
	taskID, _ := ev.Action.Value["task"].(string)
	key, _ := ev.Action.Value["key"].(string)
	outcome := b.handleClick(ctx, platformFeishu, chatClick{taskID: taskID, key: key, sender: ev.Operator.OpenID, accounts: feishuAccounts(ev.Operator.UserID, ev.Operator.OpenID)})
	switch {
	case outcome == nil:
		return map[string]any{}
	case outcome.private:
		return map[string]any{"toast": map[string]string{"type": "warning", "content": outcome.toast}}
	}
	return map[string]any{"card": map[string]any{"type": "raw", "data": b.feishuCard(chatMessage{card: outcome.card})}}
}

// notifyResults tells each chat how the jobs started from it ended, through the REST API; a
// result Feishu did not take is sent again later, at most five times.
func (b *FeishuBot) notifyResults(ctx context.Context) {
	for _, r := range b.pendingResults(ctx, platformFeishu, nil) {
		if b.attempted(ctx, r.jobID) != nil {
			continue
		}
		err := b.api(ctx, "/open-apis/im/v1/messages?receive_id_type=chat_id",
			map[string]any{"receive_id": r.place.id, "msg_type": "interactive", "content": b.cardContent(r.message)})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("feishu: send result of %s: %v", r.jobID, err)
			continue
		}
		b.delivered(ctx, r.jobID)
	}
}

// tenantToken is the app's access token for the REST API, fetched again when it nears its end
// or Feishu turned it down (refresh).
func (b *FeishuBot) tenantToken(ctx context.Context, refresh bool) (token, base string, err error) {
	b.sessionMu.Lock()
	s, secret, token, until := b.settings, b.secret, b.token, b.tokenUntil
	b.sessionMu.Unlock()
	base = strings.TrimRight(s.address, "/")
	if !refresh && token != "" && time.Now().Before(until) {
		return token, base, nil
	}
	var out struct {
		Code   int    `json:"code"`
		Msg    string `json:"msg"`
		Token  string `json:"tenant_access_token"`
		Expire int    `json:"expire"`
	}
	if err = b.post(ctx, base+"/open-apis/auth/v3/tenant_access_token/internal", "", map[string]string{"app_id": s.appID, "app_secret": secret}, &out); err != nil {
		return "", base, err
	}
	if out.Code != 0 || out.Token == "" {
		return "", base, feishuAuthError{code: out.Code, message: out.Msg}
	}
	b.sessionMu.Lock()
	b.token, b.tokenUntil = out.Token, time.Now().Add(time.Duration(out.Expire)*time.Second-5*time.Minute)
	b.sessionMu.Unlock()
	return out.Token, base, nil
}

// post sends JSON to Feishu and reads its JSON answer into out.
func (b *FeishuBot) post(ctx context.Context, address, token string, body, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach Feishu: %v", err)
	}
	defer resp.Body.Close()
	if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
		return fmt.Errorf("cannot read Feishu's answer (HTTP %d)", resp.StatusCode)
	}
	return nil
}

// api posts to the open platform's REST API as the app; a turned-down token is fetched again once.
func (b *FeishuBot) api(ctx context.Context, path string, body any) error {
	for attempt := 0; ; attempt++ {
		token, base, err := b.tenantToken(ctx, attempt > 0)
		if err != nil {
			return err
		}
		var out struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		if err = b.post(ctx, base+path, token, body, &out); err != nil {
			return err
		}
		switch {
		case out.Code == 0:
			return nil
		case attempt == 0 && (out.Code == 99991661 || out.Code == 99991663 || out.Code == 99991668):
			continue
		}
		return fmt.Errorf("Feishu refused the request (code %d: %s)", out.Code, out.Msg)
	}
}

// feishuTones colour a card's header.
var feishuTones = map[string]string{"info": "blue", "success": "green", "warning": "orange", "danger": "red"}

func feishuText(text string) map[string]string {
	return map[string]string{"tag": "plain_text", "content": text}
}

func feishuMarkdown(text string) map[string]any {
	return map[string]any{"tag": "markdown", "content": text}
}

// feishuCard draws a message as a Feishu card (JSON 2.0): text alone becomes a card holding only
// Markdown, so replies read the same as cards do; buttons go two to a row.
func (b *FeishuBot) feishuCard(message chatMessage) map[string]any {
	out := map[string]any{"schema": "2.0", "config": map[string]any{"update_multi": true}}
	c := message.card
	if c == nil {
		out["body"] = map[string]any{"elements": []any{feishuMarkdown(message.text)}}
		return out
	}
	tone := feishuTones[c.tone]
	if tone == "" {
		tone = "grey"
	}
	header := map[string]any{"title": feishuText(c.title), "template": tone}
	if c.desc != "" {
		header["subtitle"] = feishuText(c.desc)
	}
	out["header"] = header
	var elements []any
	if len(c.facts) > 0 {
		lines := make([]string, 0, len(c.facts))
		for _, fact := range c.facts {
			lines = append(lines, fmt.Sprintf("**%s**：%s", fact[0], fact[1]))
		}
		elements = append(elements, feishuMarkdown(strings.Join(lines, "\n")))
	}
	if c.note != "" {
		elements = append(elements, feishuMarkdown(c.note))
	}
	for i := 0; i < len(c.buttons); i += 2 {
		var columns []any
		for _, button := range c.buttons[i:min(i+2, len(c.buttons))] {
			columns = append(columns, map[string]any{"tag": "column", "width": "weighted", "weight": 1, "elements": []any{map[string]any{
				"tag": "button", "text": feishuText(button.text), "type": "primary", "width": "fill",
				"behaviors": []any{map[string]any{"type": "callback", "value": map[string]string{"task": c.taskID, "key": button.key}}},
			}}})
		}
		elements = append(elements, map[string]any{"tag": "column_set", "horizontal_spacing": "8px", "columns": columns})
	}
	if c.jobID != "" && strings.HasPrefix(b.publicURL, "https://") {
		elements = append(elements, map[string]any{"tag": "button", "text": feishuText("在 Bind Plane 中查看"), "type": "default",
			"behaviors": []any{map[string]any{"type": "open_url", "default_url": b.consoleLink(c.jobID)}}})
	}
	if len(elements) == 0 {
		elements = append(elements, feishuMarkdown(c.title))
	}
	out["body"] = map[string]any{"elements": elements}
	return out
}

// cardContent is a message as Feishu's REST API takes it: the card's JSON, as a string.
func (b *FeishuBot) cardContent(message chatMessage) string {
	data, _ := json.Marshal(b.feishuCard(message))
	return string(data)
}
