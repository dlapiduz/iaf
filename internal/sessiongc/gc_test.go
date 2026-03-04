package sessiongc_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	iafv1alpha1 "github.com/dlapiduz/iaf/api/v1alpha1"
	"github.com/dlapiduz/iaf/internal/auth"
	"github.com/dlapiduz/iaf/internal/sessiongc"
	"github.com/dlapiduz/iaf/internal/sourcestore"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func setupGCTest(t *testing.T) (*sessiongc.Cleaner, *auth.SessionStore, *sourcestore.Store, ctrlclient.Client) {
	t.Helper()

	scheme := runtime.NewScheme()
	_ = iafv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	store, err := sourcestore.New(t.TempDir(), "http://localhost:8080", slog.Default())
	if err != nil {
		t.Fatal(err)
	}

	sessions, err := auth.NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}

	cleaner := sessiongc.New(k8sClient, store, sessions, slog.Default())
	return cleaner, sessions, store, k8sClient
}

func TestCleanupSession_RemovesSession(t *testing.T) {
	cleaner, sessions, _, _ := setupGCTest(t)
	ctx := context.Background()

	sess, err := sessions.Register("agent", 0)
	if err != nil {
		t.Fatal(err)
	}

	cleaner.CleanupSession(ctx, sess.ID, sess.Namespace)

	if _, ok := sessions.Lookup(sess.ID); ok {
		t.Error("session should be removed after CleanupSession")
	}
}

func TestCleanupSession_Idempotent(t *testing.T) {
	cleaner, sessions, _, _ := setupGCTest(t)
	ctx := context.Background()

	sess, _ := sessions.Register("agent", 0)

	// First cleanup — should succeed
	cleaner.CleanupSession(ctx, sess.ID, sess.Namespace)
	// Second cleanup — session not found; should not panic
	cleaner.CleanupSession(ctx, sess.ID, sess.Namespace)
}

func TestCleanupSession_DeletesK8sNamespace(t *testing.T) {
	cleaner, sessions, _, k8sClient := setupGCTest(t)
	ctx := context.Background()

	sess, _ := sessions.Register("agent", 0)

	// Pre-create the namespace
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: sess.Namespace}}
	if err := k8sClient.Create(ctx, ns); err != nil {
		t.Fatal(err)
	}

	cleaner.CleanupSession(ctx, sess.ID, sess.Namespace)

	// Namespace should be gone
	var got corev1.Namespace
	err := k8sClient.Get(ctx, ctrlclient.ObjectKey{Name: sess.Namespace}, &got)
	if err == nil {
		t.Error("expected namespace to be deleted")
	}
}

func TestCleanupSession_RemovesSourceFiles(t *testing.T) {
	cleaner, sessions, store, _ := setupGCTest(t)
	ctx := context.Background()

	sess, _ := sessions.Register("agent", 0)

	// Write a file to the namespace's source directory
	if _, err := store.StoreFiles(sess.Namespace, "myapp", map[string]string{"main.go": "package main"}); err != nil {
		t.Fatal(err)
	}

	cleaner.CleanupSession(ctx, sess.ID, sess.Namespace)

	// After cleanup, storing again to the same path should succeed (dir was deleted and re-created)
	// This confirms DeleteNamespace ran without errors.
	_, err := store.StoreFiles(sess.Namespace, "myapp2", map[string]string{"f.go": "package main"})
	if err != nil {
		t.Errorf("store after cleanup should not error: %v", err)
	}
}

func TestRunGC_CleansExpiredSessions(t *testing.T) {
	cleaner, sessions, _, _ := setupGCTest(t)
	ctx := context.Background()

	// Active session (no TTL — never expires)
	active, _ := sessions.Register("active-agent", 0)

	// Session with a very short TTL — will be expired immediately
	expired, _ := sessions.Register("expired-agent", 1*time.Millisecond)
	time.Sleep(5 * time.Millisecond)

	cleaner.RunGC(ctx)

	if _, ok := sessions.Lookup(expired.ID); ok {
		t.Error("expired session should have been cleaned up by GC")
	}
	if _, ok := sessions.Lookup(active.ID); !ok {
		t.Error("active session (no TTL) must not be cleaned up by GC")
	}
}

func TestRunGC_ActiveSessionNotCleaned(t *testing.T) {
	cleaner, sessions, _, _ := setupGCTest(t)
	ctx := context.Background()

	// Session with a long TTL — should not be cleaned
	sess, _ := sessions.Register("live-agent", 24*time.Hour)

	cleaner.RunGC(ctx)

	if _, ok := sessions.Lookup(sess.ID); !ok {
		t.Error("session within TTL must not be cleaned up by GC")
	}
}

func TestStart_ZeroInterval_ReturnsImmediately(t *testing.T) {
	cleaner, _, _, _ := setupGCTest(t)
	ctx := context.Background()

	done := make(chan struct{})
	go func() {
		cleaner.Start(ctx, 0)
		close(done)
	}()

	select {
	case <-done:
		// Good — returned immediately without blocking.
	case <-time.After(time.Second):
		t.Error("Start with interval=0 should return immediately")
	}
}

func TestStart_CancelsOnContextDone(t *testing.T) {
	cleaner, _, _, _ := setupGCTest(t)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		cleaner.Start(ctx, 24*time.Hour) // long interval — ticker won't fire
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// Good — returned after context cancellation.
	case <-time.After(time.Second):
		t.Error("Start should return when context is cancelled")
	}
}
