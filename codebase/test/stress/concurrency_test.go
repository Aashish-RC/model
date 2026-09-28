package stress

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ra1/model-gateway/internal/keypool"
	"github.com/ra1/model-gateway/internal/pricing"
	"github.com/ra1/model-gateway/internal/server"
	"github.com/ra1/model-gateway/internal/store"
	"github.com/ra1/model-gateway/internal/wallet"
	"github.com/ra1/model-gateway/test/mockprovider"
)

func TestConcurrency_WalletZeroBalanceReadOnlyBehavior(t *testing.T) {
	mock := mockprovider.NewServer()
	defer mock.Close()

	keyStore := store.NewMemoryKeyStore()
	keyPool := keypool.NewManager()
	pricingReg := pricing.NewRegistry()
	walletMgr := wallet.NewManager()

	// Set exact balance: $0.10 (100,000 micro-dollars)
	orgID := "org-concurrency-test"
	walletMgr.SetBalance(orgID, 100000)

	rawKey, keyHash, prefix, _ := store.GenerateRawKey("conc")
	_ = keyStore.SaveKey(&store.VirtualKey{
		ID:        "vk-conc",
		KeyHash:   keyHash,
		KeyPrefix: prefix,
		OrgID:     orgID,
		IsActive:  true,
	})

	keyPool.AddKey(&keypool.Key{
		ID:       "key-conc-mock",
		Provider: "openai",
		APIKey:   "sk-mock",
		BaseURL:  mock.URL + "/v1",
		PoolType: keypool.PoolShared,
		LimitRPM: 50000,
	})

	srv := server.NewServer(server.Config{
		KeyStore: keyStore,
		KeyPool:  keyPool,
		Pricing:  pricingReg,
		Wallet:   walletMgr,
	})

	// Fire 1,000 concurrent HTTP requests against the gateway
	concurrency := 1000
	var wg sync.WaitGroup
	var succeeded atomic.Int64
	var rejectedPaymentRequired atomic.Int64

	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func(id int) {
			defer wg.Done()
			reqBody := fmt.Sprintf(`{"model":"gpt-4o","messages":[{"role":"user","content":"req-%d"}]}`, id)
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(reqBody))
			req.Header.Set("Authorization", "Bearer "+rawKey)
			req.Header.Set("Content-Type", "application/json")

			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)

			if rec.Code == http.StatusOK {
				succeeded.Add(1)
			} else if rec.Code == http.StatusPaymentRequired {
				rejectedPaymentRequired.Add(1)
			}
		}(i)
	}

	wg.Wait()

	w, err := walletMgr.GetWallet(orgID)
	if err != nil {
		t.Fatalf("failed to get wallet: %v", err)
	}

	// CRITICAL INVARIANT: Balance must never be negative
	if w.BalanceMicro < 0 {
		t.Fatalf("CRITICAL FAILURE: Balance went negative: %d", w.BalanceMicro)
	}
	if w.ReservedMicro < 0 {
		t.Fatalf("CRITICAL FAILURE: Reserved balance went negative: %d", w.ReservedMicro)
	}

	// Verify zero-balance read-only behavior:
	// Non-generative read endpoint /v1/models succeeds
	reqModels := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	recModels := httptest.NewRecorder()
	srv.ServeHTTP(recModels, reqModels)
	if recModels.Code != http.StatusOK {
		t.Errorf("expected /v1/models read endpoint to succeed at zero balance, got %d", recModels.Code)
	}

	// New generation request must fail with 402 Payment Required once balance is 0
	if w.AvailableMicro() == 0 {
		reqGen := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"gpt-4o","messages":[{"role":"user","content":"must fail"}]}`))
		reqGen.Header.Set("Authorization", "Bearer "+rawKey)
		reqGen.Header.Set("Content-Type", "application/json")

		recGen := httptest.NewRecorder()
		srv.ServeHTTP(recGen, reqGen)
		if recGen.Code != http.StatusPaymentRequired {
			t.Errorf("expected 402 Payment Required when balance is zero, got %d", recGen.Code)
		}
	}
}
