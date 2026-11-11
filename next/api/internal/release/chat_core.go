package release

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

// What the WeCom and Feishu bots share: the commands members give, the cards they answer with,
// the rules a click follows and the results sent back. Each bot only carries messages in its
// platform's protocol and draws the cards in its format.

const (
	platformWeCom  = "wecom"
	platformFeishu = "feishu"
)

// chatPlatformNames name each platform to its members.
var chatPlatformNames = map[string]string{platformWeCom: "企业微信", platformFeishu: "飞书"}

// chatVerbs are the commands, in Chinese and English. A verb comes first, then one address and,
// to release at once, a reason; nothing else does anything, so chat that mentions an address
// never brings up a card.
var chatVerbs = map[string]string{
	"查询": "lookup", "query": "lookup", "解绑": "release", "release": "release",
	"帮助": "help", "help": "help", "?": "help", "？": "help",
}

// chatReasons are the release reasons in the console's order, one button each (WeCom allows six).
var chatReasons = []struct{ code, label string }{
	{"temporary_test", "临时测试"}, {"user_report", "用户反馈"}, {"ip_mac_change", "IP / MAC 变更"},
	{"wrong_binding_fix", "修正错误绑定"}, {"security_response", "安全处置"}, {"other", "其他"},
}

func chatReasonLabel(code string) string {
	for _, reason := range chatReasons {
		if reason.code == code {
			return reason.label
		}
	}
	return code
}

// chatStatus names a release outcome as the console does, with what it means.
var chatStatus = map[string][2]string{
	"queued":                    {"排队中", ""},
	"running":                   {"进行中", ""},
	"succeeded":                 {"已解绑", "静态绑定已删除。"},
	"no_static_binding":         {"无绑定", "只有动态记录，没有绑定，未发送任何命令。"},
	"no_record":                 {"查无此 IP", "交换机上查不到这个 IP 的记录，未发送任何命令。请检查地址，以及它所在网段的交换机和命令模板是否正确。"},
	"failed":                    {"失败", ""},
	"timeout":                   {"超时", "解绑任务超时。"},
	"needs_manual_confirmation": {"结果未知", "解绑命令可能已经执行，但无法确定绑定是否已删除。重新解绑会先查询交换机。"},
}

// chatFailures words the failures a release most often ends with.
var chatFailures = map[string]string{
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

const chatHelp = "**可用的命令**（群里先 @ 我）\n" +
	"`查询 10.20.30.40`：查这个地址的 MAC 和绑定类型，只读\n" +
	"`解绑 10.20.30.40`：回一张卡片，选择原因后开始解绑\n" +
	"`解绑 10.20.30.40 临时测试`：带上原因，直接开始解绑\n" +
	"`帮助`：显示这段说明\n\n" +
	"原因只能是：临时测试、用户反馈、IP/MAC 变更、修正错误绑定、安全处置、其他。\n" +
	"一次只处理一个地址；解绑前会先查询交换机，不是静态绑定就不会发送任何命令。"

// chatOffer is the key of the button on a lookup's result that brings up the reasons.
const chatOffer = "offer"

const releaseNote = "选择原因即开始解绑。解绑前会先查询，不是静态绑定就不会发送任何命令。"

func chatTaskID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "bp_" + hex.EncodeToString(b)
}

// chatCommand is a message read as a command; problem says why it is not one.
type chatCommand struct{ verb, ip, reason, problem string }

