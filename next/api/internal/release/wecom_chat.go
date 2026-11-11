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
	"regexp"
	"strings"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

// Members talk to the bot in Chinese: an address asks for its release card, "查询" with an address
// only looks it up, and anything else gets the help text.
var (
	wecomMention = regexp.MustCompile(`@\S+`)
	wecomAddress = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}\b`)
)

// wecomReasons are the release reasons in the console's order, one button each (WeCom allows six).
var wecomReasons = []struct{ code, label string }{
	{"temporary_test", "临时测试"}, {"user_report", "用户反馈"}, {"ip_mac_change", "IP / MAC 变更"},
	{"wrong_binding_fix", "修正错误绑定"}, {"security_response", "安全处置"}, {"other", "其他"},
}

func wecomReasonLabel(code string) string {
	for _, reason := range wecomReasons {
		if reason.code == code {
			return reason.label
		}
	}
	return code
}

// wecomStatus names a release outcome as the console does, with what it means.
var wecomStatus = map[string][2]string{
	"queued":                    {"排队中", ""},
	"running":                   {"进行中", ""},
	"succeeded":                 {"已解绑", "静态绑定已删除。"},
	"no_static_binding":         {"无绑定", "只有动态记录，没有绑定，未发送任何命令。"},
	"no_record":                 {"查无此 IP", "交换机上查不到这个 IP 的记录，未发送任何命令。请检查地址，以及它所在网段的交换机和命令模板是否正确。"},
	"failed":                    {"失败", ""},
	"timeout":                   {"超时", "解绑任务超时。"},
	"needs_manual_confirmation": {"结果未知", "解绑命令可能已经执行，但无法确定绑定是否已删除。重新解绑会先查询交换机。"},
}

// wecomFailures words the failures a release most often ends with.
var wecomFailures = map[string]string{
	"Switch authentication failed":                    "设备认证失败",
	"Switch connection timed out":                     "连接设备超时",
	"Switch connection failed":                        "连接设备失败",
	"Switch command failed":                           "设备命令执行失败",
	"Switch command timed out":                        "设备命令执行超时",
	"Release command returned an error pattern":       "解绑命令的输出匹配到错误规则",
	"Pre-query could not be parsed; nothing was sent": "无法解析解绑前的查询输出，没有执行解绑",
	"Release job timed out":                           "解绑任务超时",
	"Switch configuration changed after the release was queued; release again": "提交解绑后交换机配置有变更，请重新解绑",
}

const wecomHelp = "发送要解绑的 IPv4 地址，例如 `解绑 10.20.30.40`，再选择原因，就会开始解绑。\n" +
	"只想看看它归哪台交换机，发送 `查询 10.20.30.40`。\n" +
	"一次只解绑一个地址；解绑前会先查询交换机，不是静态绑定就不会发送任何命令。"

func wecomTaskID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "bp_" + hex.EncodeToString(b)
}

// replyText answers a message in its chat; replies go out as a finished stream, which takes Markdown.
func (b *WeComBot) replyText(ctx context.Context, reqID, text string) {
	body := map[string]any{"msgtype": "stream", "stream": map[string]any{"id": wecomReqID("stream"), "finish": true, "content": text}}
	if err := b.send(ctx, wecomReply, reqID, body); err != nil && ctx.Err() == nil {
		log.Printf("wecom: reply: %v", err)
	}
}

func (b *WeComBot) replyCard(ctx context.Context, reqID string, card map[string]any) {
	if err := b.send(ctx, wecomReply, reqID, map[string]any{"msgtype": "template_card", "template_card": card}); err != nil && ctx.Err() == nil {
		log.Printf("wecom: reply card: %v", err)
	}
}

// consoleLink is the release console, on the job when there is one; WeCom requires a text notice
// card to lead somewhere when tapped.
func (b *WeComBot) consoleLink(jobID string) string {
	link := strings.TrimRight(b.publicURL, "/") + "/release"
	if jobID != "" {
		link += "?job=" + jobID
	}
	return link
}

// updateCard replaces a clicked card with a notice, for everyone or (userids) only for the members
// named; tapping it opens the release console, on jobID when given. WeCom takes it only within
// five seconds of the click.
func (b *WeComBot) updateCard(ctx context.Context, reqID, taskID, jobID, title, desc, note string, userids ...string) {
	card := map[string]any{"card_type": "text_notice", "main_title": map[string]string{"title": title, "desc": desc}, "task_id": taskID,
		"card_action": map[string]any{"type": 1, "url": b.consoleLink(jobID)}}
	if note != "" {
		card["sub_title_text"] = note
	}
	body := map[string]any{"response_type": "update_template_card", "template_card": card}
	if len(userids) > 0 {
		body["userids"] = userids
	}
	if err := b.send(ctx, wecomUpdateCard, reqID, body); err != nil && ctx.Err() == nil {
		log.Printf("wecom: update card: %v", err)
	}
}

// member is the Bind Plane user a WeCom member is bound to, if they may release addresses. A
// refusal comes back as the text to answer with.
func (b *WeComBot) member(ctx context.Context, wecomUser string) (Actor, bool, string) {
	var userID string
	err := b.db.QueryRowContext(ctx, `SELECT user_id::text FROM bp_wecom_accounts WHERE wecom_userid=$1`, wecomUser).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return Actor{}, false, fmt.Sprintf("你的企业微信账号（%s）还没有绑定 Bind Plane 用户，请联系管理员在「企业微信」页面绑定。", wecomUser)
	}
	if err != nil {
		return Actor{}, false, "暂时无法确认你的身份，请稍后再试。"
	}
	p, err := b.principals.FindPrincipalByID(ctx, userID)
	if err != nil {
		return Actor{}, false, "你绑定的 Bind Plane 账号已停用或不存在，请联系管理员。"
	}
	admin, operator := releaseRoles(p)
	if !operator {
		return Actor{}, false, "你的 Bind Plane 账号没有解绑权限（需要操作员角色）。"
	}
	return Actor{ID: p.User.ID, Name: p.User.Name, Email: p.User.Email, Via: "wecom"}, admin, ""
}

// releaseRoles reads whether a user is an admin and whether they may release addresses.
func releaseRoles(p domain.Principal) (admin, operator bool) {
	admin = p.SuperAdmin
	operator = admin
	for _, role := range p.Roles {
		switch role.Name {
		case "Admin":
			admin, operator = true, true
		case "Operator":
			operator = true
		}
	}
	return admin, operator
}

func (b *WeComBot) onMessage(ctx context.Context, frame wecomFrame) {
	var cb wecomCallback
	if json.Unmarshal(frame.Body, &cb) != nil {
		return
	}
	reqID := frame.Headers.ReqID
	if cb.MsgType != "text" {
		b.replyText(ctx, reqID, "目前只认文字消息。\n\n"+wecomHelp)
		return
	}
	if _, _, refusal := b.member(ctx, cb.From.UserID); refusal != "" {
		b.replyText(ctx, reqID, refusal)
		return
	}
	text := strings.TrimSpace(wecomMention.ReplaceAllString(cb.Text.Content, " "))
	addresses := map[string]bool{}
	var ip string
	for _, found := range wecomAddress.FindAllString(text, -1) {
		if _, err := ResolveNetwork(found, nil); err != ErrInvalidIP {
			addresses[found], ip = true, found
		}
	}
	switch {
	case len(addresses) > 1:
		b.replyText(ctx, reqID, "一次只能解绑一个地址，请分开发送。")
	case ip == "":
		b.replyText(ctx, reqID, wecomHelp)
	case strings.Contains(text, "查询") || strings.HasPrefix(strings.ToLower(text), "query"):
		b.replyText(ctx, reqID, b.lookUp(ctx, ip))
	default:
		b.offerRelease(ctx, reqID, cb, ip)
	}
}

// resolved is the switch a release of ip would go to, or why there is none, as text to answer with.
func (b *WeComBot) resolved(ctx context.Context, ip string) (name, managementIP, cidr, stop string) {
	networks, err := activeNetworks(ctx, b.db)
	if err != nil {
		return "", "", "", "暂时无法查找交换机，请稍后再试。"
	}
	selected, err := ResolveNetwork(ip, networks)
	switch {
	case errors.Is(err, ErrNoSwitch):
		return "", "", "", fmt.Sprintf("没有交换机负责 %s：它不在任何负责解绑的交换机读出的网段里。请确认地址无误，或请管理员把它的网关交换机设为「负责解绑」。", ip)
	case errors.Is(err, ErrAmbiguous):
		return "", "", "", fmt.Sprintf("有多台交换机同样负责 %s，无法确定由哪台解绑，请管理员检查这些交换机的配置。", ip)
	case err != nil:
		return "", "", "", "暂时无法查找交换机，请稍后再试。"
	}
	if err = b.db.QueryRowContext(ctx, `SELECT name,host(management_ip) FROM bp_switches WHERE id=$1`, selected.SwitchID).Scan(&name, &managementIP); err != nil {
		return "", "", "", "暂时无法查找交换机，请稍后再试。"
	}
	return name, managementIP, selected.CIDR, ""
}

// lastRelease describes the address's newest release, or "" when it has none.
func (b *WeComBot) lastRelease(ctx context.Context, ip string) string {
	var status, actor string
	var at time.Time
	err := b.db.QueryRowContext(ctx, `SELECT status,COALESCE(NULLIF(actor_name,''),actor_email),COALESCE(finished_at,created_at) FROM bp_release_jobs
 WHERE target_ip=$1::inet AND kind='release' ORDER BY created_at DESC LIMIT 1`, ip).Scan(&status, &actor, &at)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%s · %s · %s", wecomStatus[status][0], at.In(b.loc).Format("01-02 15:04"), actor)
}

func (b *WeComBot) lookUp(ctx context.Context, ip string) string {
	name, managementIP, cidr, stop := b.resolved(ctx, ip)
	if stop != "" {
		return stop
	}
	text := fmt.Sprintf("**%s**\n负责解绑：%s（%s）\n网段：%s", ip, name, managementIP, cidr)
	if last := b.lastRelease(ctx, ip); last != "" {
		text += "\n最近一次解绑：" + last
	}
	return text
}

// offerRelease answers an address with its release card: the switch it goes to and one button per
// reason. Choosing a reason starts the release, as choosing Release does in the console.
func (b *WeComBot) offerRelease(ctx context.Context, reqID string, cb wecomCallback, ip string) {
	name, managementIP, cidr, stop := b.resolved(ctx, ip)
	if stop != "" {
		b.replyText(ctx, reqID, stop)
		return
	}
	var busy string
	err := b.db.QueryRowContext(ctx, `SELECT status FROM bp_release_jobs WHERE target_ip=$1::inet AND kind='release' AND status IN ('queued','running') LIMIT 1`, ip).Scan(&busy)
	if err == nil {
		b.replyText(ctx, reqID, fmt.Sprintf("%s 已有一次解绑%s，完成后再试。", ip, wecomStatus[busy][0]))
		return
	}
	taskID := wecomTaskID()
	chatType, chatID := cb.chat()
	if _, err = b.db.ExecContext(ctx, `INSERT INTO bp_wecom_cards(task_id,kind,target_ip,wecom_userid,chat_type,chat_id) VALUES ($1,'release',$2::inet,$3,$4,$5)`,
		taskID, ip, cb.From.UserID, chatType, chatID); err != nil {
		b.replyText(ctx, reqID, "暂时无法发起解绑，请稍后再试。")
		return
	}
	facts := []map[string]string{{"keyname": "网段", "value": cidr}}
	if last := b.lastRelease(ctx, ip); last != "" {
		facts = append(facts, map[string]string{"keyname": "上次解绑", "value": last})
	}
	buttons := make([]map[string]any, 0, len(wecomReasons))
	for _, reason := range wecomReasons {
		buttons = append(buttons, map[string]any{"text": reason.label, "key": reason.code})
	}
	b.replyCard(ctx, reqID, map[string]any{
		"card_type":               "button_interaction",
		"main_title":              map[string]string{"title": "解绑 " + ip, "desc": fmt.Sprintf("交换机 %s（%s）", name, managementIP)},
		"sub_title_text":          "选择原因即开始解绑。解绑前会先查询，不是静态绑定就不会发送任何命令。",
		"horizontal_content_list": facts,
		"button_list":             buttons,
		"task_id":                 taskID,
	})
}

func (b *WeComBot) onEvent(ctx context.Context, frame wecomFrame, cb wecomCallback) {
	reqID := frame.Headers.ReqID
	switch cb.Event.EventType {
	case "enter_chat":
		if err := b.send(ctx, wecomWelcome, reqID, map[string]any{"msgtype": "text", "text": map[string]string{"content": wecomHelp}}); err != nil && ctx.Err() == nil {
			log.Printf("wecom: welcome: %v", err)
		}
	case "template_card_event":
		b.onClick(ctx, reqID, cb)
	}
}

// onClick starts the release a card's button asks for: the reason on a release card, or releasing
// again on a result card. Only the member the card was for may choose, once, while it is fresh.
func (b *WeComBot) onClick(ctx context.Context, reqID string, cb wecomCallback) {
	taskID, key := cb.cardClick()
	who := cb.From.UserID
	var kind, ip, owner, chatType, chatID string
	var original sql.NullString
	var used bool
	var created time.Time
	err := b.db.QueryRowContext(ctx, `SELECT kind,host(target_ip),job_id::text,wecom_userid,chat_type,chat_id,used_at IS NOT NULL,created_at FROM bp_wecom_cards WHERE task_id=$1`, taskID).
		Scan(&kind, &ip, &original, &owner, &chatType, &chatID, &used, &created)
	switch {
	case err != nil:
		b.updateCard(ctx, reqID, taskID, "", "这张卡片已失效", "", "请重新发送要解绑的地址。")
		return
	case who != owner:
		b.updateCard(ctx, reqID, taskID, "", "解绑 "+ip, "只有发起人可以选择", "需要解绑请自己发送这个地址。", who)
		return
	case used:
		return // a second click on a card already answered changes nothing
	case time.Since(created) > b.CardTTL:
		b.updateCard(ctx, reqID, taskID, "", "解绑 "+ip+" · 已过期", "", "请重新发送要解绑的地址。")
		return
	}
	actor, admin, refusal := b.member(ctx, who)
	if refusal != "" {
		b.updateCard(ctx, reqID, taskID, "", "解绑 "+ip, "未开始", refusal)
		return
	}
	reason := key
	if kind == "release" && !ValidReason(reason) {
		return
	}
	if result, err := b.db.ExecContext(ctx, `UPDATE bp_wecom_cards SET used_at=now() WHERE task_id=$1 AND used_at IS NULL`, taskID); err != nil {
		return
	} else if n, _ := result.RowsAffected(); n != 1 {
		return
	}
	var job Job
	if kind == "retry" {
		job, err = b.store.Retry(ctx, original.String, actor, admin)
	} else {
		job, err = b.store.Start(ctx, StartInput{TargetIP: ip, Reason: reason, Actor: actor})
	}
	title := "解绑 " + ip
	switch {
	case errors.Is(err, ErrConflict):
		b.updateCard(ctx, reqID, taskID, "", title, "未开始", "这个地址已有解绑在进行，完成后再试。")
		return
	case errors.Is(err, ErrNotFound):
		b.updateCard(ctx, reqID, taskID, "", title, "未开始", "只有原来的发起人或管理员可以重新解绑。")
		return
	case errors.Is(err, ErrAssetChanged):
		b.updateCard(ctx, reqID, taskID, "", title, "未开始", "交换机配置刚刚有变化，请重新发送地址。")
		return
	case errors.Is(err, ErrNoSwitch), errors.Is(err, ErrAmbiguous):
		_, _, _, stop := b.resolved(ctx, ip)
		b.updateCard(ctx, reqID, taskID, "", title, "未开始", stop)
		return
	case err != nil:
		log.Printf("wecom: start release: %v", err)
		b.updateCard(ctx, reqID, taskID, "", title, "未开始", "暂时无法提交，请稍后再试。")
		return
	case job.JobID == "":
		_, _, _, stop := b.resolved(ctx, ip)
		b.updateCard(ctx, reqID, taskID, "", title, "未开始", stop)
		return
	}
	if _, err = b.db.ExecContext(ctx, `INSERT INTO bp_wecom_jobs(job_id,wecom_userid,chat_type,chat_id) VALUES ($1,$2,$3,$4)`, job.JobID, who, chatType, chatID); err != nil {
		log.Printf("wecom: remember chat for job %s: %v", job.JobID, err)
	}
	b.updateCard(ctx, reqID, taskID, job.JobID, title+" · 已提交", fmt.Sprintf("交换机 %s（%s）", job.SwitchName, job.SwitchManagementIP),
		fmt.Sprintf("原因：%s · 发起人：%s。完成后会在这里通知结果。", wecomReasonLabel(job.Reason), actor.Name))
}

// notifyResults tells each chat how the releases started from it ended. A result WeCom has not
// acknowledged is sent again later, at most five times.
func (b *WeComBot) notifyResults(ctx context.Context) {
	b.pendingMu.Lock()
	waiting := map[string]bool{}
	for reqID, sent := range b.pending {
		if time.Since(sent.sent) > 30*time.Second {
			delete(b.pending, reqID)
			continue
		}
		waiting[sent.jobID] = true
	}
	b.pendingMu.Unlock()
	rows, err := b.db.QueryContext(ctx, `SELECT w.job_id::text,w.wecom_userid,w.chat_type,w.chat_id,j.status,host(j.target_ip),COALESCE(j.switch_name,''),COALESCE(host(j.switch_management_ip),''),
 j.reason,COALESCE(NULLIF(j.actor_name,''),j.actor_email),COALESCE(j.error_message,'')
 FROM bp_wecom_jobs w JOIN bp_release_jobs j ON j.id=w.job_id
 WHERE w.notified_at IS NULL AND w.attempts<5 AND j.status NOT IN ('queued','running') ORDER BY j.finished_at LIMIT 20`)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("wecom: pending results: %v", err)
		}
		return
	}
	type result struct{ job, owner, chatType, chat, status, ip, name, management, reason, actor, failure string }
	var results []result
	for rows.Next() {
		var r result
		if err = rows.Scan(&r.job, &r.owner, &r.chatType, &r.chat, &r.status, &r.ip, &r.name, &r.management, &r.reason, &r.actor, &r.failure); err != nil {
			break
		}
		if !waiting[r.job] {
			results = append(results, r)
		}
	}
	rows.Close()
	for _, r := range results {
		words := wecomStatus[r.status]
		meaning := words[1]
		if r.status == "failed" {
			meaning = "解绑失败"
			if r.failure != "" {
				reason := r.failure
				if zh, ok := wecomFailures[r.failure]; ok {
					reason = zh
				}
				meaning += "：" + reason
			}
			meaning += "。"
		}
		switchText := fmt.Sprintf("%s（%s）", r.name, r.management)
		var body map[string]any
		if r.status == "failed" || r.status == "timeout" || r.status == "needs_manual_confirmation" {
			taskID := wecomTaskID()
			if _, err := b.db.ExecContext(ctx, `INSERT INTO bp_wecom_cards(task_id,kind,target_ip,job_id,wecom_userid,chat_type,chat_id) VALUES ($1,'retry',$2::inet,$3,$4,$5,$6)`,
				taskID, r.ip, r.job, r.owner, r.chatType, r.chat); err != nil {
				continue
			}
			body = map[string]any{"msgtype": "template_card", "template_card": map[string]any{
				"card_type":      "button_interaction",
				"main_title":     map[string]string{"title": fmt.Sprintf("%s %s", r.ip, words[0]), "desc": "交换机 " + switchText},
				"sub_title_text": fmt.Sprintf("%s原因：%s · 发起人：%s", meaning, wecomReasonLabel(r.reason), r.actor),
				"button_list":    []map[string]any{{"text": "重新解绑", "key": "retry"}},
				"task_id":        taskID,
			}}
		} else {
			body = map[string]any{"msgtype": "markdown", "markdown": map[string]string{"content": fmt.Sprintf("**%s %s**\n交换机：%s\n%s\n原因：%s · 发起人：%s",
				r.ip, words[0], switchText, meaning, wecomReasonLabel(r.reason), r.actor)}}
		}
		body["chatid"] = r.chat
		reqID := wecomReqID(wecomSend)
		b.pendingMu.Lock()
		b.pending[reqID] = wecomPending{jobID: r.job, sent: time.Now()}
		b.pendingMu.Unlock()
		if _, err := b.db.ExecContext(ctx, `UPDATE bp_wecom_jobs SET attempts=attempts+1 WHERE job_id=$1`, r.job); err != nil {
			continue
		}
		if err := b.send(ctx, wecomSend, reqID, body); err != nil {
			return
		}
	}
}
