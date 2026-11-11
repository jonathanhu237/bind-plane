package release

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

// fakeFeishu stands in for Feishu's open platform: it hands out a connection address, takes the
// bot's protobuf frames, pushes members' messages and clicks as events, and records the REST
// calls the bot makes to reply and send results.
type fakeFeishu struct {
	t         *testing.T
	server    *httptest.Server
	mu        sync.Mutex
	conn      *websocket.Conn
	seq       uint64
	endpoints chan map[string]string
	frames    chan feishuFrame
	calls     chan feishuCall
}

type feishuCall struct {
	path, auth string
	body       map[string]any
}

func newFakeFeishu(t *testing.T) *fakeFeishu {
	f := &fakeFeishu{t: t, endpoints: make(chan map[string]string, 8), frames: make(chan feishuFrame, 64), calls: make(chan feishuCall, 64)}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /callback/ws/endpoint", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.endpoints <- in
		if in["AppSecret"] != "app-s3cret" {
			_, _ = io.WriteString(w, `{"code":1000040346,"msg":"invalid app secret","data":{}}`)
			return
		}
		fmt.Fprintf(w, `{"code":0,"msg":"ok","data":{"URL":"ws%s/ws?device_id=d1&service_id=9&access_key=TICKET","ClientConfig":{"PingInterval":120}}}`, strings.TrimPrefix(f.server.URL, "http"))
	})
	mux.HandleFunc("GET /ws", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("access_key") != "TICKET" {
			w.Header().Set("Handshake-Status", "403")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		f.mu.Lock()
		f.conn = conn
		f.mu.Unlock()
		for {
			_, data, err := conn.Read(context.Background())
			if err != nil {
				return
			}
			if frame, err := unmarshalFeishuFrame(data); err == nil {
				f.frames <- frame
			}
		}
	})
	mux.HandleFunc("POST /open-apis/auth/v3/tenant_access_token/internal", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["app_id"] != "cli_1" || in["app_secret"] != "app-s3cret" {
			_, _ = io.WriteString(w, `{"code":10014,"msg":"app secret invalid"}`)
			return
		}
		_, _ = io.WriteString(w, `{"code":0,"msg":"ok","tenant_access_token":"t-1","expire":7200}`)
	})
	record := func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.calls <- feishuCall{path: r.URL.RequestURI(), auth: r.Header.Get("Authorization"), body: body}
		_, _ = io.WriteString(w, `{"code":0,"msg":"success","data":{"message_id":"om_sent"}}`)
	}
	mux.HandleFunc("POST /open-apis/im/v1/messages/{id}/reply", record)
	mux.HandleFunc("POST /open-apis/im/v1/messages", record)
	f.server = httptest.NewServer(mux)
	return f
}

// push sends an event to the bot, as Feishu does, and returns the frame's message_id.
func (f *fakeFeishu) push(eventType, eventID string, event any) string {
	f.t.Helper()
	payload, _ := json.Marshal(map[string]any{"schema": "2.0", "header": map[string]string{"event_id": eventID, "event_type": eventType}, "event": event})
	f.mu.Lock()
	f.seq++
	id := fmt.Sprintf("msg-%d", f.seq)
	frame := feishuFrame{seqID: f.seq, service: 9, method: feishuData, payload: payload,
		headers: [][2]string{{"type", "event"}, {"message_id", id}, {"sum", "1"}, {"seq", "0"}, {"trace_id", "tr"}}}
	conn := f.conn
	f.mu.Unlock()
	if err := conn.Write(context.Background(), websocket.MessageBinary, frame.marshal()); err != nil {
		f.t.Fatalf("push: %v", err)
	}
	return id
}

// answer is the bot's answer to the frame with the given message_id: its payload, and the
// response it carries (a click's), decoded.
func (f *fakeFeishu) answer(id string) (map[string]any, map[string]any) {
	f.t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case frame := <-f.frames:
			if frame.method != feishuData || frame.header("message_id") != id {
				continue
			}
			if frame.header("biz_rt") == "" || frame.service != 9 {
				f.t.Fatalf("answer frame %+v", frame)
			}
			var payload map[string]any
			_ = json.Unmarshal(frame.payload, &payload)
			var data map[string]any
			if encoded, ok := payload["data"].(string); ok {
				raw, _ := base64.StdEncoding.DecodeString(encoded)
				_ = json.Unmarshal(raw, &data)
			}
			return payload, data
		case <-deadline:
			f.t.Fatalf("bot never answered %s", id)
		}
	}
}

