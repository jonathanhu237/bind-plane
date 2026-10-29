package release

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
)

// configBackupAAD separates sealed configurations from sealed credentials under the same key,
// so neither ciphertext can be substituted for the other.
const configBackupAAD = "bind-plane config backup v1"

func configCipher(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func sealConfig(key []byte, plaintext string) (string, error) {
	gcm, err := configCipher(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	return "v1:" + base64.RawURLEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plaintext), []byte(configBackupAAD))), nil
}

func openConfig(key []byte, sealed string) (string, error) {
	encoded, ok := strings.CutPrefix(sealed, "v1:")
	if !ok {
		return "", errors.New("unsupported configuration encryption format")
	}
	packed, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	gcm, err := configCipher(key)
	if err != nil {
		return "", err
	}
	if len(packed) < gcm.NonceSize() {
		return "", errors.New("sealed configuration is truncated")
	}
	plain, err := gcm.Open(nil, packed[:gcm.NonceSize()], packed[gcm.NonceSize():], []byte(configBackupAAD))
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
