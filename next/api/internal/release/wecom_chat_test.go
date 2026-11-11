package release

import (
	"strings"
	"testing"
)

// The bot acts only on a command: the verb first, then one address and, to release at once, a
// reason from the list. Anything else is explained, never guessed at.
func TestParseWecomCommandIsStrict(t *testing.T) {
	for _, tc := range []struct{ text, verb, ip, reason, problem string }{
		{text: "", verb: "help"},
		{text: "@Bind Plane", verb: "help"},
		{text: "@Bind Plane 测试", verb: "help"},
		{text: "@Bind Plane 你好呀", verb: "help"},
		{text: "@Bind Plane 测试 解绑 10.20.30.40 其他", verb: "release", ip: "10.20.30.40", reason: "other"},
		{text: "@Bind Plane 测试 查询10.20.30.40", verb: "lookup", ip: "10.20.30.40"},
		{text: "帮助", verb: "help"},
		{text: "HELP", verb: "help"},
		{text: "？", verb: "help"},
		{text: "查询 10.20.30.40", verb: "lookup", ip: "10.20.30.40"},
		{text: "@Bind Plane query 10.20.30.40", verb: "lookup", ip: "10.20.30.40"},
		{text: "查询10.20.30.40", verb: "lookup", ip: "10.20.30.40"},
		{text: "解绑 10.20.30.40", verb: "release", ip: "10.20.30.40"},
		{text: "Release 10.20.30.40", verb: "release", ip: "10.20.30.40"},
		{text: "解绑 10.20.30.40 临时测试", verb: "release", ip: "10.20.30.40", reason: "temporary_test"},
		{text: "解绑10.20.30.40 安全处置", verb: "release", ip: "10.20.30.40", reason: "security_response"},
		{text: "解绑 10.20.30.40 IP/MAC变更", verb: "release", ip: "10.20.30.40", reason: "ip_mac_change"},
		{text: "解绑 10.20.30.40 ip / mac 变更", verb: "release", ip: "10.20.30.40", reason: "ip_mac_change"},
		{text: "release 10.20.30.40 user_report", verb: "release", ip: "10.20.30.40", reason: "user_report"},
		{text: "10.20.30.40", problem: "没看懂"},
		{text: "帮我解绑 10.20.30.40", problem: "没看懂"},
		{text: "你好", problem: "没看懂"},
		{text: "@Bind Plane 帮我看看 10.20.30.40", problem: "没看懂"},
		{text: "解绑", problem: "缺少要解绑的 IPv4 地址"},
		{text: "查询", problem: "缺少要查询的 IPv4 地址"},
		{text: "解绑 10.20.30.400", problem: "不是一个 IPv4 地址"},
		{text: "解绑 10.20.30.040", problem: "不是一个 IPv4 地址"},
		{text: "解绑 10.20.30.0/24", problem: "不是一个 IPv4 地址"},
		{text: "解绑 10.20.30.40 10.20.30.41", problem: "不是可用的原因"},
		{text: "解绑 10.20.30.40 随便", problem: "不是可用的原因"},
		{text: "查询 10.20.30.40 临时测试", problem: "查询只需要一个地址"},
	} {
		got := parseWecomCommand(tc.text)
		if tc.problem != "" {
			if !strings.Contains(got.problem, tc.problem) || got.verb != "" || got.ip != "" {
				t.Errorf("%q: %+v, want a problem about %q", tc.text, got, tc.problem)
			}
			continue
		}
		if got != (wecomCommand{verb: tc.verb, ip: tc.ip, reason: tc.reason}) {
			t.Errorf("%q: %+v, want %s %s %s", tc.text, got, tc.verb, tc.ip, tc.reason)
		}
	}
}

// Every reason the bot accepts is one the console offers, and the help lists them all.
func TestWecomReasonsMatchTheHelp(t *testing.T) {
	for _, reason := range wecomReasons {
		if wecomReasonCode(reason.label) != reason.code || wecomReasonCode(reason.code) != reason.code {
			t.Errorf("reason %s not accepted by name or code", reason.code)
		}
		if !strings.Contains(strings.ReplaceAll(wecomHelp, " ", ""), strings.ReplaceAll(reason.label, " ", "")) {
			t.Errorf("help does not list %s", reason.label)
		}
	}
}