// call is the bot's next REST call, with the card it carries decoded.
func (f *fakeFeishu) call() (feishuCall, map[string]any) {
	f.t.Helper()
	select {
	case c := <-f.calls:
		var card map[string]any
		_ = json.Unmarshal([]byte(fmt.Sprint(c.body["content"])), &card)
		if c.auth != "Bearer t-1" || c.body["msg_type"] != "interactive" {
			f.t.Fatalf("REST call %+v", c)
		}
		return c, card
	case <-time.After(5 * time.Second):
		f.t.Fatalf("bot made no REST call")
	}
	return feishuCall{}, nil
}

func feishuMessage(user, open, chatType, chat, messageID, text string) map[string]any {
	content, _ := json.Marshal(map[string]string{"text": text})
	return map[string]any{
		"sender":  map[string]any{"sender_id": map[string]string{"user_id": user, "open_id": open}, "sender_type": "user"},
		"message": map[string]any{"message_id": messageID, "chat_id": chat, "chat_type": chatType, "message_type": "text", "content": string(content)},
	}
}

func feishuClick(user, open, task, key string) map[string]any {
	return map[string]any{"operator": map[string]string{"user_id": user, "open_id": open}, "token": "c-1",
		"action": map[string]any{"tag": "button", "value": map[string]string{"task": task, "key": key}}, "host": "im_message",
		"context": map[string]string{"open_message_id": "om_card", "open_chat_id": "oc_group"}}
}

