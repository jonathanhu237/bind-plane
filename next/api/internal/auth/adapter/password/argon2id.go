package password

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jonathanhu237/bind-plane/next/api/internal/auth/application"
	"golang.org/x/crypto/argon2"
)

const (
	memoryKiB = 64 * 1024
	timeCost  = 3
	threads   = 4
	saltBytes = 16
	tagBytes  = 32
	phcMaxLen = 512
)

var ErrMalformedHash = errors.New("malformed password hash")

type Hasher struct {
	sem chan struct{}
}

func NewHasher(maxConcurrency int) (*Hasher, error) {
	if maxConcurrency <= 0 {
		return nil, fmt.Errorf("password hash concurrency must be positive")
	}
	return &Hasher{sem: make(chan struct{}, maxConcurrency)}, nil
}

func (h *Hasher) Hash(ctx context.Context, value string) (string, error) {
	if err := h.acquire(ctx); err != nil {
		return "", err
	}
	defer h.release()
	salt := make([]byte, saltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	tag := argon2.IDKey([]byte(value), salt, timeCost, memoryKiB, threads, tagBytes)
	return encodePHC(salt, tag), nil
}

func (h *Hasher) Verify(ctx context.Context, encoded, value string) (bool, error) {
	parsed, err := parsePHC(encoded)
	if err != nil {
		return false, err
	}
	if err := h.acquire(ctx); err != nil {
		return false, err
	}
	defer h.release()
	actual := argon2.IDKey([]byte(value), parsed.salt, parsed.timeCost, parsed.memoryKiB, parsed.threads, uint32(len(parsed.tag)))
	return subtle.ConstantTimeCompare(actual, parsed.tag) == 1, nil
}

func (h *Hasher) acquire(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	select {
	case h.sem <- struct{}{}:
		return nil
	default:
		return application.ErrPasswordHashBusy
	}
}

func (h *Hasher) release() { <-h.sem }

type parsedHash struct {
	memoryKiB uint32
	timeCost  uint32
	threads   uint8
	salt      []byte
	tag       []byte
}

func encodePHC(salt, tag []byte) string {
	encoder := base64.RawStdEncoding.EncodeToString
	return "$argon2id$v=19$m=65536,t=3,p=4$" + encoder(salt) + "$" + encoder(tag)
}

func parsePHC(encoded string) (parsedHash, error) {
	if len(encoded) == 0 || len(encoded) > phcMaxLen {
		return parsedHash{}, ErrMalformedHash
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return parsedHash{}, ErrMalformedHash
	}
	params := map[string]uint64{}
	for _, field := range strings.Split(parts[3], ",") {
		key, value, ok := strings.Cut(field, "=")
		if !ok || key == "" || value == "" {
			return parsedHash{}, ErrMalformedHash
		}
		if _, exists := params[key]; exists {
			return parsedHash{}, ErrMalformedHash
		}
		for _, digit := range value {
			if digit < '0' || digit > '9' {
				return parsedHash{}, ErrMalformedHash
			}
		}
		parsed, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return parsedHash{}, ErrMalformedHash
		}
		params[key] = parsed
	}
	if len(params) != 3 || params["m"] != memoryKiB || params["t"] != timeCost || params["p"] != threads {
		return parsedHash{}, ErrMalformedHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) != saltBytes || base64.RawStdEncoding.EncodeToString(salt) != parts[4] {
		return parsedHash{}, ErrMalformedHash
	}
	tag, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(tag) != tagBytes || base64.RawStdEncoding.EncodeToString(tag) != parts[5] {
		return parsedHash{}, ErrMalformedHash
	}
	return parsedHash{memoryKiB: uint32(params["m"]), timeCost: uint32(params["t"]), threads: uint8(params["p"]), salt: salt, tag: tag}, nil
}
