// Package crypto provides encryption utilities for sensitive data.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

var (
	ErrMissingKey         = errors.New("encryption key is required")
	ErrInvalidKey         = errors.New("encryption key must be 32 bytes for AES-256")
	ErrCiphertextTooShort = errors.New("ciphertext too short")
)

// Encryptor defines the contract for encrypting/decrypting sensitive values.
type Encryptor interface {
	Encrypt(plaintext string) (string, error)
	Decrypt(ciphertext string) (string, error)
}

type aesGCMEncryptor struct {
	aead cipher.AEAD
}

// NewEncryptor creates an AES-256-GCM encryptor from a 32-byte key.
func NewEncryptor(key string) (Encryptor, error) {
	if key == "" {
		return nil, ErrMissingKey
	}

	keyBytes := []byte(key)
	if len(keyBytes) != 32 {
		return nil, ErrInvalidKey
	}

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	return &aesGCMEncryptor{aead: aead}, nil
}

// Encrypt encrypts plaintext using AES-256-GCM.
func (e *aesGCMEncryptor) Encrypt(plaintext string) (string, error) {
	nonce := make([]byte, e.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := e.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.URLEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts ciphertext that was encrypted with Encrypt.
func (e *aesGCMEncryptor) Decrypt(ciphertext string) (string, error) {
	data, err := base64.URLEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	nonceSize := e.aead.NonceSize()
	if len(data) < nonceSize {
		return "", ErrCiphertextTooShort
	}

	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]
	plaintext, err := e.aead.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}