// cardButtons are the callback values on a Feishu card, in order.
func cardButtons(card map[string]any) []map[string]any {
	var found []map[string]any
	var walk func(any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if v["type"] == "callback" {
				found = append(found, v["value"].(map[string]any))
			}
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(card)
	return found
}

func TestFeishuBotReleasesFromAChat(t *testing.T) {
	dsn := os.Getenv("BIND_PLANE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated test database required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var adminID, operatorID, credential, profile, switchID string
	for _, row := range []struct {
		into  *string
		query string
	}{
		{&adminID, `INSERT INTO auth_users(name,email,email_canonical,password_hash) VALUES ('Feishu Admin','feishuadmin@test.invalid','feishuadmin@test.invalid','x') RETURNING id::text`},
		{&operatorID, `INSERT INTO auth_users(name,email,email_canonical,password_hash) VALUES ('李四','lisi@test.invalid','lisi@test.invalid','x') RETURNING id::text`},
		{&credential, `INSERT INTO bp_credentials(name,username,encrypted_password) VALUES ('feishu-test','test','ciphertext') RETURNING id::text`},
		{&profile, `INSERT INTO bp_command_profiles(name) VALUES ('feishu-test') RETURNING id::text`},
	} {
		if err = db.QueryRow(row.query).Scan(row.into); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.QueryRow(`INSERT INTO bp_switches(name,management_ip,credential_id,command_profile_id,handles_release) VALUES ('FEISHU-SW','192.0.2.202',$1,$2,true) RETURNING id::text`, credential, profile).Scan(&switchID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO bp_networks(switch_id,cidr,is_validated) VALUES ($1,'10.178.0.0/16',true)`, switchID); err != nil {
		t.Fatal(err)
	}
	admin := fakeIdentity{principal: domain.Principal{User: domain.User{ID: adminID, Email: "feishuadmin@test.invalid", Name: "Feishu Admin"}, SuperAdmin: true}}
	ops, err := NewOpsHandler(db, admin, "session", "http://localhost:5173", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path, body string, want int) map[string]any {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "http://localhost:5173")
		r.AddCookie(&http.Cookie{Name: "session", Value: "token"})
		w := httptest.NewRecorder()
		ops.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: status %d, want %d: %s", method, path, w.Code, want, w.Body.String())
		}
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	settings := func() map[string]any {
		return call("GET", "/api/ops/chat/feishu", "", 200)["settings"].(map[string]any)
	}
	version := func() int { return int(settings()["version"].(float64)) }
	waitState := func(state string) map[string]any {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			if s := settings(); s["state"] == state {
				return s
			}
		}
		t.Fatalf("bot never %s: %v", state, settings())
		return nil
	}

	// The app is set on the Feishu page like the WeCom bot: an App ID and secret, the open
	// platform's address (not a WebSocket one), and members bound by their Feishu ID.
	server := newFakeFeishu(t)
	defer server.server.Close()
	call("GET", "/api/ops/chat/teams", "", 404)
	if got := fmt.Sprint(call("PATCH", "/api/ops/chat/feishu/settings", fmt.Sprintf(`{"version":%d,"is_enabled":true}`, version()), 422)); !strings.Contains(got, errBotNotReady) {
		t.Fatalf("turned on without credentials: %s", got)
	}
	call("PATCH", "/api/ops/chat/feishu/settings", fmt.Sprintf(`{"version":%d,"address":"wss://open.feishu.cn"}`, version()), 422)
	call("PATCH", "/api/ops/chat/feishu/settings", fmt.Sprintf(`{"version":%d,"app_id":"cli_1","secret":"app-s3cret","address":%q}`, version(), server.server.URL+"/"), 200)
	if s := settings(); s["has_secret"] != true || s["address"] != server.server.URL || strings.Contains(fmt.Sprint(s), "s3cret") {
		t.Fatalf("settings %v", s)
	}
	call("PUT", "/api/ops/chat/feishu/accounts/"+operatorID, `{"external_id":"u_lisi"}`, 200)
	call("PUT", "/api/ops/chat/feishu/accounts/"+adminID, `{"external_id":"u_lisi"}`, 409)
	if accounts := call("GET", "/api/ops/chat/wecom", "", 200)["accounts"]; strings.Contains(fmt.Sprint(accounts), "u_lisi") {
		t.Fatalf("a Feishu binding shows on the WeCom page: %v", accounts)
	}
	var audited int
	if err = db.QueryRow(`SELECT count(*) FROM bp_asset_events WHERE action IN ('feishu.update','feishu.account') AND actor_id=$1`, adminID).Scan(&audited); err != nil || audited != 2 {
		t.Fatalf("Feishu changes audited %d times: %v", audited, err)
	}

	principals := fakePrincipals{operatorID: {User: domain.User{ID: operatorID, Name: "李四", Email: "lisi@test.invalid"}, Roles: []domain.Role{{Name: "Operator"}}}}
	bot, err := NewFeishuBot(db, NewSQLStore(db), principals, time.UTC, "https://bind-plane.test/")
	if err != nil {
		t.Fatal(err)
	}
	bot.SettingsEvery, bot.NotifyEvery = 50*time.Millisecond, 50*time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go bot.Run(ctx)
	call("PATCH", "/api/ops/chat/feishu/settings", fmt.Sprintf(`{"version":%d,"is_enabled":true}`, version()), 200)

	// It asks for a connection with the App ID and secret, connects, and pings on the service.
	if in := <-server.endpoints; in["AppID"] != "cli_1" || in["AppSecret"] != "app-s3cret" {
		t.Fatalf("endpoint request %v", in)
	}
	if s := waitState("connected"); s["connected_at"] == nil || s["last_error"] != nil {
		t.Fatalf("connected %v", s)
	}
	select {
	case ping := <-server.frames:
		if ping.method != feishuControl || ping.header("type") != "ping" || ping.service != 9 {
			t.Fatalf("first frame %+v", ping)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no ping")
	}

	// Every event is answered on the connection; the reply goes out through the REST API.
	if payload, _ := server.answer(server.push(feishuMessageEvent, "ev1", feishuMessage("u_stranger", "ou_stranger", "p2p", "oc_p2p", "om_1", "解绑 10.178.1.20"))); payload["code"] != float64(200) {
		t.Fatalf("event answered with %v", payload)
	}
	if reply, card := server.call(); reply.path != "/open-apis/im/v1/messages/om_1/reply" || !strings.Contains(fmt.Sprint(card), "还没有绑定") || !strings.Contains(fmt.Sprint(card), "u_stranger") {
		t.Fatalf("stranger got %+v %v", reply, card)
	}
	// An event Feishu delivers again is not handled twice.
	server.answer(server.push(feishuMessageEvent, "ev2", feishuMessage("u_lisi", "ou_lisi", "p2p", "oc_p2p", "om_2", "你好")))
	server.answer(server.push(feishuMessageEvent, "ev2", feishuMessage("u_lisi", "ou_lisi", "p2p", "oc_p2p", "om_2", "你好")))
	if _, card := server.call(); !strings.Contains(fmt.Sprint(card), "没看懂") {
		t.Fatalf("not a command: %v", card)
	}
	server.answer(server.push(feishuMessageEvent, "ev3", feishuMessage("u_lisi", "ou_lisi", "p2p", "oc_p2p", "om_3", "帮助")))
	if reply, card := server.call(); reply.path != "/open-apis/im/v1/messages/om_3/reply" || strings.Contains(fmt.Sprint(card), "没看懂") || !strings.Contains(fmt.Sprint(card), "查询 10.20.30.40") {
		t.Fatalf("help %+v %v", reply, card)
	}

	// In a group the message starts with Feishu's mention placeholder; the card offers the reasons.
	server.answer(server.push(feishuMessageEvent, "ev4", feishuMessage("u_lisi", "ou_lisi", "group", "oc_group", "om_4", "@_user_1 解绑 10.178.1.20")))
	_, card := server.call()
	buttons := cardButtons(card)
	if len(buttons) != 6 || !strings.Contains(fmt.Sprint(card["header"]), "解绑 10.178.1.20") || !strings.Contains(fmt.Sprint(card["header"]), "FEISHU-SW") {
		t.Fatalf("release card %v", card)
	}
	taskID := buttons[0]["task"].(string)
	// Someone else's click is turned away with a toast, leaving the card as it is.
	if _, data := server.answer(server.push(feishuCardEvent, "ev5", feishuClick("u_other", "ou_other", taskID, "temporary_test"))); !strings.Contains(fmt.Sprint(data["toast"]), "只有发起人") || data["card"] != nil {
		t.Fatalf("someone else's click %v", data)
	}
	_, data := server.answer(server.push(feishuCardEvent, "ev6", feishuClick("u_lisi", "ou_lisi", taskID, "temporary_test")))
	if !strings.Contains(fmt.Sprint(data["card"]), "已提交") || !strings.Contains(fmt.Sprint(data["card"]), "https://bind-plane.test/release?job=") {
		t.Fatalf("click %v", data)
	}
	var jobID, source, actor, reason string
	if err = db.QueryRow(`SELECT j.id::text,j.source,j.actor_id::text,j.reason FROM bp_release_jobs j JOIN bp_chat_jobs w ON w.job_id=j.id
 WHERE j.target_ip='10.178.1.20' AND w.platform='feishu' AND w.chat_id='oc_group' AND w.owner_id='ou_lisi'`).Scan(&jobID, &source, &actor, &reason); err != nil ||
		source != "feishu" || actor != operatorID || reason != "temporary_test" {
		t.Fatalf("job %s from %s by %s for %s: %v", jobID, source, actor, reason, err)
	}
	// A second click on the same card changes nothing.
	if _, data := server.answer(server.push(feishuCardEvent, "ev7", feishuClick("u_lisi", "ou_lisi", taskID, "user_report"))); len(data) != 0 {
		t.Fatalf("second click %v", data)
	}

	// A reason in the command releases at once; a lookup reports what the switch holds.
	server.answer(server.push(feishuMessageEvent, "ev8", feishuMessage("u_lisi", "ou_lisi", "p2p", "oc_p2p", "om_8", "解绑10.178.1.22 IP/MAC变更")))
	if _, card := server.call(); !strings.Contains(fmt.Sprint(card), "已提交解绑 10.178.1.22") {
		t.Fatalf("release with a reason %v", card)
	}
	server.answer(server.push(feishuMessageEvent, "ev9", feishuMessage("u_lisi", "ou_lisi", "group", "oc_group", "om_9", "@_user_1 查询 10.178.1.30")))
	if _, card := server.call(); !strings.Contains(fmt.Sprint(card), "正在向交换机 FEISHU-SW") {
		t.Fatalf("lookup %v", card)
	}
	var lookupID string
	if err = db.QueryRow(`SELECT id::text FROM bp_release_jobs WHERE target_ip='10.178.1.30' AND kind='lookup' AND source='feishu'`).Scan(&lookupID); err != nil {
		t.Fatalf("lookup job: %v", err)
	}
	if _, err = db.Exec(`UPDATE bp_release_jobs SET status='succeeded',phase='finished',finished_at=now(),before_state='{"entry_type":"static","mac":"00e0-fc12-3456"}' WHERE id=$1`, lookupID); err != nil {
		t.Fatal(err)
	}
	sent, found := server.call()
	if sent.path != "/open-apis/im/v1/messages?receive_id_type=chat_id" || sent.body["receive_id"] != "oc_group" || !strings.Contains(fmt.Sprint(found), "查询结果") ||
		!strings.Contains(fmt.Sprint(found), "00e0-fc12-3456") || len(cardButtons(found)) != 1 {
		t.Fatalf("lookup result %+v %v", sent, found)
	}
	if _, data := server.answer(server.push(feishuCardEvent, "ev10", feishuClick("u_lisi", "ou_lisi", cardButtons(found)[0]["task"].(string), chatOffer))); len(cardButtons(data["card"].(map[string]any)["data"].(map[string]any))) != 6 {
		t.Fatalf("reasons offered %v", data)
	}

	// When a release ends, the chat hears how; a result Feishu took is not sent again.
	if _, err = db.Exec(`UPDATE bp_release_jobs SET status='succeeded',phase='finished',finished_at=now() WHERE id=$1`, jobID); err != nil {
		t.Fatal(err)
	}
	if sent, result := server.call(); sent.body["receive_id"] != "oc_group" || !strings.Contains(fmt.Sprint(result), "已解绑") {
		t.Fatalf("result %+v %v", sent, result)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		var done bool
		if db.QueryRow(`SELECT notified_at IS NOT NULL FROM bp_chat_jobs WHERE job_id=$1`, jobID).Scan(&done) == nil && done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("result never recorded as sent")
		}
	}

	// An unknown outcome comes with Release again, which starts a release linked to it.
	if err = db.QueryRow(`INSERT INTO bp_release_jobs(target_ip,kind,reason,status,phase,actor_id,actor_email,actor_name,switch_id,command_profile_id,switch_version,command_profile_version,switch_name,switch_management_ip,source,finished_at)
 SELECT '10.178.1.21','release','user_report','needs_manual_confirmation','finished',$1,'lisi@test.invalid','李四',s.id,s.command_profile_id,s.version,p.version,s.name,s.management_ip,'feishu',now()
 FROM bp_switches s JOIN bp_command_profiles p ON p.id=s.command_profile_id WHERE s.id=$2 RETURNING id::text`, operatorID, switchID).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO bp_chat_jobs(job_id,platform,owner_id,chat_type,chat_id) VALUES ($1,'feishu','ou_lisi','single','oc_p2p')`, jobID); err != nil {
		t.Fatal(err)
	}
	_, retryCard := server.call()
	if !strings.Contains(fmt.Sprint(retryCard), "结果未知") || len(cardButtons(retryCard)) != 1 || cardButtons(retryCard)[0]["key"] != "retry" {
		t.Fatalf("unknown outcome %v", retryCard)
	}
	if _, data := server.answer(server.push(feishuCardEvent, "ev11", feishuClick("u_lisi", "ou_lisi", cardButtons(retryCard)[0]["task"].(string), "retry"))); !strings.Contains(fmt.Sprint(data["card"]), "已提交") {
		t.Fatalf("release again %v", data)
	}
	var retries int
	if err = db.QueryRow(`SELECT count(*) FROM bp_release_jobs WHERE retry_of_id=$1 AND source='feishu' AND actor_id=$2`, jobID, operatorID).Scan(&retries); err != nil || retries != 1 {
		t.Fatalf("released again %d times: %v", retries, err)
	}

	// Turning it off disconnects; Feishu refusing the secret is a failure with its reason.
	call("PATCH", "/api/ops/chat/feishu/settings", fmt.Sprintf(`{"version":%d,"is_enabled":false}`, version()), 200)
	waitState("disabled")
	call("PATCH", "/api/ops/chat/feishu/settings", fmt.Sprintf(`{"version":%d,"is_enabled":true,"secret":"wrong"}`, version()), 200)
	if s := waitState("failed"); !strings.Contains(fmt.Sprint(s["last_error"]), "refused the App ID or secret (code 1000040346") {
		t.Fatalf("refused %v", s)
	}
	cancel()
}
