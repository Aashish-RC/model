package stream

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ra1/model-gateway/internal/schema"
)

type mockStreamIterator struct {
	chunks []*schema.StreamChunk
	idx    int
	failAt int
	closed bool
}

func (m *mockStreamIterator) Next() (*schema.StreamChunk, error) {
	if m.failAt > 0 && m.idx >= m.failAt {
		return nil, errors.New("simulated mid-stream failure")
	}
	if m.idx >= len(m.chunks) {
		return nil, io.EOF
	}
	c := m.chunks[m.idx]
	m.idx++
	return c, nil
}

func (m *mockStreamIterator) Close() error {
	m.closed = true
	return nil
}

func TestAccumulator_UsageCaptureAndFinalize(t *testing.T) {
	accum := NewAccumulator(10)

	// Chunk 1
	accum.RecordChunk(&schema.StreamChunk{
		Choices: []schema.StreamChoice{
			{Delta: schema.StreamDelta{Content: "Hello "}},
		},
	})

	if accum.FirstTokenTime == 0 {
		t.Errorf("expected FirstTokenTime to be set")
	}

	// Chunk 2 with Usage
	accum.RecordChunk(&schema.StreamChunk{
		Choices: []schema.StreamChoice{
			{Delta: schema.StreamDelta{Content: "world!"}},
		},
		Usage: &schema.Usage{
			PromptTokens:     10,
			CompletionTokens: 2,
			TotalTokens:      12,
		},
	})

	usage := accum.Finalize()
	if usage.PromptTokens != 10 || usage.CompletionTokens != 2 || usage.TotalTokens != 12 {
		t.Errorf("expected exact usage 10/2/12, got %+v", usage)
	}
}

func TestHandler_ForwardStream_Success(t *testing.T) {
	handler := NewHandler()
	accum := NewAccumulator(5)

	it := &mockStreamIterator{
		chunks: []*schema.StreamChunk{
			{
				ID: "chunk-1",
				Choices: []schema.StreamChoice{
					{Delta: schema.StreamDelta{Content: "Alpha"}},
				},
			},
			{
				ID: "chunk-2",
				Choices: []schema.StreamChoice{
					{Delta: schema.StreamDelta{Content: "Beta"}},
				},
			},
		},
	}

	rec := httptest.NewRecorder()
	ctx := context.Background()

	err := handler.ForwardStream(ctx, rec, it, accum)
	if err != nil {
		t.Fatalf("unexpected ForwardStream error: %v", err)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "chunk-1") || !strings.Contains(body, "chunk-2") {
		t.Errorf("expected chunks in body, got: %s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Errorf("expected [DONE] in stream output")
	}
	if !it.closed {
		t.Errorf("expected iterator to be closed")
	}
}

func TestHandler_ForwardStream_MidStreamFailure(t *testing.T) {
	handler := NewHandler()
	accum := NewAccumulator(5)

	it := &mockStreamIterator{
		chunks: []*schema.StreamChunk{
			{
				ID: "chunk-1",
				Choices: []schema.StreamChoice{
					{Delta: schema.StreamDelta{Content: "Alpha"}},
				},
			},
		},
		failAt: 1, // fails on 2nd chunk
	}

	rec := httptest.NewRecorder()
	ctx := context.Background()

	err := handler.ForwardStream(ctx, rec, it, accum)
	if err != ErrStreamAborted {
		t.Errorf("expected ErrStreamAborted, got %v", err)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "stream_terminated") {
		t.Errorf("expected stream_terminated error frame in body, got: %s", body)
	}
}

func TestHandler_ForwardStream_ClientCancel(t *testing.T) {
	handler := NewHandler()
	accum := NewAccumulator(5)

	it := &mockStreamIterator{
		chunks: []*schema.StreamChunk{
			{ID: "chunk-1"},
		},
	}

	rec := httptest.NewRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := handler.ForwardStream(ctx, rec, it, accum)
	if err != ErrClientDisconnected {
		t.Errorf("expected ErrClientDisconnected, got %v", err)
	}
}
