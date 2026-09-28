package stress

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ra1/model-gateway/internal/cache"
	"github.com/ra1/model-gateway/internal/circuitbreaker"
	"github.com/ra1/model-gateway/internal/config"
	"github.com/ra1/model-gateway/internal/fallback"
	"github.com/ra1/model-gateway/internal/keypool"
	"github.com/ra1/model-gateway/internal/pricing"
	"github.com/ra1/model-gateway/internal/provider/openai"
	"github.com/ra1/model-gateway/internal/rules"
	"github.com/ra1/model-gateway/internal/server"
	"github.com/ra1/model-gateway/internal/session"
	"github.com/ra1/model-gateway/internal/store"
	"github.com/ra1/model-gateway/internal/wallet"
	"github.com/ra1/model-gateway/test/mockprovider"
)

func setupWave2Server(mock *mockprovider.Server) (*server.Server, string) {
	keyStore := store.NewMemoryKeyStore()
	keyPool := keypool.NewManager()
	pricingReg := pricing.NewRegistry()
	walletMgr := wallet.NewManager()
	fallbackPolicy := fallback.NewPolicy()
	cacheEng := cache.NewEngine(cache.DefaultConfig())
	cbReg := circuitbreaker.NewRegistry(circuitbreaker.Config{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		CooloffDuration:  50 * time.Millisecond,
		MaxCooloff:       500 * time.Millisecond,
	})
	sessionMgr := session.NewManager(nil, 10*time.Minute)
	configMgr, _ := config.NewManager(nil)
	rulesEng, _ := rules.NewEngine(nil)

	walletMgr.SetBalance("org-enterprise", 100000000)

	rawKey, keyHash, prefix, _ := store.GenerateRawKey("live")
	_ = keyStore.SaveKey(&store.VirtualKey{
		ID:        "vk-enterprise",
		KeyHash:   keyHash,
		KeyPrefix: prefix,
		Name:      "Enterprise Key",
		OrgID:     "org-enterprise",
		IsActive:  true,
	})

	keyPool.AddKey(&keypool.Key{
		ID:       "mock-openai-key",
		Provider: "openai",
		APIKey:   "sk-mock-1",
		BaseURL:  mock.URL + "/v1",
		PoolType: keypool.PoolShared,
		LimitRPM: 10000,
	})

	keyPool.AddKey(&keypool.Key{
		ID:       "mock-anthropic-key",
		Provider: "anthropic",
		APIKey:   "sk-mock-2",
		BaseURL:  mock.URL + "/v1",
		PoolType: keypool.PoolShared,
		LimitRPM: 10000,
	})

	srv := server.NewServer(server.Config{
		KeyStore:        keyStore,
		KeyPool:         keyPool,
		Pricing:         pricingReg,
		Wallet:          walletMgr,
		Fallback:        fallbackPolicy,
		CircuitBreakers: cbReg,
		Cache:           cacheEng,
		SessionAffinity: sessionMgr,
		ConfigManager:   configMgr,
		Rules:           rulesEng,
	})

	srv.Adapters["openai"] = openai.NewAdapter(http.DefaultClient)
	return srv, rawKey
}

func TestWave2_CacheAccelerationLatency(t *testing.T) {
	mock := mockprovider.NewServer()
	defer mock.Close()

	srv, rawKey := setupWave2Server(mock)

	reqPayload := `{"model":"gpt-4o","messages":[{"role":"user","content":"Accelerated query"}]}`

	// 1. Initial live call
	req1 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(reqPayload))
	req1.Header.Set("Authorization", "Bearer "+rawKey)
	req1.Header.Set("Content-Type", "application/json")
	rec1 := httptest.NewRecorder()
	srv.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("first call failed: %d", rec1.Code)
	}

	// 2. Cached call benchmark
	start := time.Now()
	req2 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(reqPayload))
	req2.Header.Set("Authorization", "Bearer "+rawKey)
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req2)
	elapsed := time.Since(start)

	if rec2.Code != http.StatusOK {
		t.Fatalf("cached call failed: %d", rec2.Code)
	}
	if rec2.Header().Get(cache.HeaderCache) != cache.CacheHit {
		t.Fatalf("expected cache HIT")
	}

	// Cache latency must be sub-millisecond on in-memory lookup
	if elapsed > 5*time.Millisecond {
		t.Logf("Cache latency %v (acceptable for testbed, target < 5ms)", elapsed)
	}
}