// parseChatCommand reads a message strictly: the verb first, then one IPv4 address and, for a
// release, an optional reason from the list. In a group the message starts with a mention of the
// bot, whose name may hold spaces, so the command is the first verb after it.
func parseChatCommand(text string) chatCommand {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return chatCommand{verb: "help"}
	}
	// Chinese is often typed without a space after the verb: 解绑10.20.30.40.
	split := func(field string) []string {
		for _, verb := range []string{"查询", "解绑"} {
			if after, ok := strings.CutPrefix(field, verb); ok && after != "" {
				return []string{verb, after}
			}
		}
		return []string{field}
	}
	start := 0
	if strings.HasPrefix(fields[0], "@") {
		start = len(fields)
		for i, field := range fields {
			if _, ok := chatVerbs[strings.ToLower(split(field)[0])]; ok {
				start = i
				break
			}
		}
		// The bot's name cannot be told from what follows it, so a mention with no address is
		// answered with the help alone.
		if start == len(fields) && !strings.ContainsAny(text, "0123456789") {
			return chatCommand{verb: "help"}
		}
		if start == len(fields) {
			return chatCommand{problem: "没看懂这条消息。"}
		}
	}
	fields = append(split(fields[start]), fields[start+1:]...)
	verb, ok := chatVerbs[strings.ToLower(fields[0])]
	if !ok {
		return chatCommand{problem: "没看懂这条消息。"}
	}
	if verb == "help" {
		return chatCommand{verb: verb}
	}
	args := fields[1:]
	if len(args) == 0 {
		return chatCommand{problem: "缺少要" + map[string]string{"lookup": "查询", "release": "解绑"}[verb] + "的 IPv4 地址。"}
	}
	if _, err := ResolveNetwork(args[0], nil); err == ErrInvalidIP {
		return chatCommand{problem: fmt.Sprintf("「%s」不是一个 IPv4 地址，一次只能写一个地址。", args[0])}
	}
	command := chatCommand{verb: verb, ip: args[0]}
	rest := strings.Join(args[1:], "")
	switch {
	case rest == "":
	case verb == "lookup":
		return chatCommand{problem: "查询只需要一个地址，后面不用再写别的。"}
	default:
		if command.reason = chatReasonCode(rest); command.reason == "" {
			return chatCommand{problem: fmt.Sprintf("「%s」不是可用的原因。", strings.Join(args[1:], " "))}
		}
	}
	return command
}

