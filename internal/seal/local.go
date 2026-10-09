package seal

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"strings"
)

// Local seals with AES-GCM under a key derived from a local secret, with the
// user id as additional data. It exists because floci has no KMS, so compose
// can still store keys. Production must use KMS.
type Local struct {
	aead cipher.AEAD
}

// OpenLocal derives the key from secret. The secret must be at least 16 bytes.
func OpenLocal(secret string) (*Local, error) {
	if len(secret) < 16 {
		return nil, errors.New("local seal secret must be at least 16 characters")
	}
	sum := sha256.Sum256([]byte("scholia-local-seal\n" + secret))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Local{aead: aead}, nil
}

// Seal encrypts plaintext for userID with a random nonce prefix.
func (b *Local) Seal(_ context.Context, userID string, plaintext []byte) ([]byte, error) {
	if strings.TrimSpace(userID) == "" || len(plaintext) == 0 {
		return nil, errors.New("could not seal key")
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, errors.New("could not seal key")
	}
	return b.aead.Seal(nonce, nonce, plaintext, []byte(userID)), nil
}

// Open decrypts ciphertext for userID. A different user id fails.
func (b *Local) Open(_ context.Context, userID string, ciphertext []byte) ([]byte, error) {
	n := b.aead.NonceSize()
	if strings.TrimSpace(userID) == "" || len(ciphertext) <= n {
		return nil, errors.New("could not open key")
	}
	out, err := b.aead.Open(nil, ciphertext[:n], ciphertext[n:], []byte(userID))
	if err != nil {
		return nil, errors.New("could not open key")
	}
	return out, nil
}
