package crypto

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestNewEncryptor(t *testing.T) {
	t.Parallel()

	t.Run("missing key", func(t *testing.T) {
		t.Parallel()

		_, err := NewEncryptor("")
		if !errors.Is(err, ErrMissingKey) {
			t.Fatalf("expected ErrMissingKey, got %v", err)
		}
	})

	t.Run("invalid key length", func(t *testing.T) {
		t.Parallel()

		_, err := NewEncryptor("short")
		if !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("expected ErrInvalidKey, got %v", err)
		}
	})

	t.Run("valid key length", func(t *testing.T) {
		t.Parallel()

		enc, err := NewEncryptor(strings.Repeat("k", 32))
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if enc == nil {
			t.Fatal("expected encryptor instance")
		}
	})
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	t.Parallel()

	enc, err := NewEncryptor(strings.Repeat("k", 32))
	if err != nil {
		t.Fatalf("failed to build encryptor: %v", err)
	}

	first, err := enc.Encrypt("super-secret")
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}
	second, err := enc.Encrypt("super-secret")
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	if first == second {
		t.Fatal("ciphertexts should differ due to random nonce")
	}

	plaintext, err := enc.Decrypt(first)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}
	if plaintext != "super-secret" {
		t.Fatalf("unexpected plaintext: got %q", plaintext)
	}
}

func TestDecryptErrors(t *testing.T) {
	t.Parallel()

	keyA := strings.Repeat("a", 32)
	keyB := strings.Repeat("b", 32)

	encA, err := NewEncryptor(keyA)
	if err != nil {
		t.Fatalf("failed to build encryptor A: %v", err)
	}
	encB, err := NewEncryptor(keyB)
	if err != nil {
		t.Fatalf("failed to build encryptor B: %v", err)
	}

	t.Run("invalid base64", func(t *testing.T) {
		t.Parallel()

		_, err := encA.Decrypt("%%%")
		if err == nil {
			t.Fatal("expected base64 decode error")
		}
	})

	t.Run("ciphertext too short", func(t *testing.T) {
		t.Parallel()

		encoded := base64.URLEncoding.EncodeToString([]byte("tiny"))
		_, err := encA.Decrypt(encoded)
		if !errors.Is(err, ErrCiphertextTooShort) {
			t.Fatalf("expected ErrCiphertextTooShort, got %v", err)
		}
	})

	t.Run("wrong key", func(t *testing.T) {
		t.Parallel()

		ciphertext, err := encA.Encrypt("secret")
		if err != nil {
			t.Fatalf("encrypt failed: %v", err)
		}

		_, err = encB.Decrypt(ciphertext)
		if err == nil {
			t.Fatal("expected decrypt error with wrong key")
		}
	})
}
