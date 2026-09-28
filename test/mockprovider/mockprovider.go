package mockprovider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CallRecord captures an intercepted request to the mock provider.
type CallRecord struct {
	Timestamp  time.Time
	Method     string
	Path       string
	Headers    http.Header
	Body       []byte
	APIKey     string
}

// Server provides a controllable upstream test HTTP server.
type Server struct {
	server      *httptest.Server
	mu          sync.RWMutex
	calls       []CallRecord
	URL         string
	DefaultTTFT time.Duration
}

// NewServer initializes and starts a new mock provider server.
func NewServer() *Server {
	s := &Server{
		calls:       make([]CallRecord, 0),
		DefaultTTFT: 2 * time.Millisecond,
	}
	s.server = httptest.NewServer(http.HandlerFunc(s.handleHTTP))
	s.URL = s.server.URL
	return s
}

// Close shuts down the test server.
func (s *Server) Close() {
	s.server.Close()
}

// ResetHistory clears all captured calls.
func (s *Server) ResetHistory() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = make([]CallRecord, 0)
}

// GetCallHistory returns a slice of all recorded calls.
func (s *Server) GetCallHistory() []CallRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	copied := make([]CallRecord, len(s.calls))
	copy(copied, s.calls)
	return copied
}

func (s *Server) recordCall(r *http.Request, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	apiKey := r.Header.Get("Authorization")
	if apiKey == "" {
		apiKey = r.Header.Get("x-api-key")
	}
	if apiKey == "" {
		apiKey = r.Header.Get("x-goog-api-key")
	}

	s.calls = append(s.calls, CallRecord{
		Timestamp: time.Now(),
		Method:    r.Method,
		Path:      r.URL.Path,
		Headers:   r.Header.Clone(),
		Body:      body,
		APIKey:    apiKey,
	})
}

func (s *Server) handleHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.recordCall(r, body)

	// Fault Injection: Delay
	if delayHeader := r.Header.Get("X-Mock-Delay-Ms"); delayHeader != "" {
		if delayMs, err := strconv.Atoi(delayHeader); err == nil && delayMs > 0 {
			time.Sleep(time.Duration(delayMs) * time.Millisecond)
		}
	} else if s.DefaultTTFT > 0 {
		time.Sleep(s.DefaultTTFT)
	}

	// Fault Injection: Status override (429, 500, 529, 401, etc.)
	if statusHeader := r.Header.Get("X-Mock-Status"); statusHeader != "" {
		if status, err := strconv.Atoi(statusHeader); err == nil && status >= 400 {
			if retryAfter := r.Header.Get("X-Mock-Retry-After"); retryAfter != "" {
				w.Header().Set("Retry-After", retryAfter)
			}
			errType := r.Header.Get("X-Mock-Error-Type")
			if errType == "" {
				errType = "mock_injected_error"
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{
					"message": fmt.Sprintf("Mock injected error status %d", status),
					"type":    errType,
					"code":    errType,
				},
			})
			return
		}
	}

	// Route based on path dialect
	path := r.URL.Path
	if strings.Contains(path, "/chat/completions") {
		s.handleOpenAI(w, r, body)
	} else if strings.Contains(path, "/messages") {
		s.handleAnthropic(w, r, body)
	} else if strings.Contains(path, "generateContent") {
		s.handleGemini(w, r, body)
	} else {
		// Generic fallback to OpenAI format
		s.handleOpenAI(w, r, body)
	}
}

func (s *Server) handleOpenAI(w http.ResponseWriter, r *http.Request, body []byte) {
	var payload struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	_ = json.Unmarshal(body, &payload)

	if payload.Stream {
		s.streamOpenAI(w, r, payload.Model)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":      "chatcmpl-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   payload.Model,
		"choices": []map[string]any{
			{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "This is a mock completion response.",
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     12,
			"completion_tokens": 8,
			"total_tokens":      20,
		},
	})
}

func (s *Server) streamOpenAI(w http.ResponseWriter, r *http.Request, model string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Fault Injection: Disconnect after N chunks
	disconnectLimit := -1
	if disc := r.Header.Get("X-Mock-Stream-Disconnect-After-Chunks"); disc != "" {
		if d, err := strconv.Atoi(disc); err == nil {
			disconnectLimit = d
		}
	}

	// Fault Injection: Chunk delay
	chunkDelay := 5 * time.Millisecond
	if cd := r.Header.Get("X-Mock-Stream-Chunk-Delay-Ms"); cd != "" {
		if d, err := strconv.Atoi(cd); err == nil && d > 0 {
			chunkDelay = time.Duration(d) * time.Millisecond
		}
	}

	tokens := []string{"This ", "is ", "a ", "mock ", "stream."}
	for i, token := range tokens {
		if disconnectLimit >= 0 && i >= disconnectLimit {
			// Abruptly sever connection
			if hj, ok := w.(http.Hijacker); ok {
				if conn, _, err := hj.Hijack(); err == nil {
					conn.Close()
					return
				}
			}
			return
		}

		if chunkDelay > 0 {
			time.Sleep(chunkDelay)
		}

		// Fault Injection: Malformed chunk
		if r.Header.Get("X-Mock-Stream-Malformed-Chunk") == strconv.Itoa(i) {
			_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\n\n")
			flusher.Flush()
			continue
		}

		chunkData := fmt.Sprintf(`data: {"id":"chatcmpl-mock","object":"chat.completion.chunk","created":%d,"model":"%s","choices":[{"index":0,"delta":{"content":"%s"},"finish_reason":null}]}`+"\n\n",
			time.Now().Unix(), model, token)
		_, _ = fmt.Fprint(w, chunkData)
		flusher.Flush()
	}

	// Send final usage chunk and [DONE]
	usageChunk := fmt.Sprintf(`data: {"id":"chatcmpl-mock","object":"chat.completion.chunk","created":%d,"model":"%s","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`+"\n\n",
		time.Now().Unix(), model)
	_, _ = fmt.Fprint(w, usageChunk)
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func (s *Server) handleAnthropic(w http.ResponseWriter, r *http.Request, body []byte) {
	var payload struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	_ = json.Unmarshal(body, &payload)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":    "msg_mock_" + strconv.FormatInt(time.Now().UnixNano(), 10),
		"type":  "message",
		"role":  "assistant",
		"model": payload.Model,
		"content": []map[string]any{
			{
				"type": "text",
				"text": "This is a mock Anthropic completion.",
			},
		},
		"stop_reason": "end_turn",
		"usage": map[string]any{
			"input_tokens":  15,
			"output_tokens": 10,
		},
	})
}

func (s *Server) handleGemini(w http.ResponseWriter, r *http.Request, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"candidates": []map[string]any{
			{
				"content": map[string]any{
					"parts": []map[string]any{
						{"text": "This is a mock Gemini completion."},
					},
					"role": "model",
				},
				"finishReason": "STOP",
				"index":        0,
			},
		},
		"usageMetadata": map[string]any{
			"promptTokenCount":     20,
			"candidatesTokenCount": 12,
			"totalTokenCount":      32,
		},
	})
}
