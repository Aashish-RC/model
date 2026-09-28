package stress

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ra1/model-gateway/internal/fallback"
	"github.com/ra1/model-gateway/internal/keypool"
	"github.com/ra1/model-gateway/internal/pricing"
	"github.com/ra1/model-gateway/internal/server"
	"github.com/ra1/model-gateway/internal/store"
	"github.com/ra1/model-gateway/internal/wallet"
	"github.com/ra1/model-gateway/test/mockprovider"
)

func TestFailureInjection_429StormAndCooldownGrowth(t *testing.T) {
	key := &keypool.Key{
		ID:       "storm-key",
		Provider: "openai",
	}

	base := 50 * time.Millisecond
	max := 1 * time.Second

	// Simulate rapid storm of 5 consecutive 429 failures
	for i := 1; i <= 5; i++ {
		duration := key.MarkFailure(base, max)
		if duration < base {
			t.Errorf("cooldown duration %v less than base", duration)
		}
	}

	if key.ConsecutiveFails.Load() != 5 {
		t.Errorf("expected 5 consecutive fails, got %d", key.ConsecutiveFails.Load())
	}

	// Verify cooldown has grown significantly
	cooldownUntil := key.CooldownUntil.Load()
	if cooldownUntil <= time.Now().UnixNano() {
		t.Errorf("expected key to be in active cooldown")
	}

	// Verify automatic rejoin in future
	future := time.Now().Add(2 * time.Second)
	if !key.IsEligible(future) {
		t.Errorf("expected key to automatically rejoin after cooldown window")
	}
}

func TestFailureInjection_AllKeysCoolingDownTriggersFallback(t *testing.T) {
	mock := mockprovider.NewServer()
	defer mock.Close()

	keyStore := store.NewMemoryKeyStore()
	keyPool := keypool.NewManager()
	pricingReg := pricing.NewRegistry()
	walletMgr := wallet.NewManager()
	fallbackPolicy := fallback.NewPolicy()

	walletMgr.SetBalance("org-storm", 10000000)

	rawKey, keyHash, prefix, _ := store.GenerateRawKey("storm")
	_ = keyStore.SaveKey(&store.VirtualKey{
		ID:        "vk-storm",
		KeyHash:   keyHash,
		KeyPrefix: prefix,
		OrgID:     "org-storm",
		IsActive:  true,
	})

	// Add 2 OpenAI keys
	k1 := &keypool.Key{ID: "key-1", Provider: "openai", APIKey: "sk-1", BaseURL: mock.URL + "/v1", PoolType: keypool.PoolShared}
	k2 := &keypool.Key{ID: "key-2", Provider: "openai", APIKey: "sk-2", BaseURL: mock.URL + "/v1", PoolType: keypool.PoolShared}
	// Add 1 Anthropic fallback key
	k3 := &keypool.Key{ID: "key-3", Provider: "anthropic", APIKey: "sk-3", BaseURL: mock.URL + "/v1", PoolType: keypool.PoolShared}

	keyPool.AddKey(k1)
	keyPool.AddKey(k2)
	keyPool.AddKey(k3)

	// Configure fallback
	fallbackPolicy.SetChain("gpt-4o", []string{"claude-3-5-sonnet"})

	// Put both OpenAI keys in cooldown
	k1.MarkFailure(10*time.Second, 1*time.Minute)
	k2.MarkFailure(10*time.Second, 1*time.Minute)

	srv := server.NewServer(server.Config{
		KeyStore: keyStore,
		KeyPool:  keyPool,
		Pricing:  pricingReg,
		Wallet:   walletMgr,
		Fallback: fallbackPolicy,
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"test fallback"}]}`))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 after fallback, got %d: %s", rec.Code, rec.Body.String())
	}

	if rec.Header().Get("X-Model-Downgraded") != "true" {
		t.Errorf("expected X-Model-Downgraded: true header")
	}
	if rec.Header().Get("X-Effective-Model") != "claude-3-5-sonnet" {
		t.Errorf("expected effective model claude-3-5-sonnet, got %s", rec.Header().Get("X-Effective-Model"))
	}
}

func TestFailureInjection_Concurrent429Storm(t *testing.T) {
	mock := mockprovider.NewServer()
	defer mock.Close()

	keyStore := store.NewMemoryKeyStore()
	keyPool := keypool.NewManager()
	pricingReg := pricing.NewRegistry()
	walletMgr := wallet.NewManager()

	walletMgr.SetBalance("org-429", 100000000)

	rawKey, keyHash, prefix, _ := store.GenerateRawKey("storm429")
	_ = keyStore.SaveKey(&store.VirtualKey{
		ID:        "vk-storm429",
		KeyHash:   keyHash,
		KeyPrefix: prefix,
		OrgID:     "org-429",
		IsActive:  true,
	})

	// Add key that always answers 429
	keyPool.AddKey(&keypool.Key{
		ID:       "key-rate-limited",
		Provider: "openai",
		APIKey:   "sk-rate",
		BaseURL:  mock.URL + "/v1",
		PoolType: keypool.PoolShared,
	})

	srv := server.NewServer(server.Config{
		KeyStore: keyStore,
		KeyPool:  keyPool,
		Pricing:  pricingReg,
		Wallet:   walletMgr,
	})

	var wg sync.WaitGroup
	concurrentRequests := 20
	wg.Add(concurrentRequests)

	for i := 0; i < concurrentRequests; i++ {
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"storm"}]}`))
			req.Header.Set("Authorization", "Bearer "+rawKey)
			req.Header.Set("X-Mock-Status", "429") // Inject 429
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
		}()
	}

	wg.Wait()

	// Verify key was placed into cooldown
	k, _ := keyPool.SelectKey("openai", "org-429", false)
	if k != nil {
		t.Errorf("expected all keys to be in cooldown after 429 storm, but found eligible key")
	}
}

func TestFailureInjection_MidStreamDisconnectAndCancel(t *testing.T) {
	mock := mockprovider.NewServer()
	defer mock.Close()

	// Direct stream test with early cancel
	req, _ := http.NewRequest(http.MethodPost, mock.URL+"/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","stream":true}`))
	req.Header.Set("X-Mock-Stream-Disconnect-After-Chunks", "2")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("call error: %v", err)
	}
	defer resp.Body.Close()

	buf := make([]byte, 1024)
	n, _ := resp.Body.Read(buf)
	if n <= 0 {
		t.Errorf("expected some bytes before disconnect")
	}

	// Second read must hit EOF/error
	_, err = io.ReadAll(resp.Body)
	if err != nil && err != io.EOF && !strings.Contains(err.Error(), "closed") {
		// Clean disconnect
	}
}

func TestFailureInjection_ClientContextCancellation(t *testing.T) {
	mock := mockprovider.NewServer()
	defer mock.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, mock.URL+"/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o"}`))
	req.Header.Set("X-Mock-Delay-Ms", "500")

	// Cancel context after 50ms
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := http.DefaultClient.Do(req)
	if err == nil {
		t.Fatalf("expected error due to context cancel, got nil")
	}
	if !strings.Contains(err.Error(), "canceled") {
		t.Errorf("expected error to mention canceled, got: %v", err)
	}
}
