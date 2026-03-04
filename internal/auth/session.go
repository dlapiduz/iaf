package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Session represents an agent session with its associated namespace.
type Session struct {
	ID             string        `json:"id"`
	Namespace      string        `json:"namespace"`
	Name           string        `json:"name"`
	CreatedAt      time.Time     `json:"created_at"`
	LastActivityAt time.Time     `json:"last_activity_at"`
	TTL            time.Duration `json:"ttl"` // 0 = no expiry
}

// Expired returns true if the session has a TTL and has been inactive beyond it.
func (s *Session) Expired() bool {
	if s.TTL == 0 {
		return false
	}
	last := s.LastActivityAt
	if last.IsZero() {
		last = s.CreatedAt
	}
	return time.Since(last) > s.TTL
}

// SessionStore manages sessions with file-based persistence.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	path     string
}

// NewSessionStore creates a new session store that persists to the given file path.
// If the file exists, sessions are loaded from it.
func NewSessionStore(path string) (*SessionStore, error) {
	s := &SessionStore{
		sessions: make(map[string]*Session),
		path:     path,
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("creating session store directory: %w", err)
	}

	data, err := os.ReadFile(path)
	if err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &s.sessions); err != nil {
			return nil, fmt.Errorf("loading sessions: %w", err)
		}
	}

	return s, nil
}

// Register creates a new session with an auto-generated ID, namespace, and optional TTL.
// ttl == 0 means sessions never expire.
func (s *SessionStore) Register(name string, ttl time.Duration) (*Session, error) {
	id, err := generateID()
	if err != nil {
		return nil, fmt.Errorf("generating session ID: %w", err)
	}

	now := time.Now().UTC()
	sess := &Session{
		ID:             id,
		Namespace:      "iaf-" + id,
		Name:           name,
		CreatedAt:      now,
		LastActivityAt: now,
		TTL:            ttl,
	}

	s.mu.Lock()
	s.sessions[id] = sess
	err = s.persistLocked()
	s.mu.Unlock()

	if err != nil {
		return nil, fmt.Errorf("persisting session: %w", err)
	}
	return sess, nil
}

// Touch updates the session's LastActivityAt to now.
// Silently does nothing if the session is not found.
func (s *SessionStore) Touch(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[sessionID]; ok {
		sess.LastActivityAt = time.Now().UTC()
		_ = s.persistLocked()
	}
}

// Delete removes the session from the store.
func (s *SessionStore) Delete(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[sessionID]; !ok {
		return fmt.Errorf("session %q not found", sessionID)
	}
	delete(s.sessions, sessionID)
	return s.persistLocked()
}

// Lookup returns the session for the given ID, or false if not found.
func (s *SessionStore) Lookup(id string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	return sess, ok
}

// List returns all sessions.
func (s *SessionStore) List() []*Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		result = append(result, sess)
	}
	return result
}

// ListExpired returns all sessions that have exceeded their TTL.
func (s *SessionStore) ListExpired() []*Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var expired []*Session
	for _, sess := range s.sessions {
		if sess.Expired() {
			expired = append(expired, sess)
		}
	}
	return expired
}

// Namespaces returns all session namespaces except the one specified.
func (s *SessionStore) Namespaces(exclude string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var ns []string
	for _, sess := range s.sessions {
		if sess.Namespace != exclude {
			ns = append(ns, sess.Namespace)
		}
	}
	return ns
}

// persistLocked writes sessions to disk. Caller must hold s.mu.
func (s *SessionStore) persistLocked() error {
	data, err := json.MarshalIndent(s.sessions, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}

func generateID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