// chatReasonCode is the reason a member typed, by its name (spaces and case ignored) or code.
func chatReasonCode(typed string) string {
	plain := func(text string) string { return strings.ToLower(strings.Join(strings.Fields(text), "")) }
	for _, reason := range chatReasons {
		if plain(typed) == plain(reason.label) || plain(typed) == reason.code {
			return reason.code
		}
	}
	return ""
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

// chatPlace is where a reply or a later result goes: a single chat or a group, by the ID the
// platform sends to.
type chatPlace struct{ kind, id string }

// chatCard is a card in the bots' shared terms; each bot draws it in its platform's format. A
// card without buttons is a notice, which leads to its job in the release console.
type chatCard struct {
	taskID      string
	title, desc string // desc names the switch
	note        string
	facts       [][2]string
	buttons     []chatButton
	jobID       string
	tone        string // info, success, warning or danger; Feishu colours the card's header by it
}

type chatButton struct{ text, key string }

// chatMessage is a reply or result: Markdown text, or a card.
type chatMessage struct {
	text string
	card *chatCard
}

// chatIncoming is a member's message to the bot.
type chatIncoming struct {
	text     string
	isText   bool
	sender   string   // the member, as the platform names them in a chat: owns cards and results
	accounts []string // the member's IDs a binding may name, the one to show first
	place    chatPlace
}

// chatClick is a member choosing a button on a card.
type chatClick struct {
	taskID, key string
	sender      string
	accounts    []string
}

// clickOutcome answers a click: card replaces the clicked card. A private one is meant only for
// the member who clicked; WeCom shows it to them alone, Feishu shows the toast instead.
type clickOutcome struct {
	card    *chatCard
	private bool
	toast   string
}

// chatResult is a finished job's result, ready to send to the chat it was started from.
type chatResult struct {
	jobID   string
	place   chatPlace
	message chatMessage
}

// botSettings are a bot's settings as admins left them; sealed is the encrypted secret.
type botSettings struct {
	enabled                bool
	appID, sealed, address string
	version                int
}

var errBotSettingsChanged = errors.New("settings changed")

// chatCore is what both bots stand on: the release store the console uses, and the shared rules.
type chatCore struct {
	db         *sql.DB
	store      Store
	principals PrincipalFinder
	loc        *time.Location
	publicURL  string // where notices lead: the release console

	// How often the settings are checked and results sent; tests shorten them.
	SettingsEvery, NotifyEvery time.Duration
	// How long a card's buttons stay usable.
	CardTTL time.Duration
}

func newChatCore(db *sql.DB, store Store, principals PrincipalFinder, loc *time.Location, publicURL string) chatCore {
	return chatCore{db: db, store: store, principals: principals, loc: loc, publicURL: publicURL,
		SettingsEvery: 5 * time.Second, NotifyEvery: 3 * time.Second, CardTTL: 30 * time.Minute}
}

// consoleLink is the release console, on the job when there is one.
func (c *chatCore) consoleLink(jobID string) string {
	link := strings.TrimRight(c.publicURL, "/") + "/release"
	if jobID != "" {
		link += "?job=" + jobID
	}
	return link
}

func (c *chatCore) loadBot(ctx context.Context, platform string) (botSettings, error) {
	var s botSettings
	var sealed sql.NullString
	err := c.db.QueryRowContext(ctx, `SELECT is_enabled,app_id,encrypted_bot_secret,address,version FROM bp_chat_bots WHERE platform=$1`, platform).
		Scan(&s.enabled, &s.appID, &sealed, &s.address, &s.version)
	s.sealed = sealed.String
	return s, err
}

// setState records how a bot's connection stands for its page; a failure keeps its reason.
func (c *chatCore) setState(ctx context.Context, platform, state, reason string) {
	if _, err := c.db.ExecContext(ctx, `UPDATE bp_chat_bots SET state=$2,state_at=now(),last_error=NULLIF($3,''),
 connected_at=CASE WHEN $2='connected' THEN now() ELSE connected_at END WHERE platform=$1 AND (state<>$2 OR last_error IS DISTINCT FROM NULLIF($3,''))`, platform, state, reason); err != nil && ctx.Err() == nil {
		log.Printf("%s: record state: %v", platform, err)
	}
}

// runBot keeps a bot connected while it is turned on: session serves one connection, and after
// it fails, pause says how long to wait (0 for growing pauses). New settings reconnect at once.
func (c *chatCore) runBot(ctx context.Context, platform string, session func(context.Context, botSettings) error, pause func(error) time.Duration) {
	backoff := time.Second
	for ctx.Err() == nil {
		s, err := c.loadBot(ctx, platform)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("%s: load settings: %v", platform, err)
			}
			c.waitChange(ctx, platform, -1, c.SettingsEvery)
			continue
		}
		if !s.enabled {
			c.setState(ctx, platform, "disabled", "")
			c.waitChange(ctx, platform, s.version, 0)
			continue
		}
		err = session(ctx, s)
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, errBotSettingsChanged) {
			backoff = time.Second
			continue
		}
		c.setState(ctx, platform, "failed", err.Error())
		if wait := pause(err); wait > 0 {
			c.waitChange(ctx, platform, s.version, wait)
		} else {
			c.waitChange(ctx, platform, s.version, backoff)
			backoff = min(backoff*2, time.Minute)
		}
	}
}

// waitChange returns once the platform's settings version differs from version, after limit (0
// waits for the change alone), or when ctx ends.
func (c *chatCore) waitChange(ctx context.Context, platform string, version int, limit time.Duration) {
	var deadline <-chan time.Time
	if limit > 0 {
		timer := time.NewTimer(limit)
		defer timer.Stop()
		deadline = timer.C
	}
	tick := time.NewTicker(c.SettingsEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline:
			return
		case <-tick.C:
			if s, err := c.loadBot(ctx, platform); err == nil && s.version != version {
				return
			}
		}
	}
}

