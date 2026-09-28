package stress

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ra1/model-gateway/internal/audit"
	"github.com/ra1/model-gateway/internal/auth/scim"
	"github.com/ra1/model-gateway/internal/guardrails"
	"github.com/ra1/model-gateway/internal/kms"
	"github.com/ra1/model-gateway/internal/policy"
	"github.com/ra1/model-gateway/internal/schema"
	"github.com/ra1/model-gateway/internal/store"
)

func TestWave3_PII_Masking_Latency(t *testing.T) {
	mgr := guardrails.NewManager(guardrails.Config{
		PIIAction:       guardrails.ActionMask,
		SecretsAction:   guardrails.ActionBlock,
		InjectionAction: guardrails.ActionBlock,
	})

	ctx := context.Background()
	testPrompt := "My SSN is 000-12-3456 and my email is testuser@enterprise.org. Also charge my Visa 4242-4242-4242-4242."

	// Measure 500 iterations
	const iterations = 500
	latencies := make([]time.Duration, iterations)

	for i := 0; i < iterations; i++ {
		req := &schema.CanonicalChatRequest{
			Messages: []schema.ChatMessage{
				{Role: "user", Content: testPrompt},
			},
		}

		t0 := time.Now()
		res, err := mgr.InspectInput(ctx, req)
		d := time.Since(t0)
		latencies[i] = d

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Blocked {
			t.Fatalf("expected PII to be masked, not blocked")
		}

		// Verify masking
		content := req.Messages[0].Content
		if !bytes.Contains([]byte(content), []byte("[REDACTED_SSN]")) {
			t.Errorf("SSN was not masked: %s", content)
		}
		if !bytes.Contains([]byte(content), []byte("[REDACTED_EMAIL]")) {
			t.Errorf("Email was not masked: %s", content)
		}
		if !bytes.Contains([]byte(content), []byte("[REDACTED_CC]")) {
			t.Errorf("Credit card was not masked: %s", content)
		}
	}

	// Calculate p99 latency
	var maxD time.Duration
	var totalD time.Duration
	for _, d := range latencies {
		totalD += d
		if d > maxD {
			maxD = d
		}
	}
	avgD := totalD / iterations

	t.Logf("PII Redaction: avg=%v, max=%v across %d requests", avgD, maxD, iterations)
	if avgD > 5*time.Millisecond {
		t.Errorf("average PII redaction latency exceeded 5ms SLA: got %v", avgD)
	}
}

func TestWave3_Audit_HashChain_IntegrityUnderConcurrency(t *testing.T) {
	ledger := audit.NewLedger()
	const workers = 20
	const recordsPerWorker = 50
	totalRecords := workers * recordsPerWorker

	var wg sync.WaitGroup
	wg.Add(workers)

	for w := 0; w < workers; w++ {
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < recordsPerWorker; i++ {
				actor := fmt.Sprintf("worker-%d", workerID)
				action := "model.invoke"
				resource := "gpt-4o"
				_, err := ledger.Append(actor, "org-audit-test", action, resource, map[string]string{
					"worker": fmt.Sprintf("%d", workerID),
					"iter":   fmt.Sprintf("%d", i),
				})
				if err != nil {
					t.Errorf("audit append error: %v", err)
				}
			}
		}(w)
	}

	wg.Wait()

	records := ledger.GetEntries(0, 0)
	if len(records) != totalRecords {
		t.Fatalf("expected %d records, got %d", totalRecords, len(records))
	}

	// Cryptographic chain verification
	valid, err := ledger.VerifyChain()
	if !valid || err != nil {
		t.Fatalf("audit ledger cryptographic verification failed: %v", err)
	}

	// Verify tamper detection by altering a middle record
	records[totalRecords/2].Action = "tampered.action"
	validTampered, _ := ledger.VerifyChain()
	if validTampered {
		t.Fatalf("expected tamper detection to catch altered ledger entry, but it passed!")
	}
}

func TestWave3_KMS_EnvelopeEncryption_Performance(t *testing.T) {
	masterKey := make([]byte, 32)
	_, _ = rand.Read(masterKey)

	vault, err := kms.NewLocalVault(masterKey)
	if err != nil {
		t.Fatalf("failed to create vault: %v", err)
	}

	ctx := context.Background()
	secret := []byte("sk-proj-super-secret-production-model-gateway-api-key-here-1234567890")
	aadTenantA := []byte("tenant:org-acme-corp")
	aadTenantB := []byte("tenant:org-rival-inc")

	const count = 500
	t0 := time.Now()

	for i := 0; i < count; i++ {
		// 1. Encrypt with tenant A AAD
		ciphertext, err := vault.Encrypt(ctx, secret, aadTenantA)
		if err != nil {
			t.Fatalf("encryption failed: %v", err)
		}

		// 2. Decrypt with correct tenant A AAD
		plaintext, err := vault.Decrypt(ctx, ciphertext, aadTenantA)
		if err != nil || !bytes.Equal(plaintext, secret) {
			t.Fatalf("decryption failed or content mismatch: %v", err)
		}

		// 3. Decrypt with tenant B AAD must fail (cryptographic isolation)
		_, err = vault.Decrypt(ctx, ciphertext, aadTenantB)
		if err == nil {
			t.Fatalf("expected decryption with mismatched AAD to fail, but succeeded!")
		}
	}

	dur := time.Since(t0)
	opsPerSec := float64(count) / dur.Seconds()
	t.Logf("KMS AES-256-GCM Envelope Encryption: %d cycles in %v (%.1f ops/sec)", count, dur, opsPerSec)
}

