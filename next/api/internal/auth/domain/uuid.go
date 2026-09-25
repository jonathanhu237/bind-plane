package domain

import "regexp"

var canonicalUUIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func IsCanonicalUUID(value string) bool { return canonicalUUIDPattern.MatchString(value) }