// settingsChanged says whether admins changed the settings a session runs with.
func (c *chatCore) settingsChanged(ctx context.Context, platform string, version int) bool {
	s, err := c.loadBot(ctx, platform)
	return err == nil && s.version != version
}

// member is the Bind Plane user a platform's member is bound to, by any of their IDs, if they
// may release addresses. A refusal comes back as the text to answer with.
func (c *chatCore) member(ctx context.Context, platform string, accounts []string) (Actor, bool, string) {
	name := chatPlatformNames[platform]
	if len(accounts) == 0 || accounts[0] == "" {
		return Actor{}, false, "无法确认你的身份，请稍后再试。"
	}
	var userID string
	err := c.db.QueryRowContext(ctx, `SELECT user_id::text FROM bp_chat_accounts WHERE platform=$1 AND external_id IN ($2,$3) ORDER BY external_id=$2 DESC LIMIT 1`,
		platform, accounts[0], accounts[len(accounts)-1]).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return Actor{}, false, fmt.Sprintf("你的%s账号（%s）还没有绑定 Bind Plane 用户，请联系管理员在「%s」页面绑定。", name, accounts[0], name)
	}
	if err != nil {
		return Actor{}, false, "暂时无法确认你的身份，请稍后再试。"
	}
	p, err := c.principals.FindPrincipalByID(ctx, userID)
	if err != nil {
		return Actor{}, false, "你绑定的 Bind Plane 账号已停用或不存在，请联系管理员。"
	}
	admin, operator := releaseRoles(p)
	if !operator {
		return Actor{}, false, "你的 Bind Plane 账号没有解绑权限（需要操作员角色）。"
	}
	return Actor{ID: p.User.ID, Name: p.User.Name, Email: p.User.Email, Via: platform}, admin, ""
}

// handleMessage answers a member's message: a command does what it says, anything else gets the
// commands back.
func (c *chatCore) handleMessage(ctx context.Context, platform string, in chatIncoming) chatMessage {
	if !in.isText {
		return chatMessage{text: "目前只认文字消息。\n\n" + chatHelp}
	}
	command := parseChatCommand(in.text)
	if command.problem != "" {
		return chatMessage{text: command.problem + "\n\n" + chatHelp}
	}
	if command.verb == "help" {
		return chatMessage{text: chatHelp}
	}
	actor, _, refusal := c.member(ctx, platform, in.accounts)
	if refusal != "" {
		return chatMessage{text: refusal}
	}
	switch {
	case command.verb == "lookup":
		return c.startLookup(ctx, platform, in, command.ip, actor)
	case command.reason == "":
		return c.offerRelease(ctx, platform, in, command.ip)
	default:
		return c.releaseNow(ctx, platform, in, command.ip, command.reason, actor)
	}
}

// releaseNow starts a release a command gave a reason for, as choosing Release does in the console.
func (c *chatCore) releaseNow(ctx context.Context, platform string, in chatIncoming, ip, reason string, actor Actor) chatMessage {
	job, err := c.store.Start(ctx, StartInput{TargetIP: ip, Reason: reason, Actor: actor})
	if problem := c.notStarted(ctx, ip, job, err); problem != "" {
		return chatMessage{text: fmt.Sprintf("**解绑 %s 未开始**\n%s", ip, problem)}
	}
	c.rememberChat(ctx, platform, job.JobID, in.sender, in.place)
	return chatMessage{text: fmt.Sprintf("**已提交解绑 %s**\n交换机 %s（%s） · 原因：%s\n完成后会在这里通知结果。", ip, job.SwitchName, job.SwitchManagementIP, chatReasonLabel(reason))}
}

