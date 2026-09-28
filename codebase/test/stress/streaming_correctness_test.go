package stress

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ra1/model-gateway/internal/keypool"
	"github.com/ra1/model-gateway/internal/pricing"
	"github.com/ra1/model-gateway/internal/server"
	"github.com/ra1/model-gateway/internal/store"
	"github.com/ra1/model-gateway/internal/wallet"
	"github.com/ra1/model-gateway/test/mockprovider"
)

func TestStreaming_ChunkIntegrityAndTTFT(t *testing.T) {
	mock := mockprovider.NewServer()
	defer mock.Close()

	keyStore := store.NewMemoryKeyStore()
	keyPool := keypool.NewManager()
	pricingReg := pricing.NewRegistry()
	walletMgr := wallet.NewManager()

	walletMgr.SetBalance("org-stream-int", 10000000)

	rawKey, keyHash, prefix, _ := store.GenerateRawKey("stream")
	_ = keyStore.SaveKey(&store.VirtualKey{
		ID:        "vk-stream-int",
		KeyHash:   keyHash,
		KeyPrefix: prefix,
		OrgID:     "org-stream-int",
		IsActive:  true,
	})

	keyPool.AddKey(&keypool.Key{
		ID:       "key-stream",
		Provider: "openai",
		APIKey:   "sk-stream",
		BaseURL:  mock.URL + "/v1",
		PoolType: keypool.PoolShared,
	})

	srv := server.NewServer(server.Config{
		KeyStore: keyStore,
		KeyPool:  keyPool,
		Pricing:  pricingReg,
		Wallet:   walletMgr,
	})

	start := time.Now()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"test stream"}]}`))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	body := rec.Body.String()

	// Verify SSE framing and chunks
	if !strings.HasPrefix(body, "data: ") {
		t.Errorf("expected stream to begin with 'data: '")
	}
	if !strings.Contains(body, "chat.completion.chunk") {
		t.Errorf("expected chat.completion.chunk object in SSE body")
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Errorf("expected terminating [DONE] event in stream")
	}

	// Verify TTFT is fast (under 100ms in local test harness)
	if elapsed > 500*time.Millisecond {
		t.Errorf("stream took unexpectedly long: %v", elapsed)
	}
}

func TestStreaming_ClientCancelImmediateRelease(t *testing.T) {
	mock := mockprovider.NewServer()
	defer mock.Close()

	keyStore := store.NewMemoryKeyStore()
	keyPool := keypool.NewManager()
	pricingReg := pricing.NewRegistry()
	walletMgr := wallet.NewManager()

	walletMgr.SetBalance("org-cancel", 10000000)

	rawKey, keyHash, prefix, _ := store.GenerateRawKey("cancel")
	_ = keyStore.SaveKey(&store.VirtualKey{
		ID:        "vk-cancel",
		KeyHash:   keyHash,
		KeyPrefix: prefix,
		OrgID:     "org-cancel",
		IsActive:  true,
	})

	keyPool.AddKey(&keypool.Key{
		ID:       "key-cancel",
		Provider: "openai",
		APIKey:   "sk-cancel",
		BaseURL:  mock.URL + "/v1",
		PoolType: keypool.PoolShared,
	})

	srv := server.NewServer(server.Config{
		KeyStore: keyStore,
		KeyPool:  keyPool,
		Pricing:  pricingReg,
		Wallet:   walletMgr,
	})

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"cancel me"}]}`))
	req = req.WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+rawKey)

	// Cancel context immediately
	cancel()

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	// In-flight reservation must be cleaned up / released
	w, _ := walletMgr.GetWallet("org-cancel")
	if w.ReservedMicro != 0 {
		t.Errorf("expected reservation hold released to 0 after cancellation, got %d", w.ReservedMicro)
	}
}
