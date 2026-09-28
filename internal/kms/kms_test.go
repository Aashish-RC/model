package kms

import (
	"context"
	"crypto/rand"
	"testing"
)

func TestVault_EncryptDecrypt_RoundtripSuccess(t *testing.T) {
	ctx := context.Background()
	masterKey := make([]byte, 32)
	_, _ = rand.Read(masterKey)

	vault, err := NewLocalVault(masterKey)
	if err != nil {
		t.Fatalf("failed to create vault: %v", err)
	}

	secret := []byte("sk-proj-super-secret-api-key-1234567890")
	aad := []byte("org-enterprise:key-openai-prod")

	encrypted, err := vault.Encrypt(ctx, secret, aad)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	decrypted, err := vault.Decrypt(ctx, encrypted, aad)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}

	if string(decrypted) != string(secret) {
		t.Errorf("expected %s, got %s", secret, decrypted)
	}
}

func TestVault_MismatchedAAD_PreventsTransplantAttack(t *testing.T) {
	ctx := context.Background()
	masterKey := make([]byte, 32)
	_, _ = rand.Read(masterKey)

	vault, _ := NewLocalVault(masterKey)
	secret := []byte("sk-secret-payload")

	// Encrypted under tenant org-A
	encrypted, err := vault.Encrypt(ctx, secret, []byte("org-A"))
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}

	// Attempting to decrypt under tenant org-B must fail authentication
	_, err = vault.Decrypt(ctx, encrypted, []byte("org-B"))
	if err == nil {
		t.Fatalf("expected authentication failure for mismatched AAD")
	}
}

func TestVault_CorruptedCiphertext_FailsAuthentication(t *testing.T) {
	ctx := context.Background()
	masterKey := make([]byte, 32)
	_, _ = rand.Read(masterKey)

	vault, _ := NewLocalVault(masterKey)
	secret := []byte("sk-secret")
	aad := []byte("org-1")

	encrypted, _ := vault.Encrypt(ctx, secret, aad)

	// Tamper with the last character of the ciphertext
	tampered := encrypted[:len(encrypted)-1] + "f"
	if tampered == encrypted {
		tampered = encrypted[:len(encrypted)-1] + "0"
	}

	_, err := vault.Decrypt(ctx, tampered, aad)
	if err == nil {
		t.Fatalf("expected error on tampered ciphertext")
	}
}

func TestVault_UniqueNoncePerEncryption(t *testing.T) {
	ctx := context.Background()
	masterKey := make([]byte, 32)
	_, _ = rand.Read(masterKey)

	vault, _ := NewLocalVault(masterKey)
	secret := []byte("identical-secret")
	aad := []byte("org-1")

	c1, _ := vault.Encrypt(ctx, secret, aad)
	c2, _ := vault.Encrypt(ctx, secret, aad)

	if c1 == c2 {
		t.Errorf("encryptions of identical plaintext must produce distinct ciphertexts due to random nonces")
	}
}

func TestVault_InvalidMasterKeyLength(t *testing.T) {
	shortKey := make([]byte, 16)
	_, err := NewLocalVault(shortKey)
	if err == nil {
		t.Fatalf("expected error for 16-byte key (must be 32 bytes)")
	}
}
