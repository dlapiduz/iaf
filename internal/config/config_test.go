package config

import (
	"os"
	"testing"
)

// TestLoad_TLSIssuerDefaultsToEmpty is a regression test for the bug where
// the default TLS issuer was "selfsigned-issuer" (non-empty). This caused every
// controller reconcile to fail with "no matches for kind Certificate" on clusters
// without cert-manager, preventing apps from ever reaching Running state.
func TestLoad_TLSIssuerDefaultsToEmpty(t *testing.T) {
	os.Unsetenv("IAF_TLS_ISSUER")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TLSIssuer != "" {
		t.Errorf("TLSIssuer must default to empty string so cert-manager is optional; got %q — "+
			"a non-empty default causes every reconcile to fail on clusters without cert-manager", cfg.TLSIssuer)
	}
}

// TestLoad_TLSIssuerRespectedWhenSet verifies that operators can opt in to
// cert-manager TLS by setting IAF_TLS_ISSUER.
func TestLoad_TLSIssuerRespectedWhenSet(t *testing.T) {
	t.Setenv("IAF_TLS_ISSUER", "selfsigned-issuer")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TLSIssuer != "selfsigned-issuer" {
		t.Errorf("expected TLSIssuer=%q, got %q", "selfsigned-issuer", cfg.TLSIssuer)
	}
}
