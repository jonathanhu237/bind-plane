package release

import (
	"strings"
	"testing"
	"time"
)

func TestCronNextMatchesStandardSemantics(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, tc := range []struct{ expr, after, want string }{
		{"0 */4 * * *", "2026-10-30 03:39", "2026-10-30 04:00"},
		{"0 */4 * * *", "2026-10-30 04:00", "2026-10-30 08:00"},
		{"*/5 * * * *", "2026-10-30 03:39", "2026-10-30 03:40"},
		{"30 2 * * *", "2026-10-30 03:00", "2026-10-31 02:30"},
		{"0 2 1 * *", "2026-10-30 03:00", "2026-11-01 02:00"},
		{"0 3 * * 1", "2026-10-30 03:00", "2026-11-02 03:00"},            // next Monday
		{"0 0 * * 7", "2026-10-30 03:00", "2026-11-01 00:00"},            // 7 is Sunday
		{"0 0 13 * 5", "2026-10-30 03:00", "2026-11-06 00:00"},           // day 13 OR Friday
		{"15,45 9-17/4 * * 1-6", "2026-10-30 03:00", "2026-10-30 09:15"}, // Monday to Saturday
		{"15,45 9-17/4 * 1-6 *", "2026-10-30 03:00", "2027-01-01 09:15"}, // January to June
		{"0 0 1 1 *", "2026-12-31 23:59", "2027-01-01 00:00"},
	} {
		c, err := ParseCron(tc.expr)
		if err != nil {
			t.Fatalf("%s: %v", tc.expr, err)
		}
		if got := c.Next(at(tc.after)); !got.Equal(at(tc.want)) {
			t.Errorf("%s after %s = %s, want %s", tc.expr, tc.after, got.Format("2006-01-02 15:04"), tc.want)
		}
	}
	never, _ := ParseCron("0 0 30 2 *")
	if !never.Next(at("2026-01-01 00:00")).IsZero() {
		t.Error("30 February matched")
	}
	for _, bad := range []string{"", "* * * *", "60 * * * *", "* 24 * * *", "* * 0 * *", "* * * 13 *", "* * * * 8", "*/0 * * * *", "5-1 * * * *", "a * * * *"} {
		if _, err := ParseCron(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestDiffLinesProducesMinimalHunks(t *testing.T) {
	split := func(s string) []string { return strings.Split(s, "\n") }
	old := split("sysname SW1\n#\nvlan 10\nvlan 20\n#\ninterface Vlanif10\n ip address 10.0.0.1 255.255.255.0\n#\nreturn")
	new := split("sysname SW1\n#\nvlan 10\nvlan 30\n#\ninterface Vlanif10\n ip address 10.0.0.1 255.255.255.0\n ntp enable\n#\nreturn")
	d := DiffLines(old, new, 1)
	if d.Added != 2 || d.Removed != 1 || d.Replaced || len(d.Hunks) != 2 {
		t.Fatalf("%+v", d)
	}
	first := d.Hunks[0]
	if first.OldStart != 3 || first.OldLines != 3 || first.NewStart != 3 || first.NewLines != 3 ||
		first.Lines[1] != (DiffLine{"remove", "vlan 20"}) || first.Lines[2] != (DiffLine{"add", "vlan 30"}) {
		t.Fatalf("first hunk %+v", first)
	}
	if got := d.Hunks[1].Lines; got[1] != (DiffLine{"add", " ntp enable"}) || got[0].Kind != "context" {
		t.Fatalf("second hunk %+v", got)
	}
	if same := DiffLines(old, old, 3); same.Added != 0 || same.Removed != 0 || len(same.Hunks) != 0 {
		t.Fatalf("identical input %+v", same)
	}
	if all := DiffLines(nil, []string{"a", "b"}, 3); all.Added != 2 || len(all.Hunks) != 1 || all.Hunks[0].OldLines != 0 {
		t.Fatalf("from empty %+v", all)
	}
}

func TestDiffLinesFallsBackToReplacementForUnalignableInput(t *testing.T) {
	var a, b []string
	for i := 0; i < maxDiffEdits; i++ {
		a = append(a, "old "+strings.Repeat("x", i%7))
		b = append(b, "new "+strings.Repeat("y", i%5))
	}
	d := DiffLines(a, b, 3)
	if !d.Replaced || d.Added != len(b) || d.Removed != len(a) {
		t.Fatalf("replaced=%v added=%d removed=%d", d.Replaced, d.Added, d.Removed)
	}
}

func TestConfigSealingIsBoundToItsPurpose(t *testing.T) {
	key := make([]byte, 32)
	sealed, err := sealConfig(key, "local-user admin password irreversible-cipher $1a$secret")
	if err != nil || strings.Contains(sealed, "secret") {
		t.Fatalf("%q %v", sealed, err)
	}
	if plain, err := openConfig(key, sealed); err != nil || !strings.HasSuffix(plain, "$1a$secret") {
		t.Fatalf("%q %v", plain, err)
	}
	// A credential sealed without the configuration label must not open as a configuration.
	credential, err := (&AdminHandler{key: key}).seal("switch-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = openConfig(key, credential); err == nil {
		t.Fatal("credential ciphertext opened as configuration")
	}
	if _, err = openConfig(key, "v1:AAAA"); err == nil {
		t.Fatal("truncated ciphertext opened")
	}
}
