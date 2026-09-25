package application

import (
	"encoding/base64"
	"encoding/json"
	"errors"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
)

// AccessListOptions describes a stable, cursor-based access listing. Query,
// sort, and direction are part of the cursor context so a cursor cannot be
// accidentally reused for a different result set.
type AccessListOptions struct {
	Cursor    string
	Query     string
	RoleID    string
	Status    string
	Sort      string
	Direction string
	Limit     int
}

const (
	DefaultAccessPageSize = 25
	MaxAccessPageSize     = 100
)

type AccessCursor struct {
	Version   int    `json:"v"`
	Query     string `json:"q"`
	RoleID    string `json:"role,omitempty"`
	Status    string `json:"status,omitempty"`
	Sort      string `json:"s"`
	Direction string `json:"d"`
	Value     string `json:"value"`
	ID        string `json:"id"`
}

var ErrInvalidCursor = errors.New("invalid cursor")

func EncodeAccessCursor(cursor AccessCursor) (string, error) {
	if cursor.Version == 0 {
		cursor.Version = 1
	}
	if cursor.Version != 1 || !domain.IsCanonicalUUID(cursor.ID) || cursor.Sort == "" || cursor.Direction == "" {
		return "", ErrInvalidCursor
	}
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", ErrInvalidCursor
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func DecodeAccessCursor(value string) (AccessCursor, error) {
	if value == "" {
		return AccessCursor{}, ErrInvalidCursor
	}
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return AccessCursor{}, ErrInvalidCursor
	}
	var cursor AccessCursor
	if err := json.Unmarshal(payload, &cursor); err != nil || cursor.Version != 1 || cursor.Sort == "" || cursor.Direction == "" || !domain.IsCanonicalUUID(cursor.ID) {
		return AccessCursor{}, ErrInvalidCursor
	}
	return cursor, nil
}
