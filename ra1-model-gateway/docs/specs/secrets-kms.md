# Specification: Secrets & KMS Integration (Sealed Secret Vault)

**Status:** Draft (Wave 3)  
**Layer:** Cryptographic Key Management & Storage (`internal/kms`)  
**Design Reference Mode:** `BOTH`  

---

## 1. Purpose & Overview

Production deployments of `ra1-model-gateway` must never persist raw upstream provider credentials (e.g. OpenAI, Anthropic, Gemini API keys) in plaintext inside PostgreSQL or in-memory dumps.

The Secrets & KMS subsystem provides envelope encryption and authenticated data protection (AES-256-GCM) for all credentials at rest. The master key can be derived from an environment master key (`GATEWAY_MASTER_KEY`), AWS KMS, Google Cloud KMS, or HashiCorp Vault.

---

## 2. Envelope Encryption Architecture

1. **Master Key (KEK - Key Encryption Key):**
   - 256-bit cryptographic key loaded securely at startup or fetched from cloud KMS.
2. **Data Encryption (DEK - Data Encryption Key / Direct AES-256-GCM):**
   - Standard Galois/Counter Mode (GCM) provides both confidentiality and integrity authentication.
   - For every credential encrypted:
     - Generates a unique 96-bit cryptographically random Nonce (`crypto/rand`).
     - Encrypts plaintext API key: $C = \text{AES-GCM-Encrypt}(K_{master}, \text{Nonce}, P, \text{AAD})$.
     - Additional Authenticated Data ($\text{AAD}$) binds the ciphertext to the `OrgID` and `KeyID`, preventing ciphertext transplant attacks across organizations.
3. **Sealed Payload Format:**
   - Base64-encoded binary string containing:
     $$\text{v1}:[\text{12-byte Nonce}][\text{Ciphertext}][\text{16-byte Authentication Tag}]$$

---

## 3. Interfaces & Provider Abstraction

```go
type Vault interface {
    Encrypt(ctx context.Context, plaintext []byte, aad []byte) (string, error)
    Decrypt(ctx context.Context, ciphertext string, aad []byte) ([]byte, error)
}
```

- **LocalVault:** In-memory / environment variable AES-256-GCM implementation for sealed local delivery.
- **KMSVault:** Envelope encryption delegating KEK operations to cloud KMS / Vault providers.

---

## 4. Acceptance Tests

1. `TestVault_EncryptDecrypt_RoundtripSuccess`
2. `TestVault_CorruptedCiphertext_FailsAuthentication`
3. `TestVault_MismatchedAAD_PreventsTransplantAttack`
4. `TestVault_UniqueNoncePerEncryption`
5. `TestVault_MissingMasterKey_RejectsInitialization`
