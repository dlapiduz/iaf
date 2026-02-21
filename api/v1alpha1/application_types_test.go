package v1alpha1

import "testing"

func boolPtr(b bool) *bool { return &b }

func TestIsTLSEnabled(t *testing.T) {
	tests := []struct {
		name     string
		app      *Application
		expected bool
	}{
		{
			name:     "nil TLS config defaults to enabled",
			app:      &Application{},
			expected: true,
		},
		{
			name:     "nil Enabled field defaults to enabled",
			app:      &Application{Spec: ApplicationSpec{TLS: &TLSConfig{}}},
			expected: true,
		},
		{
			name:     "explicit true",
			app:      &Application{Spec: ApplicationSpec{TLS: &TLSConfig{Enabled: boolPtr(true)}}},
			expected: true,
		},
		{
			name:     "explicit false",
			app:      &Application{Spec: ApplicationSpec{TLS: &TLSConfig{Enabled: boolPtr(false)}}},
			expected: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsTLSEnabled(tt.app)
			if got != tt.expected {
				t.Errorf("IsTLSEnabled() = %v, want %v", got, tt.expected)
			}
		})
	}
}
