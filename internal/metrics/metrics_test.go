package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetrics_RecordingAndScraping(t *testing.T) {
	m := NewMetrics()

	// Increment metrics
	m.RequestsTotal.WithLabelValues("gpt-4o", "openai", "200").Inc()
	m.TokensTotal.WithLabelValues("gpt-4o", "openai", "prompt").Add(100)
	m.TokensTotal.WithLabelValues("gpt-4o", "openai", "completion").Add(50)
	m.ModelFallbacksTotal.WithLabelValues("gpt-4o", "claude-3-5-sonnet").Inc()
	m.RequestDuration.WithLabelValues("gpt-4o", "openai", "false").Observe(0.35)

	// Scrape endpoint
	req, _ := http.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "gateway_requests_total") {
		t.Errorf("expected gateway_requests_total in scrape")
	}
	if !strings.Contains(body, "gateway_tokens_total") {
		t.Errorf("expected gateway_tokens_total in scrape")
	}
	if !strings.Contains(body, "gateway_model_fallbacks_total") {
		t.Errorf("expected gateway_model_fallbacks_total in scrape")
	}
}
