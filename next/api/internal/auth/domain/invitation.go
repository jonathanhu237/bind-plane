package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
)

const InvitationTokenVersion = "v1"
const invitationContext = "temvia-user-invitation-v1"

type InvitationMaterial struct {
	Selector       []byte
	VerifierDigest []byte
}

func NewInvitationMaterial(key, selector []byte) (InvitationMaterial, error) {
	verifier, err := deriveInvitationVerifier(key, selector)
	if err != nil {
		return InvitationMaterial{}, err
	}
	digest := sha256.Sum256(verifier)
	return InvitationMaterial{Selector: append([]byte(nil), selector...), VerifierDigest: append([]byte(nil), digest[:]...)}, nil
}

func NewInvitationToken(key, selector []byte) (string, error) {
	verifier, err := deriveInvitationVerifier(key, selector)
	if err != nil {
		return "", err
	}
	return InvitationTokenVersion + "." + base64.RawURLEncoding.EncodeToString(selector) + "." + base64.RawURLEncoding.EncodeToString(verifier), nil
}

func deriveInvitationVerifier(key, selector []byte) ([]byte, error) {
	if len(key) != 32 || len(selector) != 16 {
		return nil, fmt.Errorf("invalid invitation key or selector length")
	}
	message := append([]byte(invitationContext), selector...)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(message)
	return mac.Sum(nil), nil
}

func ParseInvitationToken(value string) (selector, verifierDigest []byte, ok bool) {
	parts := strings.Split(value, ".")
	if len(parts) != 3 || parts[0] != InvitationTokenVersion {
		return nil, nil, false
	}
	selector, ok = decodeCanonicalBase64URL(parts[1], 16)
	if !ok {
		return nil, nil, false
	}
	verifier, ok := decodeCanonicalBase64URL(parts[2], 32)
	if !ok {
		return nil, nil, false
	}
	digest := sha256.Sum256(verifier)
	return selector, append([]byte(nil), digest[:]...), true
}
