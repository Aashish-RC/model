package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ra1/model-gateway/internal/cache"
	"github.com/ra1/model-gateway/internal/circuitbreaker"
	"github.com/ra1/model-gateway/internal/config"
	"github.com/ra1/model-gateway/internal/fallback"
	"github.com/ra1/model-gateway/internal/keypool"
	"github.com/ra1/model-gateway/internal/metrics"
	"github.com/ra1/model-gateway/internal/pricing"
	"github.com/ra1/model-gateway/internal/provider"
	"github.com/ra1/model-gateway/internal/provider/anthropic"
	"github.com/ra1/model-gateway/internal/provider/gemini"
	"github.com/ra1/model-gateway/internal/provider/generic"
	"github.com/ra1/model-gateway/internal/provider/openai"
	"github.com/ra1/model-gateway/internal/router"
	"github.com/ra1/model-gateway/internal/routing"
	"github.com/ra1/model-gateway/internal/rules"
	"github.com/ra1/model-gateway/internal/schema"
	"github.com/ra1/model-gateway/internal/session"
	"github.com/ra1/model-gateway/internal/store"
	"github.com/ra1/model-gateway/internal/stream"
	"github.com/ra1/model-gateway/internal/wallet"
)

// Server coordinates the entire gateway data plane.
type Server struct {
	KeyStore        *store.MemoryKeyStore
	KeyPool         *keypool.Manager
	Pricing         *pricing.Registry
	Wallet          *wallet.Manager
	Fallback        *fallback.Policy
	Metrics         *metrics.Metrics
	StreamHandler   *stream.Handler
	Adapters        map[string]provider.Adapter
	CircuitBreakers *circuitbreaker.Registry
	Cache           *cache.Engine
	SessionAffinity *session.Manager
	Rules           *rules.Engine
	ConfigManager   *config.Manager
	Strategies      map[routing.StrategyType]routing.Strategy
	mux             *http.ServeMux
	DefaultProvider string
}

// Config provides initial server dependencies.
type Config struct {
	KeyStore        *store.MemoryKeyStore
	KeyPool         *keypool.Manager
	Pricing         *pricing.Registry
	Wallet          *wallet.Manager
	Fallback        *fallback.Policy
	Metrics         *metrics.Metrics
	CircuitBreakers *circuitbreaker.Registry
	Cache           *cache.Engine
	SessionAffinity *session.Manager
	Rules           *rules.Engine
	ConfigManager   *config.Manager
}

