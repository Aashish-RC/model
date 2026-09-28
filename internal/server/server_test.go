package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ra1/model-gateway/internal/fallback"
	"github.com/ra1/model-gateway/internal/keypool"
	"github.com/ra1/model-gateway/internal/pricing"
	"github.com/ra1/model-gateway/internal/provider/openai"
	"github.com/ra1/model-gateway/internal/schema"
	"github.com/ra1/model-gateway/internal/store"
	"github.com/ra1/model-gateway/internal/wallet"
	"github.com/ra1/model-gateway/test/mockprovider"
)

func setupTestServer(mock *mockprovider.Server) (*Server, string) {
	keyStore := store.NewMemoryKeyStore()
	keyPool := keypool.NewManager()
	pricingReg := pricing.NewRegistry()
	walletMgr := wallet.NewManager()
	fallbackPolicy := fallback.NewPolicy()

	// Provision Org Wallet: $10.00
	walletMgr.SetBalance("org-test", 10000000)

	// Provision Virtual Key
	rawKey, keyHash, prefix, _ := store.GenerateRawKey("dev")
	_ = keyStore.SaveKey(&store.VirtualKey{
		ID:        "vk-test",
		KeyHash:   keyHash,
		KeyPrefix: prefix,
		Name:      "Test Key",
		OrgID:     "org-test",
		IsActive:  true,
	})

	// Add mock key to KeyPool
	keyPool.AddKey(&keypool.Key{
		ID:       "mock-openai-key",
		Provider: "openai",
		APIKey:   "sk-mock-val",
		BaseURL:  mock.URL + "/v1",
		PoolType: keypool.PoolShared,
		LimitRPM: 1000,
	})

	// Add mock key for Anthropic
	keyPool.AddKey(&keypool.Key{
		ID:       "mock-anthropic-key",
		Provider: "anthropic",
		APIKey:   "sk-mock-ant",
		BaseURL:  mock.URL + "/v1",
		PoolType: keypool.PoolShared,
		LimitRPM: 1000,
	})

	srv := NewServer(Config{
		KeyStore: keyStore,
		KeyPool:  keyPool,
		Pricing:  pricingReg,
		Wallet:   walletMgr,
		Fallback: fallbackPolicy,
	})

	// Override adapter clients to point to mock server
	srv.Adapters["openai"] = openai.NewAdapter(http.DefaultClient)

	return srv, rawKey
}

func TestServer_HealthAndModels(t *testing.T) {
	mock := mockprovider.NewServer()
	defer mock.Close()

	srv, _ := setupTestServer(mock)

	// 1. Health
	reqHealth := httptest.NewRequest(http.MethodGet, "/health", nil)
	recHealth := httptest.NewRecorder()
	srv.ServeHTTP(recHealth, reqHealth)

	if recHealth.Code != http.StatusOK {
		t.Errorf("expected 200 on /health, got %d", recHealth.Code)
	}

	// 2. Models
	reqModels := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	recModels := httptest.NewRecorder()
	srv.ServeHTTP(recModels, reqModels)

	if recModels.Code != http.StatusOK {
		t.Errorf("expected 200 on /v1/models, got %d", recModels.Code)
	}
}

func TestServer_ChatCompletions_EndToEnd(t *testing.T) {
	mock := mockprovider.NewServer()
	defer mock.Close()

	srv, rawKey := setupTestServer(mock)

	reqPayload := `{"model":"gpt-4o","messages":[{"role":"user","content":"Hello world"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(reqPayload))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d with body: %s", rec.Code, rec.Body.String())
	}

	var chatResp schema.CanonicalChatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &chatResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if chatResp.Model != "gpt-4o" {
		t.Errorf("expected model gpt-4o, got %s", chatResp.Model)
	}

	// Verify wallet was settled
	w, _ := srv.Wallet.GetWallet("org-test")
	if w.BalanceMicro >= 10000000 {
		t.Errorf("expected balance to decrease after settlement, got %d", w.BalanceMicro)
	}
	if w.ReservedMicro != 0 {
		t.Errorf("expected reservation hold released to 0, got %d", w.ReservedMicro)
	}
}

func TestServer_ChatCompletions_Streaming(t *testing.T) {
	mock := mockprovider.NewServer()
	defer mock.Close()

	srv, rawKey := setupTestServer(mock)

	reqPayload := `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"Stream test"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(reqPayload))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for streaming, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "data: [DONE]") {
		t.Errorf("expected [DONE] in SSE output")
	}
}

func TestServer_ChatCompletions_Fallback(t *testing.T) {
	mock := mockprovider.NewServer()
	defer mock.Close()

	srv, rawKey := setupTestServer(mock)

	// Configure fallback: gpt-4o -> claude-3-5-sonnet
	srv.Fallback.SetChain("gpt-4o", []string{"claude-3-5-sonnet"})

	// Put all gpt-4o keys in cooldown
	k, _ := srv.KeyPool.SelectKey("openai", "org-test", false)
	k.MarkFailure(srv.KeyPool.BaseCooldown, srv.KeyPool.MaxCooldown)

	reqPayload := `{"model":"gpt-4o","messages":[{"role":"user","content":"Hello fallback"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(reqPayload))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 after fallback, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify downgrade headers
	if rec.Header().Get("X-Model-Downgraded") != "true" {
		t.Errorf("expected X-Model-Downgraded: true header")
	}
	if rec.Header().Get("X-Effective-Model") != "claude-3-5-sonnet" {
		t.Errorf("expected X-Effective-Model claude-3-5-sonnet, got %s", rec.Header().Get("X-Effective-Model"))
	}
}
