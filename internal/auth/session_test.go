package auth

import (
	"path/filepath"
	"sync"
	"testing"
)

func TestRegisterAndLookup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	store, err := NewSessionStore(path)
	if err != nil {
		t.Fatal(err)
	}

	sess, err := store.Register("my-project")
	if err != nil {
		t.Fatal(err)
	}

	if sess.ID == "" {
		t.Fatal("expected non-empty session ID")
	}
	if len(sess.ID) != 32 {
		t.Errorf("expected 32-char hex session ID (128-bit), got %d chars: %s", len(sess.ID), sess.ID)
	}
	if sess.Namespace != "iaf-"+sess.ID {
		t.Errorf("expected namespace iaf-%s, got %s", sess.ID, sess.Namespace)
	}
	if sess.Name != "my-project" {
		t.Errorf("expected name my-project, got %s", sess.Name)
	}
	if sess.CreatedAt.IsZero() {
		t.Error("expected non-zero created_at")
	}

	// Lookup should find it
	found, ok := store.Lookup(sess.ID)
	if !ok {
		t.Fatal("expected to find session")
	}
	if found.Namespace != sess.Namespace {
		t.Errorf("expected namespace %s, got %s", sess.Namespace, found.Namespace)
	}

	// Lookup missing session
	_, ok = store.Lookup("nonexistent")
	if ok {
		t.Error("expected not found for nonexistent session")
	}
}

func TestSessionPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")

	// Create store and register a session
	store1, err := NewSessionStore(path)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store1.Register("test")
	if err != nil {
		t.Fatal(err)
	}

	// Create a new store from the same file — should load the session
	store2, err := NewSessionStore(path)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := store2.Lookup(sess.ID)
	if !ok {
		t.Fatal("expected session to survive reload")
	}
	if found.Name != "test" {
		t.Errorf("expected name test, got %s", found.Name)
	}
}

func TestRegisterEmptyName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	store, err := NewSessionStore(path)
	if err != nil {
		t.Fatal(err)
	}

	sess, err := store.Register("")
	if err != nil {
		t.Fatal(err)
	}
	if sess.Name != "" {
		t.Errorf("expected empty name, got %s", sess.Name)
	}
	if sess.Namespace == "" {
		t.Error("expected non-empty namespace even with empty name")
	}
}

func TestConcurrentRegister(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	store, err := NewSessionStore(path)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	sessions := make([]*Session, 20)
	errs := make([]error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sessions[idx], errs[idx] = store.Register("")
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("register %d failed: %v", i, err)
		}
	}

	// All IDs should be unique
	ids := make(map[string]bool)
	for _, s := range sessions {
		if s == nil {
			continue
		}
		if ids[s.ID] {
			t.Errorf("duplicate session ID: %s", s.ID)
		}
		ids[s.ID] = true
	}
}
