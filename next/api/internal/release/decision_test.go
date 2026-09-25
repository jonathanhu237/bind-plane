package release

import "testing"

func TestCanConfirmRelease(t *testing.T) {
	for _, tc := range []struct {
		before       string
		force, admin bool
		allowed      bool
	}{
		{"static", false, false, true},
		{"static", true, true, false},
		{"dynamic", false, false, true},
		{"missing", false, true, false},
		{"missing", true, false, false},
		{"missing", true, true, true},
		{"unknown", true, true, false},
	} {
		if got := CanConfirmRelease(tc.before, tc.force, tc.admin); got != tc.allowed {
			t.Errorf("%+v got %v", tc, got)
		}
	}
}
