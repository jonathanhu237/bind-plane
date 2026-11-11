package release

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

type fakePrincipals map[string]domain.Principal

func (f fakePrincipals) FindPrincipalByID(_ context.Context, id string) (domain.Principal, error) {
	if p, ok := f[id]; ok {
		return p, nil
	}
	return domain.Principal{}, sql.ErrNoRows
}

// fakeWeCom stands in for WeCom's long-connection server: it records what the bot sends and
// pushes members' messages and clicks to it.
type fakeWeCom struct {
	t      *testing.T
	server *httptest.Server
	mu     sync.Mutex
	conn   *websocket.Conn
	in     chan map[string]any
}

func newFakeWeCom(t *testing.T) *fakeWeCom {
	f := &fakeWeCom{t: t, in: make(chan map[string]any, 64)}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			var frame map[string]any
			if json.Unmarshal(data, &frame) == nil {
				f.in <- frame
			}
		}
	}))
	return f
}

func (f *fakeWeCom) url() string { return "ws" + strings.TrimPrefix(f.server.URL, "http") }

func (f *fakeWeCom) push(frame any) {
	f.t.Helper()
	data, _ := json.Marshal(frame)
	f.mu.Lock()
	conn := f.conn
	f.mu.Unlock()
	if err := conn.Write(context.Background(), websocket.MessageText, data); err != nil {
		f.t.Fatalf("push: %v", err)
	}
}

// next is the bot's next frame other than a heartbeat, which must be a cmd frame.
func (f *fakeWeCom) next(cmd string) map[string]any {
	f.t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case frame := <-f.in:
			if frame["cmd"] == wecomPing {
				continue
			}
			if frame["cmd"] != cmd {
				f.t.Fatalf("bot sent %v, want %s", frame, cmd)
			}
			return frame
		case <-deadline:
			f.t.Fatalf("bot sent no %s", cmd)
		}
	}
}

func (f *fakeWeCom) ack(frame map[string]any, code int) {
	f.push(map[string]any{"headers": frame["headers"], "errcode": code, "errmsg": "ok"})
}

func wecomText(reqID, chatType, chatID, user, text string) map[string]any {
	return map[string]any{"cmd": wecomMessage, "headers": map[string]string{"req_id": reqID},
		"body": map[string]any{"msgid": reqID, "chattype": chatType, "chatid": chatID, "from": map[string]string{"userid": user}, "msgtype": "text", "text": map[string]string{"content": text}}}
}

func wecomClick(reqID, user, taskID, key string) map[string]any {
	return map[string]any{"cmd": wecomEvent, "headers": map[string]string{"req_id": reqID},
		"body": map[string]any{"msgid": reqID, "chattype": "group", "chatid": "grp1", "from": map[string]string{"userid": user}, "msgtype": "event",
			"event": map[string]any{"eventtype": "template_card_event",
				"template_card_event": map[string]string{"card_type": "button_interaction", "event_key": key, "task_id": taskID}}}}
}

func frameBody(frame map[string]any) map[string]any { return frame["body"].(map[string]any) }

