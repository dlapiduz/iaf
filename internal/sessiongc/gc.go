// Package sessiongc provides background garbage collection for expired agent sessions.
// It deletes the session's Kubernetes namespace (cascading to all resources within),
// cleans up source tarballs, and removes the session from the store.
package sessiongc

import (
	"context"
	"log/slog"
	"time"

	"github.com/dlapiduz/iaf/internal/auth"
	"github.com/dlapiduz/iaf/internal/sourcestore"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Cleaner handles session cleanup.
type Cleaner struct {
	client   client.Client
	store    *sourcestore.Store
	sessions *auth.SessionStore
	logger   *slog.Logger
}

// New creates a new Cleaner.
func New(c client.Client, store *sourcestore.Store, sessions *auth.SessionStore, logger *slog.Logger) *Cleaner {
	return &Cleaner{
		client:   c,
		store:    store,
		sessions: sessions,
		logger:   logger,
	}
}

// CleanupSession deletes all resources associated with a session.
// It is idempotent — not-found errors are ignored.
func (cl *Cleaner) CleanupSession(ctx context.Context, sessionID, namespace string) {
	cl.logger.Warn("deleting session namespace",
		"session_id", sessionID,
		"namespace", namespace,
	)

	// Delete the Kubernetes namespace — this cascades to all resources within it.
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}
	if err := cl.client.Delete(ctx, ns); err != nil && !apierrors.IsNotFound(err) {
		cl.logger.Error("failed to delete namespace",
			"namespace", namespace,
			"error", err,
		)
		// Continue cleanup even if namespace deletion fails.
	}

	// Remove source tarballs for the namespace.
	if err := cl.store.DeleteNamespace(namespace); err != nil {
		cl.logger.Error("failed to delete source store namespace",
			"namespace", namespace,
			"error", err,
		)
	}

	// Remove the session from the store.
	if err := cl.sessions.Delete(sessionID); err != nil {
		cl.logger.Error("failed to delete session",
			"session_id", sessionID,
			"error", err,
		)
	}
}

// RunGC runs one garbage-collection pass: finds expired sessions and cleans them up.
func (cl *Cleaner) RunGC(ctx context.Context) {
	expired := cl.sessions.ListExpired()
	if len(expired) == 0 {
		return
	}
	cl.logger.Info("GC: cleaning up expired sessions", "count", len(expired))
	for _, sess := range expired {
		cl.CleanupSession(ctx, sess.ID, sess.Namespace)
	}
}

// Start runs the GC on a ticker. It blocks until ctx is cancelled.
// If interval is zero, Start returns immediately without running GC.
func (cl *Cleaner) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cl.RunGC(ctx)
		}
	}
}
