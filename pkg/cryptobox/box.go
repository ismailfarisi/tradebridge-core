// Package cryptobox provides authenticated symmetric encryption (AES-256-GCM)
// for secrets stored at rest, such as provider API keys entered in the UI.
package cryptobox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
)

// Box seals and opens secrets with a single AES-256 key.
type Box struct {
	aead cipher.AEAD
}

// New derives a 256-bit key from the given secret material (SHA-256) and builds
// a Box. Any non-empty secret works; a dedicated high-entropy key is preferred.
func New(secret string) (*Box, error) {
	if secret == "" {
		return nil, errors.New("cryptobox: empty secret")
	}
	sum := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Seal encrypts plaintext; the nonce is prepended to the ciphertext.
func (b *Box) Seal(plaintext string) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return b.aead.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

// Open decrypts a value produced by Seal.
func (b *Box) Open(ciphertext []byte) (string, error) {
	ns := b.aead.NonceSize()
	if len(ciphertext) < ns {
		return "", errors.New("cryptobox: ciphertext too short")
	}
	plaintext, err := b.aead.Open(nil, ciphertext[:ns], ciphertext[ns:], nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