// notStarted says why a release did not start, or "" when it did.
func (c *chatCore) notStarted(ctx context.Context, ip string, job Job, err error) string {
	switch {
	case errors.Is(err, ErrConflict):
		return "这个地址已有解绑在进行，完成后再试。"
	case errors.Is(err, ErrNotFound):
		return "只有原来的发起人或管理员可以重新解绑。"
	case errors.Is(err, ErrAssetChanged):
		return "交换机配置刚刚有变化，请重新发送命令。"
	case errors.Is(err, ErrNoSwitch), errors.Is(err, ErrAmbiguous), err == nil && job.JobID == "":
		_, _, _, stop := c.resolved(ctx, ip)
		return stop
	case err != nil:
		log.Printf("chat: start release: %v", err)
		return "暂时无法提交，请稍后再试。"
	}
	return ""
}

// rememberChat records where a started job's result goes.
func (c *chatCore) rememberChat(ctx context.Context, platform, jobID, owner string, place chatPlace) {
	if _, err := c.db.ExecContext(ctx, `INSERT INTO bp_chat_jobs(job_id,platform,owner_id,chat_type,chat_id) VALUES ($1,$2,$3,$4,$5)`,
		jobID, platform, owner, place.kind, place.id); err != nil {
		log.Printf("%s: remember chat for job %s: %v", platform, jobID, err)
	}
}

// newCard records a card with buttons: what a click on it does, for whom and in which chat.
func (c *chatCore) newCard(ctx context.Context, platform, kind, ip, jobID, owner string, place chatPlace) (string, error) {
	taskID := chatTaskID()
	_, err := c.db.ExecContext(ctx, `INSERT INTO bp_chat_cards(task_id,platform,kind,target_ip,job_id,owner_id,chat_type,chat_id) VALUES ($1,$2,$3,$4::inet,NULLIF($5,'')::uuid,$6,$7,$8)`,
		taskID, platform, kind, ip, jobID, owner, place.kind, place.id)
	return taskID, err
}

// resolved is the switch a release of ip would go to, or why there is none, as text to answer with.
func (c *chatCore) resolved(ctx context.Context, ip string) (name, managementIP, cidr, stop string) {
	networks, err := activeNetworks(ctx, c.db)
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
	if err = c.db.QueryRowContext(ctx, `SELECT name,host(management_ip) FROM bp_switches WHERE id=$1`, selected.SwitchID).Scan(&name, &managementIP); err != nil {
		return "", "", "", "暂时无法查找交换机，请稍后再试。"
	}
	return name, managementIP, selected.CIDR, ""
}

// lastRelease describes the address's newest release, or "" when it has none.
func (c *chatCore) lastRelease(ctx context.Context, ip string) string {
	var status, actor string
	var at time.Time
	err := c.db.QueryRowContext(ctx, `SELECT status,COALESCE(NULLIF(actor_name,''),actor_email),COALESCE(finished_at,created_at) FROM bp_release_jobs
 WHERE target_ip=$1::inet AND kind='release' ORDER BY created_at DESC LIMIT 1`, ip).Scan(&status, &actor, &at)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%s · %s · %s", chatStatus[status][0], at.In(c.loc).Format("01-02 15:04"), actor)
}

// startLookup asks the address's switch for its ARP entry through the console's own store; the
// MAC and entry type come back to the chat when the worker has read them.
func (c *chatCore) startLookup(ctx context.Context, platform string, in chatIncoming, ip string, actor Actor) chatMessage {
	job, err := c.store.Lookup(ctx, ip, actor)
	if err == nil && job.JobID == "" {
		_, _, _, stop := c.resolved(ctx, ip)
		return chatMessage{text: stop}
	}
	if err != nil {
		log.Printf("%s: start lookup: %v", platform, err)
		return chatMessage{text: "暂时无法查询，请稍后再试。"}
	}
	c.rememberChat(ctx, platform, job.JobID, in.sender, in.place)
	return chatMessage{text: fmt.Sprintf("正在向交换机 %s（%s）查询 %s，稍等几秒。", job.SwitchName, job.SwitchManagementIP, ip)}
}

