// Package budget contains the per-session circuit breaker.
package budget

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/tokenless/tokenless/pkg/config"
)

// Action is one recorded entry in a session's recent-actions ring buffer.
type Action struct {
	Timestamp time.Time `json:"timestamp"`
	Action    string    `json:"action"`
	Tokens    int64     `json:"tokens"`
	Cost      float64   `json:"cost"`
	Outcome   string    `json:"outcome,omitempty"`
}

// Session is the persisted state of a single agent session.
type Session struct {
	ID               string    `json:"id"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	TotalTokens      int64     `json:"total_tokens"`
	TotalCostUSD     float64   `json:"total_cost_usd"`
	PromptTokens     int64     `json:"prompt_tokens"`
	CompletionTokens int64     `json:"completion_tokens"`
	CachedTokens     int64     `json:"cached_tokens"`
	RecentActions    []Action  `json:"recent_actions"`
	TripCount        int       `json:"trip_count"`
	Tripped          bool      `json:"tripped"`
	mu               sync.Mutex `json:"-"`
}

// Lock and Unlock expose the embedded mutex.
func (s *Session) Lock()   { s.mu.Lock() }
func (s *Session) Unlock() { s.mu.Unlock() }

// Store persists sessions to disk as a single JSON map.
type Store struct {
	mu       sync.Mutex
	path     string
	Sessions map[string]*Session `json:"sessions"`
}

// NewStore opens the persistent store at config.SessionsPath().
func NewStore() (*Store, error) {
	path, err := config.SessionsPath()
	if err != nil {
		return nil, err
	}
	s := &Store{
		path:     path,
		Sessions: make(map[string]*Session),
	}
	if err := s.load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return s, nil
}

// GetOrCreate returns the session for id, creating it if absent.
func (s *Store) GetOrCreate(id string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.Sessions[id]; ok {
		return sess
	}
	now := time.Now().UTC()
	sess := &Session{
		ID:            id,
		CreatedAt:     now,
		UpdatedAt:     now,
		RecentActions: make([]Action, 0, 16),
	}
	s.Sessions[id] = sess
	return sess
}

// Get returns the session for id and a presence flag.
func (s *Store) Get(id string) (*Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.Sessions[id]
	return sess, ok
}

// Save flushes the in-memory store to disk atomically.
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persist()
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, s)
}

func (s *Store) persist() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}