package stream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ra1/model-gateway/internal/provider"
	"github.com/ra1/model-gateway/internal/schema"
)

var (
	ErrClientDisconnected = errors.New("client disconnected mid-stream")
	ErrStreamAborted      = errors.New("upstream stream aborted prematurely")
)

// Accumulator collects streaming telemetry and token usage.
type Accumulator struct {
	mu               sync.Mutex
	StartTime        time.Time
	FirstTokenTime   time.Duration
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	ContentBuilder   strings.Builder
	UsageCaptured    bool
	ChunkCount       int
}

// NewAccumulator initializes an accumulator for a streaming request.
func NewAccumulator(promptTokens int) *Accumulator {
	return &Accumulator{
		StartTime:    time.Now(),
		PromptTokens: promptTokens,
	}
}

// RecordChunk updates statistics based on an incoming stream chunk.
func (a *Accumulator) RecordChunk(chunk *schema.StreamChunk) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.ChunkCount++
	if a.FirstTokenTime == 0 {
		elapsed := time.Since(a.StartTime)
		if elapsed == 0 {
			elapsed = time.Nanosecond
		}
		a.FirstTokenTime = elapsed
	}

	for _, choice := range chunk.Choices {
		if choice.Delta.Content != "" {
			a.ContentBuilder.WriteString(choice.Delta.Content)
		}
	}

	if chunk.Usage != nil && chunk.Usage.TotalTokens > 0 {
		a.UsageCaptured = true
		a.PromptTokens = chunk.Usage.PromptTokens
		a.CompletionTokens = chunk.Usage.CompletionTokens
		a.TotalTokens = chunk.Usage.TotalTokens
	}
}

// Finalize resolves token totals. If usage was not emitted, estimate based on text.
func (a *Accumulator) Finalize() schema.Usage {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.UsageCaptured {
		// Fast heuristic estimate: ~4 chars per token if no tokenizer hooked yet
		text := a.ContentBuilder.String()
		a.CompletionTokens = len(text) / 4
		if a.CompletionTokens == 0 && len(text) > 0 {
			a.CompletionTokens = 1
		}
		a.TotalTokens = a.PromptTokens + a.CompletionTokens
	}

	return schema.Usage{
		PromptTokens:     a.PromptTokens,
		CompletionTokens: a.CompletionTokens,
		TotalTokens:      a.TotalTokens,
	}
}

// Handler forwards chunks from upstream to HTTP response with low overhead.
type Handler struct {
	FlushInterval time.Duration
}

// NewHandler creates a stream handler.
func NewHandler() *Handler {
	return &Handler{
		FlushInterval: 0, // Flush immediately
	}
}

// ForwardStream pipes chunks from upstream StreamIterator to the client http.ResponseWriter.
func (h *Handler) ForwardStream(
	ctx context.Context,
	w http.ResponseWriter,
	iterator provider.StreamIterator,
	accum *Accumulator,
) error {
	defer iterator.Close()

	flusher, ok := w.(http.Flusher)
	if !ok {
		return errors.New("streaming unsupported by ResponseWriter")
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	firstChunkSent := false

	for {
		select {
		case <-ctx.Done():
			// Client disconnected or request timeout
			return ErrClientDisconnected
		default:
		}

		chunk, err := iterator.Next()
		if err == io.EOF {
			// Stream completed normally
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			flusher.Flush()
			return nil
		}
		if err != nil {
			// Mid-stream error
			if firstChunkSent {
				// We already wrote 200 OK and chunks: emit SSE error frame
				errFrame := `data: {"error":{"message":"Upstream stream interrupted","type":"upstream_error","code":"stream_terminated"}}` + "\n\n"
				_, _ = fmt.Fprint(w, errFrame)
				_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
				flusher.Flush()
				return ErrStreamAborted
			}
			return err
		}

		if chunk != nil {
			accum.RecordChunk(chunk)
			dataBytes, marshalErr := json.Marshal(chunk)
			if marshalErr == nil {
				_, _ = fmt.Fprintf(w, "data: %s\n\n", dataBytes)
				flusher.Flush()
				firstChunkSent = true
			}
		}
	}
}