// offerRelease answers an address with its release card: the switch it goes to and one button per
// reason. Choosing a reason starts the release, as choosing Release does in the console.
func (c *chatCore) offerRelease(ctx context.Context, platform string, in chatIncoming, ip string) chatMessage {
	name, managementIP, cidr, stop := c.resolved(ctx, ip)
	if stop != "" {
		return chatMessage{text: stop}
	}
	var busy string
	err := c.db.QueryRowContext(ctx, `SELECT status FROM bp_release_jobs WHERE target_ip=$1::inet AND kind='release' AND status IN ('queued','running') LIMIT 1`, ip).Scan(&busy)
	if err == nil {
		return chatMessage{text: fmt.Sprintf("%s 已有一次解绑%s，完成后再试。", ip, chatStatus[busy][0])}
	}
	taskID, err := c.newCard(ctx, platform, "release", ip, "", in.sender, in.place)
	if err != nil {
		return chatMessage{text: "暂时无法发起解绑，请稍后再试。"}
	}
	facts := [][2]string{{"网段", cidr}}
	if last := c.lastRelease(ctx, ip); last != "" {
		facts = append(facts, [2]string{"上次解绑", last})
	}
	return chatMessage{card: reasonCard(taskID, ip, fmt.Sprintf("%s（%s）", name, managementIP), releaseNote, facts)}
}

// reasonCard is the card that releases ip: its switch, and one button per reason.
func reasonCard(taskID, ip, switchText, note string, facts [][2]string) *chatCard {
	buttons := make([]chatButton, 0, len(chatReasons))
	for _, reason := range chatReasons {
		buttons = append(buttons, chatButton{text: reason.label, key: reason.code})
	}
	return &chatCard{taskID: taskID, title: "解绑 " + ip, desc: "交换机 " + switchText, note: note, facts: facts, buttons: buttons, tone: "info"}
}

// notice is a card without buttons that replaces a clicked one.
func notice(taskID, jobID, title, desc, note, tone string) *chatCard {
	return &chatCard{taskID: taskID, jobID: jobID, title: title, desc: desc, note: note, tone: tone}
}

// handleClick starts the release a card's button asks for: the reason on a release card, or
// releasing again on a result card. Only the member the card was for may choose, once, while it is
// fresh. nil means the click changes nothing.
func (c *chatCore) handleClick(ctx context.Context, platform string, click chatClick) *clickOutcome {
	taskID, who := click.taskID, click.sender
	var kind, ip, owner string
	var place chatPlace
	var original sql.NullString
	var used bool
	var created time.Time
	err := c.db.QueryRowContext(ctx, `SELECT kind,host(target_ip),job_id::text,owner_id,chat_type,chat_id,used_at IS NOT NULL,created_at FROM bp_chat_cards WHERE task_id=$1 AND platform=$2`, taskID, platform).
		Scan(&kind, &ip, &original, &owner, &place.kind, &place.id, &used, &created)
	switch {
	case err != nil:
		return &clickOutcome{card: notice(taskID, "", "这张卡片已失效", "", "请重新发送「解绑 地址」。", "")}
	case who != owner:
		text := fmt.Sprintf("需要解绑请自己发送「解绑 %s」。", ip)
		return &clickOutcome{card: notice(taskID, "", "解绑 "+ip, "只有发起人可以选择", text, ""), private: true, toast: "只有发起人可以选择。" + text}
	case used:
		return nil // a second click on a card already answered changes nothing
	case time.Since(created) > c.CardTTL:
		return &clickOutcome{card: notice(taskID, "", "解绑 "+ip+" · 已过期", "", fmt.Sprintf("请重新发送「解绑 %s」。", ip), "")}
	}
	actor, admin, refusal := c.member(ctx, platform, click.accounts)
	if refusal != "" {
		return &clickOutcome{card: notice(taskID, "", "解绑 "+ip, "未开始", refusal, "")}
	}
	reason := click.key
	if kind == "release" && reason == chatOffer {
		// A lookup's result asked for the reasons: the same card now offers them, still unused.
		name, managementIP, _, stop := c.resolved(ctx, ip)
		if stop != "" {
			return &clickOutcome{card: notice(taskID, "", "解绑 "+ip, "未开始", stop, "")}
		}
		return &clickOutcome{card: reasonCard(taskID, ip, fmt.Sprintf("%s（%s）", name, managementIP), releaseNote, nil)}
	}
	if kind == "release" && !ValidReason(reason) {
		return nil
	}
	if result, err := c.db.ExecContext(ctx, `UPDATE bp_chat_cards SET used_at=now() WHERE task_id=$1 AND used_at IS NULL`, taskID); err != nil {
		return nil
	} else if n, _ := result.RowsAffected(); n != 1 {
		return nil
	}
	var job Job
	if kind == "retry" {
		job, err = c.store.Retry(ctx, original.String, actor, admin)
	} else {
		job, err = c.store.Start(ctx, StartInput{TargetIP: ip, Reason: reason, Actor: actor})
	}
	title := "解绑 " + ip
	if problem := c.notStarted(ctx, ip, job, err); problem != "" {
		return &clickOutcome{card: notice(taskID, "", title, "未开始", problem, "warning")}
	}
	c.rememberChat(ctx, platform, job.JobID, who, place)
	return &clickOutcome{card: notice(taskID, job.JobID, title+" · 已提交", fmt.Sprintf("交换机 %s（%s）", job.SwitchName, job.SwitchManagementIP),
		fmt.Sprintf("原因：%s · 发起人：%s。完成后会在这里通知结果。", chatReasonLabel(job.Reason), actor.Name), "success")}
}

