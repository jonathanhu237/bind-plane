package release

// CanConfirmRelease is evaluated again under a database lock, not just in the UI.
func CanConfirmRelease(before string, force, admin bool) bool {
	if before == "static" {
		return !force
	}
	return before == "missing" && force && admin
}

func ValidReason(value string) bool {
	switch value {
	case "temporary_test", "user_report", "ip_mac_change", "wrong_binding_fix", "security_response", "other":
		return true
	default:
		return false
	}
}
