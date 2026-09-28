package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

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
	"github.com/ra1/model-gateway/internal/schema"
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
	mux             *http.ServeMux
	DefaultProvider string
}

// Config provides initial server dependencies.
type Config struct {
	KeyStore *store.MemoryKeyStore
	KeyPool  *keypool.Manager
	Pricing  *pricing.Registry
	Wallet   *wallet.Manager
	Fallback *fallback.Policy
	Metrics  *metrics.Metrics
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

	s := &Server{
		KeyStore:      cfg.KeyStore,
		KeyPool:       cfg.KeyPool,
		Pricing:       cfg.Pricing,
		Wallet:        cfg.Wallet,
		Fallback:      cfg.Fallback,
		Metrics:       cfg.Metrics,
		StreamHandler: stream.NewHandler(),
		Adapters:      make(map[string]provider.Adapter),
		mux:           http.NewServeMux(),
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
	s.mux.HandleFunc("/v1/chat/completions", s.handleChatCompletions)
	s.mux.HandleFunc("/v1/models", s.handleModels)
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/healthz", s.handleHealth)
	s.mux.HandleFunc("/ready", s.handleReady)
	s.mux.Handle("/metrics", s.Metrics.Handler())
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

		if chatReq.Stream {
			it, sErr := adapter.ExecuteStream(r.Context(), &chatReq, cred)
			if sErr != nil {
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
