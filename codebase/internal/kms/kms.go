package kms

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

var (
	ErrInvalidMasterKey    = errors.New("master key must be exactly 32 bytes (256-bit)")
	ErrInvalidCiphertext   = errors.New("invalid ciphertext envelope format")
	ErrAuthenticationFail  = errors.New("ciphertext authentication failed (tampered data or mismatched AAD)")
)

// Vault defines the encryption and decryption abstraction for credentials at rest.
type Vault interface {
	Encrypt(ctx context.Context, plaintext []byte, aad []byte) (string, error)
	Decrypt(ctx context.Context, ciphertext string, aad []byte) ([]byte, error)
}

// LocalVault implements Vault using AES-256-GCM envelope encryption.
type LocalVault struct {
	masterKey []byte
}

// NewLocalVault initializes a LocalVault with a 32-byte AES-256 master key.
func NewLocalVault(key []byte) (*LocalVault, error) {
	if len(key) != 32 {
		return nil, ErrInvalidMasterKey
	}
	keyCopy := make([]byte, 32)
	copy(keyCopy, key)
	return &LocalVault{masterKey: keyCopy}, nil
}

// Encrypt encrypts plaintext with AES-256-GCM and binds it to additional authenticated data (AAD).
func (v *LocalVault) Encrypt(ctx context.Context, plaintext []byte, aad []byte) (string, error) {
	block, err := aes.NewCipher(v.masterKey)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher block: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate random nonce: %w", err)
	}

	sealed := gcm.Seal(nil, nonce, plaintext, aad)

	// Format: v1:<nonce_hex>:<ciphertext_hex>
	envelope := fmt.Sprintf("v1:%s:%s", hex.EncodeToString(nonce), hex.EncodeToString(sealed))
	return envelope, nil
}

// Decrypt authenticates and decrypts an envelope ciphertext using the master key and expected AAD.
func (v *LocalVault) Decrypt(ctx context.Context, ciphertext string, aad []byte) ([]byte, error) {
	parts := strings.Split(ciphertext, ":")
	if len(parts) != 3 || parts[0] != "v1" {
		return nil, ErrInvalidCiphertext
	}

	nonce, err := hex.DecodeString(parts[1])
	if err != nil {
		return nil, ErrInvalidCiphertext
	}

	sealed, err := hex.DecodeString(parts[2])
	if err != nil {
		return nil, ErrInvalidCiphertext
	}

	block, err := aes.NewCipher(v.masterKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher block: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	if len(nonce) != gcm.NonceSize() {
		return nil, ErrInvalidCiphertext
	}

	plaintext, err := gcm.Open(nil, nonce, sealed, aad)
	if err != nil {
		return nil, ErrAuthenticationFail
	}

	return plaintext, nil
}
