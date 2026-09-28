package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds the registered Prometheus collectors.
type Metrics struct {
	Registry              *prometheus.Registry
	RequestsTotal         *prometheus.CounterVec
	ErrorsTotal           *prometheus.CounterVec
	TokensTotal           *prometheus.CounterVec
	KeyRotationsTotal     *prometheus.CounterVec
	ModelFallbacksTotal   *prometheus.CounterVec
	RequestDuration       *prometheus.HistogramVec
	UpstreamTTFT          *prometheus.HistogramVec
}

// NewMetrics initializes and registers all Prometheus collectors.
func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()

	m := &Metrics{
		Registry: reg,
		RequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "gateway_requests_total",
				Help: "Total number of HTTP requests processed by the gateway.",
			},
			[]string{"model", "provider", "status_code"},
		),
		ErrorsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "gateway_errors_total",
				Help: "Total number of classified error responses.",
			},
			[]string{"model", "provider", "failure_class"},
		),
		TokensTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "gateway_tokens_total",
				Help: "Total tokens generated and processed.",
			},
			[]string{"model", "provider", "token_type"},
		),
		KeyRotationsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "gateway_key_rotations_total",
				Help: "Total number of upstream key rotations.",
			},
			[]string{"provider", "reason"},
		),
		ModelFallbacksTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "gateway_model_fallbacks_total",
				Help: "Total number of model fallback activations.",
			},
			[]string{"original_model", "fallback_model"},
		),
		RequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "gateway_request_duration_seconds",
				Help:    "End-to-end HTTP request duration in seconds.",
				Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0},
			},
			[]string{"model", "provider", "is_stream"},
		),
		UpstreamTTFT: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "gateway_upstream_ttft_seconds",
				Help:    "Time to first token for streaming responses in seconds.",
				Buckets: []float64{0.05, 0.1, 0.2, 0.5, 1.0, 2.0, 5.0},
			},
			[]string{"model", "provider"},
		),
	}

	reg.MustRegister(
		m.RequestsTotal,
		m.ErrorsTotal,
		m.TokensTotal,
		m.KeyRotationsTotal,
		m.ModelFallbacksTotal,
		m.RequestDuration,
		m.UpstreamTTFT,
	)

	return m
}

// Handler returns the Prometheus HTTP scrape handler.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{})
}