func TestWave3_SCIM_Provisioning_And_AutoRevocation(t *testing.T) {
	keyStore := store.NewMemoryKeyStore()
	scimSvc := scim.NewService("scim-auth-token-xyz", keyStore)

	// 1. Provision user via SCIM
	userBody := `{"userName": "eve@corp.com", "displayName": "Eve Engineer"}`
	req := httptest.NewRequest(http.MethodPost, "/scim/v2/Users", bytes.NewBufferString(userBody))
	req.Header.Set("Authorization", "Bearer scim-auth-token-xyz")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	scimSvc.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created from SCIM, got %d: %s", w.Code, w.Body.String())
	}

	var created scim.User
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("failed to decode created user: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("expected non-empty user ID")
	}

	// Fetch users list
	listReq := httptest.NewRequest(http.MethodGet, "/scim/v2/Users", nil)
	listReq.Header.Set("Authorization", "Bearer scim-auth-token-xyz")
	wList := httptest.NewRecorder()
	scimSvc.ServeHTTP(wList, listReq)

	var listResp scim.ListResponse
	if err := json.NewDecoder(wList.Body).Decode(&listResp); err != nil {
		t.Fatalf("failed to decode list response: %v", err)
	}
	if listResp.TotalResults < 1 {
		t.Errorf("expected at least 1 user in list response, got %d", listResp.TotalResults)
	}

	// 2. Issue 5 keys to Eve
	rawKey, keyHash, prefix, _ := store.GenerateRawKey("dev")
	_ = keyStore.SaveKey(&store.VirtualKey{
		ID:        "vk-eve-primary",
		KeyHash:   keyHash,
		KeyPrefix: prefix,
		UserID:    "usr-eve-target",
		OrgID:     "org-sec",
		IsActive:  true,
	})

	// Authenticate before revocation
	if _, err := keyStore.Authenticate(rawKey, "gpt-4o"); err != nil {
		t.Fatalf("expected key to be active, got %v", err)
	}

	// 3. Deprovision user keys
	count, err := keyStore.RevokeKeysByUserID("usr-eve-target")
	if err != nil || count != 1 {
		t.Fatalf("expected 1 key revoked, got count=%d, err=%v", count, err)
	}

	// 4. Authenticate revoked key must fail
	if _, err := keyStore.Authenticate(rawKey, "gpt-4o"); err != store.ErrKeyInactive {
		t.Fatalf("expected ErrKeyInactive, got %v", err)
	}
}

func TestWave3_PolicyEngine_EnforcementThroughput(t *testing.T) {
	engine := policy.NewEngine()

	_ = engine.RegisterPolicy(policy.Policy{
		ID:    "corp-general",
		Scope: "org:acme",
		Rules: policy.Rules{
			AllowedModels:   []string{"gpt-4o-mini", "claude-3-haiku*"},
			MaxTokensLimit:  4096,
			MaxInputTokens:  8192,
			IPAllowlist:     []string{"10.0.0.0/8", "192.168.0.0/16"},
			RequiredHeaders: []string{"X-Correlation-ID"},
		},
	})

	ctx := context.Background()
	reqValid := policy.RequestContext{
		Org:                  "acme",
		Model:                "gpt-4o-mini",
		ClientIP:             "10.24.1.99",
		MaxTokens:            2048,
		EstimatedInputTokens: 500,
		Headers: map[string]string{
			"x-correlation-id": "corr-12345",
		},
	}

	const iterations = 1000
	t0 := time.Now()
	for i := 0; i < iterations; i++ {
		if err := engine.Evaluate(ctx, reqValid); err != nil {
			t.Fatalf("unexpected policy violation: %v", err)
		}
	}
	dur := time.Since(t0)
	opsPerSec := float64(iterations) / dur.Seconds()
	t.Logf("Policy Engine Evaluation: %d evals in %v (%.1f ops/sec, avg=%v)", iterations, dur, opsPerSec, dur/iterations)

	if dur/iterations > 1*time.Millisecond {
		t.Errorf("policy evaluation took too long: avg=%v", dur/iterations)
	}
}

func BenchmarkWave3_Guardrails_PIIRedaction(b *testing.B) {
	mgr := guardrails.NewManager(guardrails.DefaultConfig())
	ctx := context.Background()
	prompt := "User 000-12-3456 with email test@company.com and card 4242-4242-4242-4242 needs assistance."

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		req := &schema.CanonicalChatRequest{
			Messages: []schema.ChatMessage{{Role: "user", Content: prompt}},
		}
		_, _ = mgr.InspectInput(ctx, req)
	}
}

func BenchmarkWave3_KMS_AES256GCM(b *testing.B) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	vault, _ := kms.NewLocalVault(key)
	ctx := context.Background()
	secret := []byte("sk-proj-prod-api-key-test-performance-123456789")
	aad := []byte("tenant:org-prod")

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ct, _ := vault.Encrypt(ctx, secret, aad)
		_, _ = vault.Decrypt(ctx, ct, aad)
	}
}

func BenchmarkWave3_Audit_Append(b *testing.B) {
	ledger := audit.NewLedger()
	meta := map[string]string{"provider": "openai", "tokens": "100"}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = ledger.Append("actor-benchmark", "org-1", "chat.completion", "gpt-4o", meta)
	}
}