// NewServer initializes a fully-wired gateway server.
func NewServer(cfg Config) *Server {
	if cfg.KeyStore == nil {
		cfg.KeyStore = store.NewMemoryKeyStore()
	}
	if cfg.KeyPool == nil {
		cfg.KeyPool = keypool.NewManager()
	}
	if cfg.Pricing == nil {
		cfg.Pricing = pricing.NewRegistry()
	}
	if cfg.Wallet == nil {
		cfg.Wallet = wallet.NewManager()
	}
	if cfg.Fallback == nil {
		cfg.Fallback = fallback.NewPolicy()
	}
	if cfg.Metrics == nil {
		cfg.Metrics = metrics.NewMetrics()
	}
	if cfg.CircuitBreakers == nil {
		cfg.CircuitBreakers = circuitbreaker.NewRegistry(circuitbreaker.DefaultConfig())
	}
	if cfg.Cache == nil {
		cfg.Cache = cache.NewEngine(cache.DefaultConfig())
	}
	if cfg.SessionAffinity == nil {
		cfg.SessionAffinity = session.NewManager(nil, 30*time.Minute)
	}
	if cfg.ConfigManager == nil {
		cfg.ConfigManager, _ = config.NewManager(nil)
	}
	if cfg.Rules == nil {
		cfg.Rules, _ = rules.NewEngine(nil)
	}

	strats := map[routing.StrategyType]routing.Strategy{
		routing.StrategyLowestLatency: &routing.LowestLatencyStrategy{},
		routing.StrategyLowestCost:    &routing.LowestCostStrategy{},
		routing.StrategyLeastBusy:     &routing.LeastBusyStrategy{},
		routing.StrategyHeadroom:      &routing.HeadroomStrategy{},
		routing.StrategyAdaptive:      routing.NewAdaptiveStrategy(routing.DefaultWeights(), 0.05),
	}

	s := &Server{
		KeyStore:        cfg.KeyStore,
		KeyPool:         cfg.KeyPool,
		Pricing:         cfg.Pricing,
		Wallet:          cfg.Wallet,
		Fallback:        cfg.Fallback,
		Metrics:         cfg.Metrics,
		StreamHandler:   stream.NewHandler(),
		CircuitBreakers: cfg.CircuitBreakers,
		Cache:           cfg.Cache,
		SessionAffinity: cfg.SessionAffinity,
		Rules:           cfg.Rules,
		ConfigManager:   cfg.ConfigManager,
		Strategies:      strats,
		Adapters:        make(map[string]provider.Adapter),
		mux:             http.NewServeMux(),
	}

	// Register default provider adapters
	httpClient := http.DefaultClient
	s.Adapters["openai"] = openai.NewAdapter(httpClient)
	s.Adapters["anthropic"] = anthropic.NewAdapter(httpClient)
	s.Adapters["gemini"] = gemini.NewAdapter(httpClient)
	s.Adapters["generic"] = generic.NewAdapter(httpClient)

	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	s.mux.HandleFunc("/admin/v1/reload", s.handleAdminReload)
	s.mux.HandleFunc("/v1/chat/completions", s.handleChatCompletions)
	s.mux.HandleFunc("/v1/models", s.handleModels)
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/healthz", s.handleHealth)
	s.mux.HandleFunc("/ready", s.handleReady)
	s.mux.Handle("/metrics", s.Metrics.Handler())
}

