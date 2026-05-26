package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/BackendStack21/go-vector/pkg/vector"
	"github.com/BackendStack21/odek/internal/llm"
)

// ── Constants ─────────────────────────────────────────────────────────────

const (
	// vectorDim is the output dimensionality for RandomProjections.
	// 256 dims balances search quality with memory/CPU for ~100K sessions.
	vectorDim = 256

	// vectorFile is the persisted vector store filename.
	vectorFile = "vectors.gob"

	// embedderFile is the persisted RandomProjections state filename.
	embedderFile = "embedder.gob"
)

// ── Vector Index ──────────────────────────────────────────────────────────

// VectorIndex provides semantic session search using go-vector's
// RandomProjections embedder + brute-force k-NN store.
//
// Lifecycle:
//  1. Init() loads persisted state, or fits+embeds from all sessions.
//  2. On Add(): embed conversation text and insert into store.
//  3. On Search(): embed query, k-NN search, return ranked results.
//  4. On Remove(): delete from store.
//  5. Save() persists both embedder and store to disk atomically.
//
// Thread-safe: all exported methods hold a RWMutex.
type VectorIndex struct {
	mu    sync.RWMutex
	store *vector.Store
	emb   *vector.RandomProjections
	dir   string
	ready bool
}

// ── Init ──────────────────────────────────────────────────────────────────

// Init creates or loads the vector index from the session directory.
// If persisted state exists, it is loaded directly.
// Otherwise, it scans all session JSON files, fits the embedder,
// indexes every session, and persists the result.
func (vi *VectorIndex) Init(dir string) error {
	vi.dir = dir

	storePath := filepath.Join(dir, vectorFile)
	embPath := filepath.Join(dir, embedderFile)

	// Try loading existing persisted state.
	store := vector.NewStore(vector.CosineDistance)
	if err := store.Load(storePath); err == nil {
		emb, err := vector.LoadEmbedder(embPath)
		if err == nil {
			vi.store = store
			vi.emb = emb
			vi.ready = true
			return nil
		}
	}

	// No valid persisted state — build from scratch.
	return vi.rebuild(dir)
}

// rebuild scans all session files, fits the embedder, and indexes
// every session's conversation text.
func (vi *VectorIndex) rebuild(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("vector: read dir: %w", err)
	}

	// Collect all session texts for fitting + indexing.
	type sessText struct {
		id   string
		text string
	}
	var allTexts []sessText
	var corpus []string

	for _, e := range entries {
		if e.IsDir() || !isSessionFile(e.Name()) {
			continue
		}
		sid := idFromPath(e.Name())
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		text := extractConversationText(data)
		if text == "" {
			continue
		}
		allTexts = append(allTexts, sessText{id: sid, text: text})
		corpus = append(corpus, text)
	}

	// Fit the embedder.
	vi.emb = vector.NewRandomProjections(vectorDim)
	vi.emb.Fit(corpus)

	// Embed all sessions.
	vi.store = vector.NewStore(vector.CosineDistance)
	for _, st := range allTexts {
		vec, err := vi.emb.Embed(st.text)
		if err != nil {
			continue
		}
		vi.store.Add(st.id, vec)
	}

	vi.ready = true
	return vi.Save()
}

// Ready returns true if the index has been initialized.
func (vi *VectorIndex) Ready() bool {
	vi.mu.RLock()
	defer vi.mu.RUnlock()
	return vi.ready
}

// ── Mutation ──────────────────────────────────────────────────────────────

// Add embeds the conversation text and adds the session to the index.
// If the session already exists, it is replaced (remove then add).
func (vi *VectorIndex) Add(sessionID string, messages []llm.Message) error {
	vi.mu.Lock()
	defer vi.mu.Unlock()

	if !vi.ready {
		return fmt.Errorf("vector: index not initialized")
	}

	text := BuildConversationText(messages)
	if text == "" {
		return nil
	}

	vec, err := vi.emb.Embed(text)
	if err != nil {
		return fmt.Errorf("vector: embed: %w", err)
	}

	// Replace if exists.
	vi.store.Remove(sessionID)
	vi.store.Add(sessionID, vec)

	return vi.saveLocked()
}

