package application

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/domain"
	_ "golang.org/x/image/webp"
)

const (
	DefaultSystemName      = "Temvia"
	MaxSystemNameLength    = 50
	MaxSystemIconBytes     = 2 * 1024 * 1024
	MaxSystemIconDimension = 4096
	MaxSystemIconPixels    = 16 * 1024 * 1024
	SystemIconPreserve     = "preserve"
	SystemIconReplace      = "replace"
	SystemIconDefault      = "default"
	defaultSystemIconMIME  = "image/svg+xml"
	// Lucide Layers (ISC), matching the React Layers3 default mark.
	defaultSystemIconMarkup = `<svg xmlns="http://www.w3.org/2000/svg" width="64" height="64" viewBox="0 0 64 64"><!-- Lucide Layers. Copyright (c) 2026 Lucide Icons and Contributors. ISC License: Permission to use, copy, modify, and/or distribute this software for any purpose with or without fee is hereby granted, provided that the above copyright notice and this permission notice appear in all copies. THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE. --><rect width="64" height="64" rx="16" fill="#18181b"/><g transform="translate(16 16) scale(1.3333333333)" fill="none" stroke="white" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M12.83 2.18a2 2 0 0 0-1.66 0L2.6 6.08a1 1 0 0 0 0 1.83l8.58 3.91a2 2 0 0 0 1.66 0l8.58-3.9a1 1 0 0 0 0-1.83z"/><path d="M2 12a1 1 0 0 0 .58.91l8.6 3.91a2 2 0 0 0 1.65 0l8.58-3.9A1 1 0 0 0 22 12"/><path d="M2 17a1 1 0 0 0 .58.91l8.6 3.91a2 2 0 0 0 1.65 0l8.58-3.9A1 1 0 0 0 22 17"/></g></svg>`
)

// SystemIdentityRecord is the persistence projection for the shared product
// identity. IconBytes is nil when the built-in icon is active.
type SystemIdentityRecord struct {
	SystemName    string
	IconMediaType string
	IconBytes     []byte
	Revision      int64
	UpdatedAt     time.Time
}

type SystemIdentityView struct {
	SystemName    string
	IconMediaType string
	IconBytes     []byte
	Revision      int64
	UpdatedAt     time.Time
}

type SystemIdentityInput struct {
	SystemName    string
	IconAction    string
	IconMediaType string
	IconBytes     []byte
	Revision      int64
}

type SystemIdentityStore interface {
	GetSystemIdentity(context.Context) (SystemIdentityRecord, error)
	SaveSystemIdentity(context.Context, int64, SystemIdentityRecord) (SystemIdentityRecord, error)
}

// SystemIdentityProvider is deliberately independent from email settings so a
// mail worker can resolve the current brand without knowing SMTP credentials.
type SystemIdentityProvider interface {
	CurrentSystemIdentity(context.Context) (SystemIdentityView, error)
}

type SystemIdentityManagement struct {
	store  SystemIdentityStore
	saveMu sync.Mutex
}

func NewSystemIdentityManagement(store SystemIdentityStore) *SystemIdentityManagement {
	return &SystemIdentityManagement{store: store}
}

func (s *SystemIdentityManagement) CurrentSystemIdentity(ctx context.Context) (SystemIdentityView, error) {
	return s.GetSystemIdentity(ctx)
}

func (s *SystemIdentityManagement) GetSystemIdentity(ctx context.Context) (SystemIdentityView, error) {
	if s == nil || s.store == nil {
		return SystemIdentityView{}, ErrDependencyUnavailable
	}
	record, err := s.store.GetSystemIdentity(ctx)
	if errors.Is(err, ErrSystemIdentityNotConfigured) {
		return defaultSystemIdentityView(), nil
	}
	if err != nil {
		return SystemIdentityView{}, dependencyError(err)
	}
	return systemIdentityView(record), nil
}