func (s *Server) handleAdminReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var newCfg config.RuntimeConfig
	if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "Invalid configuration JSON", "invalid_config", "bad_json")
		return
	}
	if err := s.ConfigManager.Update(&newCfg); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, err.Error(), "invalid_config", "validation_failed")
		return
	}
	if len(newCfg.CELRules) > 0 {
		_ = s.Rules.UpdateRules(newCfg.CELRules)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":   "reloaded",
		"revision": s.ConfigManager.Get().Revision,
	})
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) writeJSONError(w http.ResponseWriter, status int, msg, errType, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(schema.ErrorResponse{
		Error: schema.APIError{
			Message: msg,
			Type:    errType,
			Code:    code,
		},
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"healthy"}`))
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ready"}`))
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{
		"object": "list",
		"data": [
			{"id": "gpt-4o", "object": "model", "owned_by": "openai"},
			{"id": "gpt-4o-mini", "object": "model", "owned_by": "openai"},
			{"id": "claude-3-5-sonnet", "object": "model", "owned_by": "anthropic"},
			{"id": "gemini-1.5-pro", "object": "model", "owned_by": "google"}
		]
	}`))
}

func (s *Server) resolveAdapter(model string) (string, provider.Adapter) {
	for name, adapter := range s.Adapters {
		if adapter.SupportsModel(model) {
			return name, adapter
		}
	}
	// Fall back to OpenAI or Generic adapter
	if strings.Contains(model, "claude") {
		return "anthropic", s.Adapters["anthropic"]
	}
	if strings.Contains(model, "gemini") {
		return "gemini", s.Adapters["gemini"]
	}
	return "openai", s.Adapters["openai"]
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// 1. Authenticate Virtual Key
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		s.writeJSONError(w, http.StatusUnauthorized, "Missing or invalid bearer token", "auth_error", "invalid_api_key")
		return
	}
	rawKey := strings.TrimPrefix(authHeader, "Bearer ")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "Failed to read request body", "invalid_request_error", "invalid_body")
		return
	}

	var chatReq schema.CanonicalChatRequest
	if err := json.Unmarshal(body, &chatReq); err != nil {
		s.writeJSONError(w, http.StatusBadRequest, "Invalid JSON payload", "invalid_request_error", "invalid_json")
		return
	}

	vKey, err := s.KeyStore.Authenticate(rawKey, chatReq.RequestedModel)
	if err != nil {
		if err == store.ErrModelNotAllowed {
			s.writeJSONError(w, http.StatusForbidden, err.Error(), "permission_error", "model_not_allowed")
			return
		}
		s.writeJSONError(w, http.StatusUnauthorized, err.Error(), "auth_error", "unauthorized")
		return
	}

	chatReq.VirtualKeyID = vKey.ID
	chatReq.OrgID = vKey.OrgID
	chatReq.TeamID = vKey.TeamID
	if chatReq.ID == "" {
		chatReq.ID = fmt.Sprintf("req-%d", time.Now().UnixNano())
	}

	// Cache Check (non-streaming requests with no bypass)
	bypassCache := r.Header.Get("Cache-Control") == "no-cache" || r.Header.Get("X-Gateway-Cache-Bypass") == "true"
	var exactCacheKey string
	if !chatReq.Stream && !bypassCache {
		exactCacheKey = cache.HashRequest(vKey.OrgID, &chatReq)
		if cached, found := s.Cache.GetExact(exactCacheKey); found && cached != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set(cache.HeaderCache, cache.CacheHit)
			w.Header().Set(cache.HeaderCacheType, cache.TypeExact)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(cached)
			s.Metrics.RequestsTotal.WithLabelValues(chatReq.RequestedModel, "cache", "200").Inc()
			s.Metrics.RequestDuration.WithLabelValues(chatReq.RequestedModel, "cache", "false").Observe(time.Since(start).Seconds())
			return
		}
	}

	// CEL Rules Evaluation
	evalCtx := &rules.EvaluationContext{
		Model:           chatReq.RequestedModel,
		Stream:          chatReq.Stream,
		EstimatedTokens: 10,
		Headers:         make(map[string]string),
		User:            chatReq.User,
		KeyID:           vKey.ID,
		OrgID:           vKey.OrgID,
		TeamID:          vKey.TeamID,
	}
	for k, vals := range r.Header {
		if len(vals) > 0 {
			evalCtx.Headers[strings.ToLower(k)] = vals[0]
		}
	}
	if ruleRes, rErr := s.Rules.Evaluate(r.Context(), evalCtx); rErr == nil && ruleRes != nil {
		if ruleRes.RewrittenModel != "" {
			chatReq.RequestedModel = ruleRes.RewrittenModel
		}
		for hk, hv := range ruleRes.InjectedHeaders {
			w.Header().Set(hk, hv)
		}
	}

	// Session Affinity Resolution
	sessionID := r.Header.Get("X-Session-ID")
	if sessionID == "" {
		sessionID = r.Header.Get("X-Thread-ID")
	}
	if sessionID == "" {
		sessionID = chatReq.User
	}

	// 2. Two-Phase Reservation
	maxTokens := chatReq.GetEffectiveMaxTokens()
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	// Estimate input tokens heuristic (~4 chars per token)
	var inputChars int
	for _, m := range chatReq.Messages {
		inputChars += len(m.Content)
	}
	inputEstTokens := inputChars / 4
	if inputEstTokens <= 0 {
		inputEstTokens = 10
	}

	estHoldMicro := s.Pricing.CalculateCostMicro(chatReq.RequestedModel, inputEstTokens, maxTokens, 0)
	if estHoldMicro <= 0 {
		estHoldMicro = 1000 // minimum $0.001 hold
	}

	if err := s.Wallet.Reserve(vKey.OrgID, estHoldMicro); err != nil {
		s.writeJSONError(w, http.StatusPaymentRequired, err.Error(), "billing_error", "insufficient_balance")
		return
	}

	// 3. Execution with Fallback and Key Rotation Loop
	currentModel := chatReq.RequestedModel
	visitedModels := map[string]bool{currentModel: true}
	var lastErr error
	retryPolicy := router.DefaultRetryPolicy()

	for {
		providerName, adapter := s.resolveAdapter(currentModel)
		chatReq.EffectiveModel = currentModel

		if !s.CircuitBreakers.Get(providerName).Allow() {
			nextModel, fErr := s.Fallback.GetNextModel(chatReq.RequestedModel, visitedModels)
			if fErr == nil {
				s.Metrics.ModelFallbacksTotal.WithLabelValues(chatReq.RequestedModel, nextModel).Inc()
				currentModel = nextModel
				visitedModels[currentModel] = true
				continue
			}
			lastErr = circuitbreaker.ErrCircuitOpen
			break
		}

		// Select key from pool
		key, err := s.KeyPool.SelectKey(providerName, vKey.OrgID, false)
		if err != nil {
			// All keys for this provider are exhausted or cooling down: try model fallback
			nextModel, fErr := s.Fallback.GetNextModel(chatReq.RequestedModel, visitedModels)
			if fErr == nil {
				s.Metrics.ModelFallbacksTotal.WithLabelValues(chatReq.RequestedModel, nextModel).Inc()
				currentModel = nextModel
				visitedModels[currentModel] = true
				continue
			}
			lastErr = err
			break
		}

		cred := key.ToCredential()
		if cred.Headers == nil {
			cred.Headers = make(map[string]string)
		}
		for k, vals := range r.Header {
			if strings.HasPrefix(strings.ToLower(k), "x-mock-") && len(vals) > 0 {
				cred.Headers[k] = vals[0]
			}
		}

		if chatReq.Stream {
			it, sErr := adapter.ExecuteStream(r.Context(), &chatReq, cred)
			if sErr != nil {
				s.CircuitBreakers.Get(providerName).RecordFailure()
				if sessionID != "" {
					_ = s.SessionAffinity.Unbind(r.Context(), vKey.OrgID, sessionID)
				}
				decision := router.ClassifyError(sErr)
				if decision.RotateKey {
					key.MarkFailure(s.KeyPool.BaseCooldown, s.KeyPool.MaxCooldown)
					s.Metrics.KeyRotationsTotal.WithLabelValues(providerName, "rate_limited").Inc()
					continue
				}
				lastErr = sErr
				break
			}

			// Forward Stream to response
			accum := stream.NewAccumulator(inputEstTokens)
			_ = s.StreamHandler.ForwardStream(r.Context(), w, it, accum)
			key.MarkSuccess()
			s.CircuitBreakers.Get(providerName).RecordSuccess()
			if sessionID != "" {
				_ = s.SessionAffinity.Bind(r.Context(), vKey.OrgID, sessionID, providerName, key.ID, currentModel, 30*time.Minute)
			}

			// Post-call Settlement
			usage := accum.Finalize()
			actualCost := s.Pricing.CalculateCostMicro(currentModel, usage.PromptTokens, usage.CompletionTokens, 0)
			_ = s.Wallet.Settle(vKey.OrgID, estHoldMicro, actualCost, chatReq.ID, vKey.ID, "chat stream completion")

			s.Metrics.TokensTotal.WithLabelValues(currentModel, providerName, "prompt").Add(float64(usage.PromptTokens))
			s.Metrics.TokensTotal.WithLabelValues(currentModel, providerName, "completion").Add(float64(usage.CompletionTokens))
			s.Metrics.RequestsTotal.WithLabelValues(currentModel, providerName, "200").Inc()
			s.Metrics.RequestDuration.WithLabelValues(currentModel, providerName, "true").Observe(time.Since(start).Seconds())
			if accum.FirstTokenTime > 0 {
				s.Metrics.UpstreamTTFT.WithLabelValues(currentModel, providerName).Observe(accum.FirstTokenTime.Seconds())
			}
			return
		}

		// Synchronous Execution with same-key retry
		var chatResp *schema.CanonicalChatResponse
		var execErr error

		for attempt := 0; attempt <= retryPolicy.MaxSameKeyRetries; attempt++ {
			chatResp, execErr = adapter.Execute(r.Context(), &chatReq, cred)
			if execErr == nil {
				break
			}

			decision := router.ClassifyError(execErr)
			if decision.RetrySameKey && attempt < retryPolicy.MaxSameKeyRetries {
				delay := retryPolicy.ComputeBackoff(attempt, decision.RetryAfter)
				if sleepErr := router.SleepContext(r.Context(), delay); sleepErr != nil {
					execErr = sleepErr
					break
				}
				continue
			}

			if decision.RotateKey {
				key.MarkFailure(s.KeyPool.BaseCooldown, s.KeyPool.MaxCooldown)
				s.Metrics.KeyRotationsTotal.WithLabelValues(providerName, "rate_limited").Inc()
				break
			}

			if decision.TriggerFallback {
				break
			}

			// Caller fault or non-retryable
			break
		}

		if execErr == nil && chatResp != nil {
			key.MarkSuccess()
			s.CircuitBreakers.Get(providerName).RecordSuccess()
			if sessionID != "" {
				_ = s.SessionAffinity.Bind(r.Context(), vKey.OrgID, sessionID, providerName, key.ID, currentModel, 30*time.Minute)
			}
			if exactCacheKey != "" && !chatReq.Stream {
				s.Cache.PutExact(exactCacheKey, chatResp, 1*time.Hour)
			}
			fallback.AnnotateDowngrade(chatResp, chatReq.RequestedModel, currentModel)

			// Response Headers for downgrade transparency
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Original-Model", chatReq.RequestedModel)
			w.Header().Set("X-Effective-Model", currentModel)
			if chatReq.RequestedModel != currentModel {
				w.Header().Set("X-Model-Downgraded", "true")
			}

			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(chatResp)

			// Post-Call Settlement
			actualCost := s.Pricing.CalculateCostMicro(currentModel, chatResp.Usage.PromptTokens, chatResp.Usage.CompletionTokens, 0)
			_ = s.Wallet.Settle(vKey.OrgID, estHoldMicro, actualCost, chatReq.ID, vKey.ID, "chat completion call")

			s.Metrics.TokensTotal.WithLabelValues(currentModel, providerName, "prompt").Add(float64(chatResp.Usage.PromptTokens))
			s.Metrics.TokensTotal.WithLabelValues(currentModel, providerName, "completion").Add(float64(chatResp.Usage.CompletionTokens))
			s.Metrics.RequestsTotal.WithLabelValues(currentModel, providerName, "200").Inc()
			s.Metrics.RequestDuration.WithLabelValues(currentModel, providerName, "false").Observe(time.Since(start).Seconds())
			return
		}

		if execErr != nil {
			s.CircuitBreakers.Get(providerName).RecordFailure()
			if sessionID != "" {
				_ = s.SessionAffinity.Unbind(r.Context(), vKey.OrgID, sessionID)
			}
		}

		// Try fallback if primary model failed and can downgrade
		nextModel, fErr := s.Fallback.GetNextModel(chatReq.RequestedModel, visitedModels)
		if fErr == nil {
			s.Metrics.ModelFallbacksTotal.WithLabelValues(chatReq.RequestedModel, nextModel).Inc()
			currentModel = nextModel
			visitedModels[currentModel] = true
			continue
		}

		lastErr = execErr
		break
	}

	// If execution completely failed, release hold 100%
	_ = s.Wallet.Release(vKey.OrgID, estHoldMicro)

	status := http.StatusServiceUnavailable
	errMsg := "All provider keys and fallbacks failed"
	if lastErr != nil {
		errMsg = lastErr.Error()
	}
	s.writeJSONError(w, status, errMsg, "gateway_error", "execution_failed")
	s.Metrics.RequestsTotal.WithLabelValues(chatReq.RequestedModel, "unknown", strconv.Itoa(status)).Inc()
}