func TestWeComBotReleasesFromAChat(t *testing.T) {
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
		{&adminID, `INSERT INTO auth_users(name,email,email_canonical,password_hash) VALUES ('WeCom Admin','wecomadmin@test.invalid','wecomadmin@test.invalid','x') RETURNING id::text`},
		{&operatorID, `INSERT INTO auth_users(name,email,email_canonical,password_hash) VALUES ('张三','zhangsan@test.invalid','zhangsan@test.invalid','x') RETURNING id::text`},
		{&credential, `INSERT INTO bp_credentials(name,username,encrypted_password) VALUES ('wecom-test','test','ciphertext') RETURNING id::text`},
		{&profile, `INSERT INTO bp_command_profiles(name) VALUES ('wecom-test') RETURNING id::text`},
	} {
		if err = db.QueryRow(row.query).Scan(row.into); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.QueryRow(`INSERT INTO bp_switches(name,management_ip,credential_id,command_profile_id,handles_release) VALUES ('WECOM-SW','192.0.2.201',$1,$2,true) RETURNING id::text`, credential, profile).Scan(&switchID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO bp_networks(switch_id,cidr,is_validated) VALUES ($1,'10.177.0.0/16',true)`, switchID); err != nil {
		t.Fatal(err)
	}
	admin := fakeIdentity{principal: domain.Principal{User: domain.User{ID: adminID, Email: "wecomadmin@test.invalid", Name: "WeCom Admin"}, SuperAdmin: true}}
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
	settings := func() map[string]any { return call("GET", "/api/ops/wecom", "", 200)["settings"].(map[string]any) }
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

	// The bot is set on the WeCom page: it cannot be turned on without an ID and secret, the secret
	// is never shown again, and members are bound to users by their WeCom account.
	server := newFakeWeCom(t)
	defer server.server.Close()
	if got := fmt.Sprint(call("PATCH", "/api/ops/wecom/settings", fmt.Sprintf(`{"version":%d,"is_enabled":true}`, version()), 422)); !strings.Contains(got, errWecomNotReady) {
		t.Fatalf("turned on without credentials: %s", got)
	}
	call("PATCH", "/api/ops/wecom/settings", fmt.Sprintf(`{"version":%d,"ws_url":"http://example.test"}`, version()), 422)
	call("PATCH", "/api/ops/wecom/settings", fmt.Sprintf(`{"version":%d,"bot_id":"bot-1","secret":"s3cret-value","ws_url":%q}`, version(), server.url()), 200)
	if s := settings(); s["has_secret"] != true || strings.Contains(fmt.Sprint(s), "s3cret") {
		t.Fatalf("settings %v", s)
	}
	call("PUT", "/api/ops/wecom/accounts/"+operatorID, `{"wecom_userid":"bad id"}`, 422)
	call("PUT", "/api/ops/wecom/accounts/"+operatorID, `{"wecom_userid":"zhang.san"}`, 200)
	call("PUT", "/api/ops/wecom/accounts/"+adminID, `{"wecom_userid":"zhang.san"}`, 409)
	var audited int
	if err = db.QueryRow(`SELECT count(*) FROM bp_asset_events WHERE action IN ('wecom.update','wecom.account') AND actor_id=$1`, adminID).Scan(&audited); err != nil || audited != 2 {
		t.Fatalf("WeCom changes audited %d times: %v", audited, err)
	}

	principals := fakePrincipals{operatorID: {User: domain.User{ID: operatorID, Name: "张三", Email: "zhangsan@test.invalid"}, Roles: []domain.Role{{Name: "Operator"}}}}
	bot, err := NewWeComBot(db, NewSQLStore(db), principals, time.UTC, "https://bind-plane.test/")
	if err != nil {
		t.Fatal(err)
	}
	bot.SettingsEvery, bot.NotifyEvery, bot.HeartbeatEvery = 50*time.Millisecond, 50*time.Millisecond, time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go bot.Run(ctx)
	call("PATCH", "/api/ops/wecom/settings", fmt.Sprintf(`{"version":%d,"is_enabled":true}`, version()), 200)

	// It signs in with the bot's ID and secret.
	subscribe := server.next(wecomSubscribe)
	if b := frameBody(subscribe); b["bot_id"] != "bot-1" || b["secret"] != "s3cret-value" {
		t.Fatalf("sign-in %v", subscribe)
	}
	server.ack(subscribe, 0)
	if s := waitState("connected"); s["connected_at"] == nil || s["last_error"] != nil {
		t.Fatalf("connected %v", s)
	}

	// A member nobody bound is told so; anything without an address gets the help.
	server.push(wecomText("m1", "single", "", "stranger", "解绑 10.177.1.20"))
	if reply := server.next(wecomReply); !strings.Contains(fmt.Sprint(reply), "还没有绑定") || reply["headers"].(map[string]any)["req_id"] != "m1" {
		t.Fatalf("stranger got %v", reply)
	}
	server.push(wecomText("m2", "single", "", "zhang.san", "你好"))
	if reply := server.next(wecomReply); !strings.Contains(fmt.Sprint(reply), "查询 10.20.30.40") {
		t.Fatalf("help %v", reply)
	}
	// "查询" asks the switch: a lookup job, and what it found sent back to the chat. A static
	// binding comes with the reasons, and choosing one releases it.
	server.push(wecomText("m3", "group", "grp1", "zhang.san", "@Bind Plane 查询 10.177.1.30"))
	if reply := server.next(wecomReply); !strings.Contains(fmt.Sprint(reply), "正在向交换机 WECOM-SW") {
		t.Fatalf("lookup %v", reply)
	}
	var lookupID string
	if err = db.QueryRow(`SELECT id::text FROM bp_release_jobs WHERE target_ip='10.177.1.30' AND kind='lookup' AND source='wecom' AND actor_id=$1`, operatorID).Scan(&lookupID); err != nil {
		t.Fatalf("lookup job: %v", err)
	}
	if _, err = db.Exec(`UPDATE bp_release_jobs SET status='succeeded',phase='finished',finished_at=now(),before_state='{"entry_type":"static","mac":"00e0-fc12-3456"}' WHERE id=$1`, lookupID); err != nil {
		t.Fatal(err)
	}
	found := server.next(wecomSend)
	foundCard := frameBody(found)["template_card"].(map[string]any)
	if frameBody(found)["chatid"] != "grp1" || !strings.Contains(fmt.Sprint(foundCard), "00e0-fc12-3456") || !strings.Contains(fmt.Sprint(foundCard), "静态绑定") || len(foundCard["button_list"].([]any)) != 6 {
		t.Fatalf("lookup result %v", found)
	}
	server.ack(found, 0)
	server.push(wecomClick("e0", "zhang.san", foundCard["task_id"].(string), "user_report"))
	if update := frameBody(server.next(wecomUpdateCard)); !strings.Contains(fmt.Sprint(update), "已提交") {
		t.Fatalf("release from a lookup %v", update)
	}
	var fromLookup int
	if err = db.QueryRow(`SELECT count(*) FROM bp_release_jobs WHERE target_ip='10.177.1.30' AND kind='release' AND reason='user_report' AND source='wecom'`).Scan(&fromLookup); err != nil || fromLookup != 1 {
		t.Fatalf("released from a lookup %d times: %v", fromLookup, err)
	}

	// An address gets its release card: the switch, and one button per reason.
	server.push(wecomText("m4", "group", "grp1", "zhang.san", "@Bind Plane 解绑 10.177.1.20"))
	card := frameBody(server.next(wecomReply))["template_card"].(map[string]any)
	taskID, _ := card["task_id"].(string)
	if card["card_type"] != "button_interaction" || len(card["button_list"].([]any)) != 6 || !strings.Contains(fmt.Sprint(card["main_title"]), "WECOM-SW") || taskID == "" {
		t.Fatalf("release card %v", card)
	}

	// Only the member who asked may choose; their reason starts the release, once.
	server.push(wecomClick("e1", "li.si", taskID, "temporary_test"))
	if update := frameBody(server.next(wecomUpdateCard)); !strings.Contains(fmt.Sprint(update), "只有发起人") || fmt.Sprint(update["userids"]) != "[li.si]" {
		t.Fatalf("someone else's click %v", update)
	}
	server.push(wecomClick("e2", "zhang.san", taskID, "temporary_test"))
	// WeCom requires a notice card to lead somewhere: it opens the job in the release console.
	if update := frameBody(server.next(wecomUpdateCard)); !strings.Contains(fmt.Sprint(update), "已提交") ||
		!strings.HasPrefix(fmt.Sprint(update["template_card"].(map[string]any)["card_action"].(map[string]any)["url"]), "https://bind-plane.test/release?job=") {
		t.Fatalf("click %v", update)
	}
	var jobID, source, actor, reason string
	if err = db.QueryRow(`SELECT id::text,source,actor_id::text,reason FROM bp_release_jobs WHERE target_ip='10.177.1.20' ORDER BY created_at DESC LIMIT 1`).Scan(&jobID, &source, &actor, &reason); err != nil ||
		source != "wecom" || actor != operatorID || reason != "temporary_test" {
		t.Fatalf("job %s from %s by %s for %s: %v", jobID, source, actor, reason, err)
	}
	server.push(wecomText("m5", "group", "grp1", "zhang.san", "解绑 10.177.1.20"))
	if reply := server.next(wecomReply); !strings.Contains(fmt.Sprint(reply), "已有一次解绑") {
		t.Fatalf("busy address %v", reply)
	}

	// When it ends, the chat hears how; WeCom's acknowledgement stops it being sent again.
	if _, err = db.Exec(`UPDATE bp_release_jobs SET status='succeeded',phase='finished',finished_at=now() WHERE id=$1`, jobID); err != nil {
		t.Fatal(err)
	}
	result := server.next(wecomSend)
	if b := frameBody(result); b["chatid"] != "grp1" || b["msgtype"] != "markdown" || !strings.Contains(fmt.Sprint(b), "已解绑") {
		t.Fatalf("result %v", result)
	}
	server.ack(result, 0)
	waitNotified := func(id string) {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			var done bool
			if db.QueryRow(`SELECT notified_at IS NOT NULL FROM bp_wecom_jobs WHERE job_id=$1`, id).Scan(&done) == nil && done {
				return
			}
		}
		t.Fatalf("result of %s never recorded as sent", id)
	}
	waitNotified(jobID)

	// An unknown outcome comes with a card to release again, which starts a release linked to it.
	if err = db.QueryRow(`INSERT INTO bp_release_jobs(target_ip,kind,reason,status,phase,actor_id,actor_email,actor_name,switch_id,command_profile_id,switch_version,command_profile_version,switch_name,switch_management_ip,source,finished_at)
 SELECT '10.177.1.21','release','user_report','needs_manual_confirmation','finished',$1,'zhangsan@test.invalid','张三',s.id,s.command_profile_id,s.version,p.version,s.name,s.management_ip,'wecom',now()
 FROM bp_switches s JOIN bp_command_profiles p ON p.id=s.command_profile_id WHERE s.id=$2 RETURNING id::text`, operatorID, switchID).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO bp_wecom_jobs(job_id,wecom_userid,chat_type,chat_id) VALUES ($1,'zhang.san','single','zhang.san')`, jobID); err != nil {
		t.Fatal(err)
	}
	result = server.next(wecomSend)
	retryCard := frameBody(result)["template_card"].(map[string]any)
	if frameBody(result)["chatid"] != "zhang.san" || !strings.Contains(fmt.Sprint(retryCard), "结果未知") || !strings.Contains(fmt.Sprint(retryCard["button_list"]), "重新解绑") {
		t.Fatalf("unknown outcome %v", result)
	}
	server.ack(result, 0)
	waitNotified(jobID)
	server.push(wecomClick("e3", "zhang.san", retryCard["task_id"].(string), "retry"))
	if update := frameBody(server.next(wecomUpdateCard)); !strings.Contains(fmt.Sprint(update), "已提交") {
		t.Fatalf("release again %v", update)
	}
	var retries int
	if err = db.QueryRow(`SELECT count(*) FROM bp_release_jobs WHERE retry_of_id=$1 AND source='wecom' AND actor_id=$2`, jobID, operatorID).Scan(&retries); err != nil || retries != 1 {
		t.Fatalf("released again %d times: %v", retries, err)
	}

	// Turning it off disconnects; WeCom refusing the secret is a failure with its reason.
	call("PATCH", "/api/ops/wecom/settings", fmt.Sprintf(`{"version":%d,"is_enabled":false}`, version()), 200)
	waitState("disabled")
	call("PATCH", "/api/ops/wecom/settings", fmt.Sprintf(`{"version":%d,"is_enabled":true,"secret":"wrong"}`, version()), 200)
	server.ack(server.next(wecomSubscribe), 40001)
	if s := waitState("failed"); !strings.Contains(fmt.Sprint(s["last_error"]), "refused the bot ID or secret") {
		t.Fatalf("refused %v", s)
	}
	cancel()
}

// WeCom's server matches handshake header names case-sensitively, so the bot must spell them as
// the WebSocket specification does on the wire, not as Go canonicalizes them.
func TestWeComHandshakeSpellsHeadersExactly(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	raw := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 4096)
		n, _ := conn.Read(buf)
		raw <- string(buf[:n])
		_, _ = conn.Write([]byte("HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\n\r\n"))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, _ = websocket.Dial(ctx, "ws://"+ln.Addr().String(), wecomDial)
	request := <-raw
	for _, name := range []string{"Sec-WebSocket-Key: ", "Sec-WebSocket-Version: 13"} {
		if !strings.Contains(request, "\r\n"+name) {
			t.Fatalf("handshake lacks %q:\n%s", name, request)
		}
	}
	if strings.Contains(request, "Sec-Websocket-") {
		t.Fatalf("handshake has a canonicalized name:\n%s", request)
	}
}