func (s *SystemIdentityManagement) SaveSystemIdentity(ctx context.Context, input SystemIdentityInput) (SystemIdentityView, error) {
	if s == nil || s.store == nil {
		return SystemIdentityView{}, ErrDependencyUnavailable
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()

	name, err := normalizeSystemIdentityName(input.SystemName)
	if err != nil {
		return SystemIdentityView{}, err
	}
	action := strings.TrimSpace(strings.ToLower(input.IconAction))
	if action == "" {
		action = SystemIconPreserve
	}
	if action != SystemIconPreserve && action != SystemIconReplace && action != SystemIconDefault {
		return SystemIdentityView{}, invalidIdentityField("iconAction", "invalid_action")
	}
	if input.Revision < 0 {
		return SystemIdentityView{}, ErrStaleRevision
	}

	current, currentErr := s.store.GetSystemIdentity(ctx)
	hasCurrent := currentErr == nil
	if errors.Is(currentErr, ErrSystemIdentityNotConfigured) {
		current = defaultSystemIdentityRecord()
	} else if currentErr != nil {
		return SystemIdentityView{}, dependencyError(currentErr)
	}
	currentRevision := current.Revision
	if !hasCurrent {
		currentRevision = 0
	}
	if input.Revision != currentRevision {
		return SystemIdentityView{}, ErrStaleRevision
	}

	record := SystemIdentityRecord{SystemName: name, Revision: input.Revision}
	switch action {
	case SystemIconPreserve:
		record.IconMediaType = current.IconMediaType
		record.IconBytes = append([]byte(nil), current.IconBytes...)
	case SystemIconDefault:
		// An empty icon projection activates the built-in icon. The default icon
		// itself is served by the HTTP adapter and is never persisted as bytes.
	case SystemIconReplace:
		mediaType, iconErr := validateSystemIcon(input.IconBytes, input.IconMediaType)
		if iconErr != nil {
			return SystemIdentityView{}, iconErr
		}
		record.IconMediaType = mediaType
		record.IconBytes = append([]byte(nil), input.IconBytes...)
	}

	saved, err := s.store.SaveSystemIdentity(ctx, input.Revision, record)
	if err != nil {
		if errors.Is(err, ErrStaleRevision) {
			return SystemIdentityView{}, ErrStaleRevision
		}
		return SystemIdentityView{}, dependencyError(err)
	}
	return systemIdentityView(saved), nil
}

func (v SystemIdentityView) DisplayName() string {
	if name := strings.TrimSpace(v.SystemName); name != "" {
		return name
	}
	return DefaultSystemName
}

func DefaultSystemIcon() (mediaType string, data []byte) {
	return defaultSystemIconMIME, []byte(defaultSystemIconMarkup)
}

func (v SystemIdentityView) HasCustomIcon() bool {
	return len(v.IconBytes) > 0 && v.IconMediaType != ""
}

func defaultSystemIdentityRecord() SystemIdentityRecord {
	return SystemIdentityRecord{SystemName: DefaultSystemName, Revision: 0}
}

func defaultSystemIdentityView() SystemIdentityView {
	return systemIdentityView(defaultSystemIdentityRecord())
}

func systemIdentityView(record SystemIdentityRecord) SystemIdentityView {
	name := strings.TrimSpace(record.SystemName)
	if name == "" {
		name = DefaultSystemName
	}
	return SystemIdentityView{SystemName: name, IconMediaType: record.IconMediaType, IconBytes: append([]byte(nil), record.IconBytes...), Revision: record.Revision, UpdatedAt: record.UpdatedAt}
}

func normalizeSystemIdentityName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if err := validateSystemIdentityName(name, "systemName"); err != nil {
		return "", err
	}
	return name, nil
}

func validateSystemIdentityName(value, field string) error {
	if value == "" {
		return invalidIdentityField(field, "required")
	}
	if len([]rune(value)) > MaxSystemNameLength {
		return invalidIdentityField(field, "too_long")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return invalidIdentityField(field, "control_character")
		}
	}
	return nil
}

func invalidIdentityField(field, code string) error {
	return &domain.ValidationErrors{Items: []domain.FieldError{{Field: field, Code: code}}}
}

func validateSystemIcon(data []byte, suppliedMediaType string) (string, error) {
	if len(data) == 0 || len(data) > MaxSystemIconBytes {
		return "", invalidIdentityField("icon", "invalid_icon")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > MaxSystemIconDimension || config.Height > MaxSystemIconDimension || config.Width > math.MaxInt/config.Height || config.Width*config.Height > MaxSystemIconPixels {
		return "", invalidIdentityField("icon", "invalid_icon")
	}
	mediaType := map[string]string{"png": "image/png", "jpeg": "image/jpeg", "webp": "image/webp"}[format]
	if mediaType == "" {
		return "", invalidIdentityField("icon", "unsupported_type")
	}
	if supplied := strings.ToLower(strings.TrimSpace(suppliedMediaType)); supplied != "" && supplied != mediaType {
		return "", invalidIdentityField("icon", "invalid_icon")
	}
	// DecodeConfig only validates the image header. Decode the complete image
	// before accepting it so a truncated payload with a valid header cannot be
	// published or served as the system icon.
	if _, decodedFormat, decodeErr := image.Decode(bytes.NewReader(data)); decodeErr != nil || decodedFormat != format {
		return "", invalidIdentityField("icon", "invalid_icon")
	}
	return mediaType, nil
}