// lookupResult words what a lookup found. It only reports: a static binding comes as a card whose
// one button brings up the reasons, so releasing it stays a choice of its own; anything else is a
// message. ok is false when the card could not be recorded.
func (c *chatCore) lookupResult(ctx context.Context, platform, owner string, place chatPlace, status, ip, switchText, entry, mac, failure string) (chatMessage, bool) {
	if status == "succeeded" && entry == "static" {
		taskID, err := c.newCard(ctx, platform, "release", ip, "", owner, place)
		if err != nil {
			return chatMessage{}, false
		}
		return chatMessage{card: &chatCard{taskID: taskID, title: "查询结果 · " + ip, desc: "交换机 " + switchText,
			facts: [][2]string{{"MAC", mac}, {"类型", "静态绑定"}}, note: "只是查询，没有改动交换机。要解绑这个地址，点下面的按钮再选择原因。",
			buttons: []chatButton{{text: "解绑这个地址", key: chatOffer}}, tone: "info"}}, true
	}
	switch {
	case status == "succeeded" && entry == "dynamic":
		return chatMessage{text: fmt.Sprintf("**%s · 动态记录**\nMAC：%s\n交换机：%s\n只有动态记录，没有静态绑定，不需要解绑。", ip, mac, switchText)}, true
	case status == "succeeded":
		return chatMessage{text: fmt.Sprintf("**%s · 查无此 IP**\n交换机：%s\n交换机上查不到这个 IP 的记录。请检查地址，以及它所在网段的交换机和命令模板是否正确。", ip, switchText)}, true
	}
	reason := "暂时无法读取交换机"
	if status == "timeout" {
		reason = "查询超时"
	} else if failure == "Lookup output could not be parsed" {
		reason = "无法解析交换机的回复，不确定这个地址的记录"
	} else if zh, ok := chatFailures[failure]; ok {
		reason = zh
	}
	return chatMessage{text: fmt.Sprintf("**%s 查询失败**\n交换机：%s\n%s。可以稍后再查一次。", ip, switchText, reason)}, true
}

