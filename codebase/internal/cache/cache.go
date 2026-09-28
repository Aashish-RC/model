package cache

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/ra1/model-gateway/internal/schema"
)

const (
	HeaderCache           = "X-Gateway-Cache"
	HeaderCacheType       = "X-Gateway-Cache-Type"
	HeaderCacheSimilarity = "X-Gateway-Cache-Similarity"

	CacheHit  = "HIT"
	CacheMiss = "MISS"

	TypeExact    = "exact"
	TypeSemantic = "semantic"

	DefaultSemanticThreshold = 0.95
)

// CachedResponse stores the serialized response and metadata.
type CachedResponse struct {
	Response   *schema.CanonicalChatResponse
	CreatedAt  time.Time
	ExpiresAt  time.Time
	CacheType  string
	Similarity float64
}

// Vector represents an embedding vector.
type Vector []float32

// CosineSimilarity computes cosine similarity between two vectors.
func CosineSimilarity(a, b Vector) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0.0
	}
	var dot, normA, normB float64
	for i := range a {
		valA := float64(a[i])
		valB := float64(b[i])
		dot += valA * valB
		normA += valA * valA
		normB += valB * valB
	}
	if normA == 0 || normB == 0 {
		return 0.0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

type lruEntry struct {
	key       string
	embedding Vector
	prompt    string
	resp      *schema.CanonicalChatResponse
	expiresAt time.Time
}

// Config configures the cache engine.
type Config struct {
	MaxEntries        int
	DefaultTTL        time.Duration
	SemanticThreshold float64
}

// DefaultConfig provides standard cache settings.
func DefaultConfig() Config {
	return Config{
		MaxEntries:        10000,
		DefaultTTL:        1 * time.Hour,
		SemanticThreshold: DefaultSemanticThreshold,
	}
}

// Engine implements exact and semantic caching.
type Engine struct {
	cfg Config
	mu  sync.RWMutex

	// Exact cache (LRU)
	exactList  *list.List
	exactItems map[string]*list.Element

	// Semantic cache (LRU + linear scan over candidate vectors)
	semanticList  *list.List
	semanticItems map[string]*list.Element
}

// NewEngine initializes the cache engine.
func NewEngine(cfg Config) *Engine {
	if cfg.MaxEntries <= 0 {
		cfg.MaxEntries = 10000
	}
	if cfg.DefaultTTL <= 0 {
		cfg.DefaultTTL = 1 * time.Hour
	}
	if cfg.SemanticThreshold <= 0 {
		cfg.SemanticThreshold = DefaultSemanticThreshold
	}

	return &Engine{
		cfg:           cfg,
		exactList:     list.New(),
		exactItems:    make(map[string]*list.Element),
		semanticList:  list.New(),
		semanticItems: make(map[string]*list.Element),
	}
}

// HashRequest generates a normalized SHA-256 key for exact matching.
func HashRequest(orgID string, req *schema.CanonicalChatRequest) string {
	type normReq struct {
		OrgID    string               `json:"org"`
		Model    string               `json:"model"`
		Messages []schema.ChatMessage `json:"messages"`
		Temp     *float32             `json:"temp,omitempty"`
	}

	n := normReq{
		OrgID:    orgID,
		Model:    strings.TrimSpace(req.RequestedModel),
		Messages: req.Messages,
		Temp:     req.Temperature,
	}

	data, _ := json.Marshal(n)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// GetExact attempts an exact-match lookup.
func (e *Engine) GetExact(key string) (*schema.CanonicalChatResponse, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	elem, found := e.exactItems[key]
	if !found {
		return nil, false
	}

	entry := elem.Value.(*lruEntry)
	if time.Now().After(entry.expiresAt) {
		e.exactList.Remove(elem)
		delete(e.exactItems, key)
		return nil, false
	}

	e.exactList.MoveToFront(elem)
	return entry.resp, true
}

// PutExact stores an exact-match response in the cache.
func (e *Engine) PutExact(key string, resp *schema.CanonicalChatResponse, ttl time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if ttl <= 0 {
		ttl = e.cfg.DefaultTTL
	}

	if elem, found := e.exactItems[key]; found {
		e.exactList.MoveToFront(elem)
		elem.Value.(*lruEntry).resp = resp
		elem.Value.(*lruEntry).expiresAt = time.Now().Add(ttl)
		return
	}

	// Enforce capacity limit
	for e.exactList.Len() >= e.cfg.MaxEntries {
		oldest := e.exactList.Back()
		if oldest == nil {
			break
		}
		e.exactList.Remove(oldest)
		delete(e.exactItems, oldest.Value.(*lruEntry).key)
	}

	entry := &lruEntry{
		key:       key,
		resp:      resp,
		expiresAt: time.Now().Add(ttl),
	}
	elem := e.exactList.PushFront(entry)
	e.exactItems[key] = elem
}

// GetSemantic checks vector similarity against indexed entries.
func (e *Engine) GetSemantic(queryVec Vector) (*schema.CanonicalChatResponse, float64, bool) {
	if len(queryVec) == 0 {
		return nil, 0, false
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()
	var bestResp *schema.CanonicalChatResponse
	var bestSim float64 = -1.0
	var bestElem *list.Element

	for elem := e.semanticList.Front(); elem != nil; {
		next := elem.Next()
		entry := elem.Value.(*lruEntry)
		if now.After(entry.expiresAt) {
			e.semanticList.Remove(elem)
			delete(e.semanticItems, entry.key)
			elem = next
			continue
		}

		sim := CosineSimilarity(queryVec, entry.embedding)
		if sim > bestSim {
			bestSim = sim
			bestResp = entry.resp
			bestElem = elem
		}
		elem = next
	}

	if bestSim >= e.cfg.SemanticThreshold && bestResp != nil {
		e.semanticList.MoveToFront(bestElem)
		return bestResp, bestSim, true
	}

	return nil, bestSim, false
}

// PutSemantic indexes a response with its prompt embedding vector.
func (e *Engine) PutSemantic(key string, vec Vector, prompt string, resp *schema.CanonicalChatResponse, ttl time.Duration) {
	if len(vec) == 0 {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if ttl <= 0 {
		ttl = e.cfg.DefaultTTL
	}

	// Enforce capacity limit
	for e.semanticList.Len() >= e.cfg.MaxEntries {
		oldest := e.semanticList.Back()
		if oldest == nil {
			break
		}
		e.semanticList.Remove(oldest)
		delete(e.semanticItems, oldest.Value.(*lruEntry).key)
	}

	entry := &lruEntry{
		key:       key,
		embedding: vec,
		prompt:    prompt,
		resp:      resp,
		expiresAt: time.Now().Add(ttl),
	}
	elem := e.semanticList.PushFront(entry)
	e.semanticItems[key] = elem
}
