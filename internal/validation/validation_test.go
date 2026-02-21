package validation_test

import (
	"testing"

	"github.com/dlapiduz/iaf/internal/validation"
)

func TestValidateAppName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		errMsg  string
	}{
		// Valid
		{"simple lowercase", "myapp", false, ""},
		{"with hyphens", "my-app", false, ""},
		{"with digits", "app1", false, ""},
		{"starts with digit", "1app", false, ""},
		{"max length 63", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"[:63], false, ""},
		{"single char", "a", false, ""},

		// Invalid
		{"empty", "", true, "app name is required"},
		{"too long", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", true, "63 characters or less"},
		{"uppercase", "MyApp", true, "must match"},
		{"leading hyphen", "-myapp", true, "must match"},
		{"trailing hyphen", "myapp-", true, "must match"},
		{"underscore", "my_app", true, "must match"},
		{"spaces", "my app", true, "must match"},
		{"reserved kube-", "kube-system", true, `reserved prefix "kube-"`},
		{"reserved iaf-", "iaf-controller", true, `reserved prefix "iaf-"`},
		{"reserved iaf- short", "iaf-x", true, `reserved prefix "iaf-"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validation.ValidateAppName(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errMsg)
					return
				}
				if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, got %q", err.Error())
				}
			}
		})
	}
}

func TestValidateEnvVarName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		errMsg  string
	}{
		// Valid
		{"simple uppercase", "PORT", false, ""},
		{"lowercase", "port", false, ""},
		{"underscore prefix", "_INTERNAL", false, ""},
		{"with digits", "PORT_8080", false, ""},
		{"mixed case", "MyVar", false, ""},

		// Invalid
		{"empty", "", true, "env var name is required"},
		{"starts with digit", "1PORT", true, "must match"},
		{"hyphen", "MY-VAR", true, "must match"},
		{"space", "MY VAR", true, "must match"},
		{"dot", "my.var", true, "must match"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validation.ValidateEnvVarName(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errMsg)
					return
				}
				if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, got %q", err.Error())
				}
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}