// pendingResults are the finished jobs started from the platform's chats whose results have not
// been delivered, except those in skip (sent and awaiting an acknowledgement). A result is tried
// at most five times.
func (c *chatCore) pendingResults(ctx context.Context, platform string, skip map[string]bool) []chatResult {
	rows, err := c.db.QueryContext(ctx, `SELECT w.job_id::text,w.owner_id,w.chat_type,w.chat_id,j.status,host(j.target_ip),COALESCE(j.switch_name,''),COALESCE(host(j.switch_management_ip),''),
 j.reason,COALESCE(NULLIF(j.actor_name,''),j.actor_email),
 COALESCE(j.error_message,(SELECT e.message FROM bp_release_events e WHERE e.job_id=j.id AND e.message IS NOT NULL ORDER BY e.id DESC LIMIT 1),''),j.kind,COALESCE(j.before_state->>'entry_type',''),COALESCE(j.before_state->>'mac','')
 FROM bp_chat_jobs w JOIN bp_release_jobs j ON j.id=w.job_id
 WHERE w.platform=$1 AND w.notified_at IS NULL AND w.attempts<5 AND j.status NOT IN ('queued','running') ORDER BY j.finished_at LIMIT 20`, platform)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("%s: pending results: %v", platform, err)
		}
		return nil
	}
	type row struct {
		job, owner                                                             string
		place                                                                  chatPlace
		status, ip, name, management, reason, actor, failure, kind, entry, mac string
	}
	var found []row
	for rows.Next() {
		var r row
		if err = rows.Scan(&r.job, &r.owner, &r.place.kind, &r.place.id, &r.status, &r.ip, &r.name, &r.management, &r.reason, &r.actor, &r.failure, &r.kind, &r.entry, &r.mac); err != nil {
			break
		}
		if !skip[r.job] {
			found = append(found, r)
		}
	}
	rows.Close()
	var results []chatResult
	for _, r := range found {
		words := chatStatus[r.status]
		meaning := words[1]
		if r.status == "failed" {
			meaning = "解绑失败"
			if r.failure != "" {
				reason := r.failure
				if zh, ok := chatFailures[r.failure]; ok {
					reason = zh
				}
				meaning += "：" + reason
			}
			meaning += "。"
		}
		switchText := fmt.Sprintf("%s（%s）", r.name, r.management)
		var message chatMessage
		switch {
		case r.kind == "lookup":
			var ok bool
			if message, ok = c.lookupResult(ctx, platform, r.owner, r.place, r.status, r.ip, switchText, r.entry, r.mac, r.failure); !ok {
				continue
			}
		case r.status == "failed" || r.status == "timeout" || r.status == "needs_manual_confirmation":
			taskID, err := c.newCard(ctx, platform, "retry", r.ip, r.job, r.owner, r.place)
			if err != nil {
				continue
			}
			tone := "danger"
			if r.status == "needs_manual_confirmation" {
				tone = "warning"
			}
			message = chatMessage{card: &chatCard{taskID: taskID, title: fmt.Sprintf("%s %s", r.ip, words[0]), desc: "交换机 " + switchText,
				note: fmt.Sprintf("%s原因：%s · 发起人：%s", meaning, chatReasonLabel(r.reason), r.actor), buttons: []chatButton{{text: "重新解绑", key: "retry"}}, jobID: r.job, tone: tone}}
		default:
			message = chatMessage{text: fmt.Sprintf("**%s %s**\n交换机：%s\n%s\n原因：%s · 发起人：%s", r.ip, words[0], switchText, meaning, chatReasonLabel(r.reason), r.actor)}
		}
		results = append(results, chatResult{jobID: r.job, place: r.place, message: message})
	}
	return results
}

// attempted counts a try at delivering a job's result.
func (c *chatCore) attempted(ctx context.Context, jobID string) error {
	_, err := c.db.ExecContext(ctx, `UPDATE bp_chat_jobs SET attempts=attempts+1 WHERE job_id=$1`, jobID)
	return err
}

// delivered records that a job's result reached its chat, so it is not sent again.
func (c *chatCore) delivered(ctx context.Context, jobID string) {
	if _, err := c.db.ExecContext(ctx, `UPDATE bp_chat_jobs SET notified_at=now() WHERE job_id=$1`, jobID); err != nil && ctx.Err() == nil {
		log.Printf("chat: record result sent: %v", err)
	}
}