// Remove deletes a session from the index. Idempotent.
func (vi *VectorIndex) Remove(sessionID string) error {
	vi.mu.Lock()
	defer vi.mu.Unlock()

	if !vi.ready {
		return nil
	}

	vi.store.Remove(sessionID)
	return vi.saveLocked()
}

// ── Search ────────────────────────────────────────────────────────────────

// SearchResult holds a single session search result.
type SearchResult struct {
	SessionID string  `json:"session_id"`
	Score     float32 `json:"score"` // cosine similarity, higher = more relevant
}

// Search embeds the query and returns the k most similar sessions
// ranked by cosine similarity.
// Returns nil if the index is not ready or no results found.
func (vi *VectorIndex) Search(query string, k int) ([]SearchResult, error) {
	vi.mu.RLock()
	defer vi.mu.RUnlock()

	if !vi.ready || vi.store.Len() == 0 {
		return nil, nil
	}
	if k <= 0 {
		k = 5
	}
	if k > 20 {
		k = 20
	}

	vec, err := vi.emb.Embed(query)
	if err != nil {
		return nil, fmt.Errorf("vector: embed query: %w", err)
	}

	// Cosine similarity: go-vector returns distance = 1 - similarity.
	// Convert: score = 1 - distance.
	results := vi.store.Search(vec, k)
	if len(results) == 0 {
		return nil, nil
	}

	out := make([]SearchResult, len(results))
	for i, r := range results {
		out[i] = SearchResult{
			SessionID: r.ID,
			Score:     1 - r.Distance,
		}
	}
	return out, nil
}

// ── Persistence ───────────────────────────────────────────────────────────

// Save persists both the embedder state and the vector store to disk.
func (vi *VectorIndex) Save() error {
	vi.mu.Lock()
	defer vi.mu.Unlock()
	return vi.saveLocked()
}

func (vi *VectorIndex) saveLocked() error {
	if !vi.ready || vi.dir == "" {
		return nil
	}

	// Save vector store.
	storePath := filepath.Join(vi.dir, vectorFile)
	tmpStore := storePath + ".tmp"
	if err := vi.store.Save(tmpStore); err != nil {
		os.Remove(tmpStore)
		return fmt.Errorf("vector: save store: %w", err)
	}
	if err := os.Rename(tmpStore, storePath); err != nil {
		os.Remove(tmpStore)
		return fmt.Errorf("vector: rename store: %w", err)
	}

	// Save embedder state.
	embPath := filepath.Join(vi.dir, embedderFile)
	tmpEmb := embPath + ".tmp"
	if err := vi.emb.SaveEmbedder(tmpEmb); err != nil {
		os.Remove(tmpEmb)
		return fmt.Errorf("vector: save embedder: %w", err)
	}
	if err := os.Rename(tmpEmb, embPath); err != nil {
		os.Remove(tmpEmb)
		return fmt.Errorf("vector: rename embedder: %w", err)
	}

	return nil
}

// ── Conversation Text Extraction ──────────────────────────────────────────

// BuildConversationText extracts user and assistant text from messages
// for embedding. Tool calls and results are excluded — they add noise.
func BuildConversationText(messages []llm.Message) string {
	var out string
	for _, m := range messages {
		switch m.Role {
		case "user":
			if m.Content != "" {
				out += "[User] " + m.Content + "\n"
			}
		case "assistant":
			if m.Content != "" {
				out += "[Assistant] " + m.Content + "\n"
			}
		}
	}
	return out
}

// extractConversationText parses raw JSON session bytes and extracts
// user+assistant text. Used during initial index rebuild before the
// session.Store.Load path is available for bulk operations.
func extractConversationText(data []byte) string {
	var raw struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return ""
	}
	var out string
	for _, m := range raw.Messages {
		switch m.Role {
		case "user":
			if m.Content != "" {
				out += "[User] " + m.Content + "\n"
			}
		case "assistant":
			if m.Content != "" {
				out += "[Assistant] " + m.Content + "\n"
			}
		}
	}
	return out
}

// Ensure json import is used (the explicit json.Unmarshal call above
// satisfies this, but Go might complain if the function is unused —
// keep this here to guarantee the import is visible to tests).
var _ = json.Unmarshal
