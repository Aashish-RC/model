package mockprovider

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestMockProvider_OpenAIExecution(t *testing.T) {
	mock := NewServer()
	defer mock.Close()

	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"Hello"}]}`
	req, err := http.NewRequest(http.MethodPost, mock.URL+"/v1/chat/completions", bytes.NewBufferString(reqBody))
	if err != nil {
		t.Fatalf("request creation error: %v", err)
	}
	req.Header.Set("Authorization", "Bearer sk-mock-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("call error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	history := mock.GetCallHistory()
	if len(history) != 1 {
		t.Fatalf("expected 1 call in history, got %d", len(history))
	}
	if history[0].APIKey != "Bearer sk-mock-key" {
		t.Errorf("expected api key Bearer sk-mock-key, got %s", history[0].APIKey)
	}
}

func TestMockProvider_FaultInjection(t *testing.T) {
	mock := NewServer()
	defer mock.Close()

	tests := []struct {
		name               string
		headers            map[string]string
		expectedStatus     int
		expectedRetryAfter string
	}{
		{
			name: "Injected 429 with Retry-After",
			headers: map[string]string{
				"X-Mock-Status":      "429",
				"X-Mock-Retry-After": "3",
				"X-Mock-Error-Type":  "rate_limit_exceeded",
			},
			expectedStatus:     429,
			expectedRetryAfter: "3",
		},
		{
			name: "Injected 500 Server Error",
			headers: map[string]string{
				"X-Mock-Status": "500",
			},
			expectedStatus:     500,
			expectedRetryAfter: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodPost, mock.URL+"/v1/chat/completions", bytes.NewBufferString(`{}`))
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("http call error: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, resp.StatusCode)
			}
			if tt.expectedRetryAfter != "" && resp.Header.Get("Retry-After") != tt.expectedRetryAfter {
				t.Errorf("expected Retry-After %s, got %s", tt.expectedRetryAfter, resp.Header.Get("Retry-After"))
			}
		})
	}
}

func TestMockProvider_StreamingWithDisconnect(t *testing.T) {
	mock := NewServer()
	defer mock.Close()

	req, _ := http.NewRequest(http.MethodPost, mock.URL+"/v1/chat/completions", bytes.NewBufferString(`{"model":"gpt-4o","stream":true}`))
	req.Header.Set("X-Mock-Stream-Disconnect-After-Chunks", "2")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("streaming call error: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	content := string(body)
	if !strings.Contains(content, "data: ") {
		t.Errorf("expected stream data chunks, got %s", content)
	}
	// Verify that [DONE] was not emitted because the connection was aborted
	if strings.Contains(content, "[DONE]") {
		t.Errorf("expected stream to disconnect before [DONE]")
	}
}

func TestMockProvider_LatencyInjection(t *testing.T) {
	mock := NewServer()
	defer mock.Close()

	start := time.Now()
	req, _ := http.NewRequest(http.MethodPost, mock.URL+"/v1/chat/completions", bytes.NewBufferString(`{"model":"gpt-4o"}`))
	req.Header.Set("X-Mock-Delay-Ms", "50")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("call error: %v", err)
	}
	defer resp.Body.Close()
	elapsed := time.Since(start)

	if elapsed < 45*time.Millisecond {
		t.Errorf("expected at least 45ms latency, took %v", elapsed)
	}
}