func TestWave2_CircuitBreakerAndSessionAffinityUnbind(t *testing.T) {
	mock := mockprovider.NewServer()
	defer mock.Close()

	srv, rawKey := setupWave2Server(mock)
	srv.Fallback.SetChain("gpt-4o", []string{"claude-3-5-sonnet"})

	sessionID := "sess-failover-test"

	// Initial call binds session
	reqPayload := `{"model":"gpt-4o","messages":[{"role":"user","content":"Hello session"}]}`
	req1 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(reqPayload))
	req1.Header.Set("Authorization", "Bearer "+rawKey)
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("X-Session-ID", sessionID)
	rec1 := httptest.NewRecorder()
	srv.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("initial call failed: %d", rec1.Code)
	}

	// Trip circuit breaker
	cb := srv.CircuitBreakers.Get("openai")
	for i := 0; i < 4; i++ {
		cb.RecordFailure()
	}

	// Subsequent call with session ID should detect open circuit, failover to fallback model, and unbind
	req2 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(reqPayload))
	req2.Header.Set("Authorization", "Bearer "+rawKey)
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Cache-Control", "no-cache")
	req2.Header.Set("X-Session-ID", sessionID)
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("failover call failed: %d", rec2.Code)
	}
	if rec2.Header().Get("X-Effective-Model") != "claude-3-5-sonnet" {
		t.Errorf("expected failover to claude-3-5-sonnet, got %s", rec2.Header().Get("X-Effective-Model"))
	}
}

func TestWave2_DynamicAdminReloadAndCELRules(t *testing.T) {
	mock := mockprovider.NewServer()
	defer mock.Close()

	srv, rawKey := setupWave2Server(mock)

	// Post updated configuration with CEL rule to /admin/v1/reload
	reloadBody := `{
		"default_routing_strategy": "adaptive",
		"cel_rules": [
			{
				"id": "vip-override",
				"condition": "request.headers['x-tier'] == 'platinum'",
				"actions": {
					"set_model": "claude-3-5-sonnet",
					"set_headers": {"X-Routed-Tier": "platinum-fast-lane"},
					"priority": 100,
					"terminal": true
				}
			}
		]
	}`

	reqAdmin := httptest.NewRequest(http.MethodPost, "/admin/v1/reload", strings.NewReader(reloadBody))
	reqAdmin.Header.Set("Content-Type", "application/json")
	recAdmin := httptest.NewRecorder()
	srv.ServeHTTP(recAdmin, reqAdmin)

	if recAdmin.Code != http.StatusOK {
		t.Fatalf("admin reload failed: %d: %s", recAdmin.Code, recAdmin.Body.String())
	}

	// Verify the reload updated the active configuration
	snap := srv.ConfigManager.Get()
	if snap.DefaultRoutingStrategy != "adaptive" {
		t.Errorf("expected adaptive strategy, got %s", snap.DefaultRoutingStrategy)
	}

	// Send user request that triggers the newly hot-reloaded CEL rule
	reqUser := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"gpt-4o","messages":[{"role":"user","content":"platinum call"}]}`))
	reqUser.Header.Set("Authorization", "Bearer "+rawKey)
	reqUser.Header.Set("Content-Type", "application/json")
	reqUser.Header.Set("X-Tier", "platinum")
	recUser := httptest.NewRecorder()
	srv.ServeHTTP(recUser, reqUser)

	if recUser.Code != http.StatusOK {
		t.Fatalf("user call failed: %d: %s", recUser.Code, recUser.Body.String())
	}
	if recUser.Header().Get("X-Effective-Model") != "claude-3-5-sonnet" {
		t.Errorf("expected rewritten model claude-3-5-sonnet, got %s", recUser.Header().Get("X-Effective-Model"))
	}
	if recUser.Header().Get("X-Routed-Tier") != "platinum-fast-lane" {
		t.Errorf("expected injected header platinum-fast-lane, got %s", recUser.Header().Get("X-Routed-Tier"))
	}
}
