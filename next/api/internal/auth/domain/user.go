package domain

import "time"

type Locale string

const (
	LocaleEnglish Locale = "en"
	LocaleChinese Locale = "zh-CN"
)

func (l Locale) Valid() bool { return l == LocaleEnglish || l == LocaleChinese }

type User struct {
	ID            string
	Name          string
	Email         string
	CreatedAt     time.Time
	Disabled      bool
	Locale        Locale
	HasAvatar     bool
	AvatarVersion int64
}

type Avatar struct {
	MediaType string
	Bytes     []byte
	Version   int64
	UpdatedAt time.Time
}

type EmailChangeRequest struct {
	ID                string
	UserID            string
	OldEmail          string
	NewEmail          string
	ExpiresAt         time.Time
	ResendAfter       time.Time
	AttemptsRemaining int
	Revision          int64
	CreatedAt         time.Time
	// Selector is an internal verifier input and is never serialized by the
	// HTTP transport. It is present only while the application completes a
	// request; status responses clear it.
	Selector       []byte
	VerifierDigest []byte
}

type Account struct {
	User         User
	PasswordHash string
	AuthVersion  int64
}
