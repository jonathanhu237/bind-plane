package release

import "testing"

func TestResolveNetworkLongestPrefixAndAmbiguity(t *testing.T) {
	rows := []Network{
		{SwitchID: "a", CIDR: "10.1.0.0/16", Enabled: true, Validated: true, SwitchEnabled: true},
		{SwitchID: "b", CIDR: "10.1.2.0/24", Enabled: true, Validated: true, SwitchEnabled: true},
		{SwitchID: "c", CIDR: "10.1.2.0/24", Enabled: false, Validated: true, SwitchEnabled: true},
		{SwitchID: "d", CIDR: "10.1.2.0/25", Enabled: true, Validated: false, SwitchEnabled: true},
	}
	result, err := ResolveNetwork("10.1.2.7", rows)
	if err != nil || result.SwitchID != "b" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	rows[2].Enabled = true
	if _, err := ResolveNetwork("10.1.2.7", rows); err != ErrAmbiguous {
		t.Fatalf("expected ambiguity, got %v", err)
	}
	if _, err := ResolveNetwork("10.2.0.1", rows); err != ErrNoSwitch {
		t.Fatalf("expected no switch, got %v", err)
	}
}

func TestResolveNetworkRejectsIPv6AndInvalidData(t *testing.T) {
	if _, err := ResolveNetwork("::1", nil); err != ErrInvalidIP {
		t.Fatalf("IPv6: %v", err)
	}
	if _, err := ResolveNetwork("10.1.1.2,10.1.1.3", nil); err != ErrInvalidIP {
		t.Fatalf("batch: %v", err)
	}
}
