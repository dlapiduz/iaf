package auth

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSession_Expired_NoTTL(t *testing.T) {
	sess := &Session{
		CreatedAt:      time.Now().Add(-48 * time.Hour),
		LastActivityAt: time.Now().Add(-48 * time.Hour),
		TTL:            0,
	}
	if sess.Expired() {
		t.Error("session with TTL=0 must never expire")
	}
}

func TestSession_Expired_WithinTTL(t *testing.T) {
	sess := &Session{
		CreatedAt:      time.Now().Add(-1 * time.Hour),
		LastActivityAt: time.Now().Add(-30 * time.Minute),
		TTL:            2 * time.Hour,
	}
	if sess.Expired() {
		t.Error("session active 30min ago with 2h TTL must not be expired")
	}
}

func TestSession_Expired_BeyondTTL(t *testing.T) {
	sess := &Session{
		CreatedAt:      time.Now().Add(-5 * time.Hour),
		LastActivityAt: time.Now().Add(-3 * time.Hour),
		TTL:            2 * time.Hour,
	}
	if !sess.Expired() {
		t.Error("session inactive for 3h with 2h TTL must be expired")
	}
}

func TestSession_Expired_ZeroLastActivity_UsesCreatedAt(t *testing.T) {
	sess := &Session{
		CreatedAt: time.Now().Add(-5 * time.Hour),
		// LastActivityAt is zero (old session loaded from JSON without this field)
		TTL: 2 * time.Hour,
	}
	if !sess.Expired() {
		t.Error("session with zero LastActivityAt should use CreatedAt as fallback")
	}
}

func TestTouch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	store, _ := NewSessionStore(path)
	sess, _ := store.Register("agent", 1*time.Hour)

	before := sess.LastActivityAt
	time.Sleep(2 * time.Millisecond)
	store.Touch(sess.ID)

	updated, ok := store.Lookup(sess.ID)
	if !ok {
		t.Fatal("session not found after touch")
	}
	if !updated.LastActivityAt.After(before) {
		t.Error("LastActivityAt should have been updated by Touch")
	}
}

func TestTouch_UnknownSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	store, _ := NewSessionStore(path)
	// Should not panic
	store.Touch("nonexistent-id")
}

func TestDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	store, _ := NewSessionStore(path)
	sess, _ := store.Register("agent", 0)

	if err := store.Delete(sess.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if _, ok := store.Lookup(sess.ID); ok {
		t.Error("session should not be found after Delete")
	}
}

func TestDelete_NotFound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	store, _ := NewSessionStore(path)
	if err := store.Delete("nonexistent"); err == nil {
		t.Error("expected error deleting nonexistent session")
	}
}

func TestListExpired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	store, _ := NewSessionStore(path)

	// Active session (will not expire)
	active, _ := store.Register("active", 24*time.Hour)

	// Expired session: manually inject with an old LastActivityAt
	id := "deadbeefdeadbeefdeadbeefdeadbeef"
	store.mu.Lock()
	store.sessions[id] = &Session{
		ID:             id,
		Namespace:      "iaf-" + id,
		Name:           "expired-agent",
		CreatedAt:      time.Now().Add(-10 * time.Hour),
		LastActivityAt: time.Now().Add(-10 * time.Hour),
		TTL:            1 * time.Hour,
	}
	store.mu.Unlock()

	expired := store.ListExpired()
	if len(expired) != 1 {
		t.Fatalf("expected 1 expired session, got %d", len(expired))
	}
	if expired[0].ID != id {
		t.Errorf("expected expired session %s, got %s", id, expired[0].ID)
	}

	// Active session must not be in the expired list
	for _, s := range expired {
		if s.ID == active.ID {
			t.Error("active session must not be listed as expired")
		}
	}
}

func TestList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	store, _ := NewSessionStore(path)
	store.Register("a", 0)
	store.Register("b", 0)
	store.Register("c", 0)

	all := store.List()
	if len(all) != 3 {
		t.Errorf("expected 3 sessions, got %d", len(all))
	}
}

func TestRegister_TTLPersistedAndLoaded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	store1, _ := NewSessionStore(path)
	sess, _ := store1.Register("agent", 24*time.Hour)

	store2, _ := NewSessionStore(path)
	loaded, ok := store2.Lookup(sess.ID)
	if !ok {
		t.Fatal("session not found after reload")
	}
	if loaded.TTL != 24*time.Hour {
		t.Errorf("expected TTL 24h, got %v", loaded.TTL)
	}
}
