package release

func ValidReason(value string) bool {
	switch value {
	case "temporary_test", "user_report", "ip_mac_change", "wrong_binding_fix", "security_response", "other":
		return true
	default:
		return false
	}
}
