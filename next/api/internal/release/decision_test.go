package release

import "testing"

func TestValidReason(t *testing.T) {
	for reason, valid := range map[string]bool{
		"temporary_test": true,
		"other":          true,
		"":               false,
		"Temporary_Test": false,
		"delete":         false,
	} {
		if got := ValidReason(reason); got != valid {
			t.Errorf("%q got %v", reason, got)
		}
	}
}
