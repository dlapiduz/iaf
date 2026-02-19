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
		errContains string
	}{
		// Valid names
		{name: "simple lowercase", input: "myapp", wantErr: false},
		{name: "with hyphen", input: "my-app", wantErr: false},
		{name: "with digits", input: "app1", wantErr: false},
		{name: "starts with digit", input: "1app", wantErr: false},
		{name: "63 chars", input: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"[:63], wantErr: false},
		{name: "single char", input: "a", wantErr: false},
		{name: "alphanumeric-hyphen mix", input: "hello-world-123", wantErr: false},

		// Invalid names
		{name: "empty", input: "", wantErr: true, errContains: "required"},
		{name: "uppercase", input: "MyApp", wantErr: true, errContains: "must match"},
		{name: "leading hyphen", input: "-myapp", wantErr: true, errContains: "must match"},
		{name: "space", input: "my app", wantErr: true, errContains: "must match"},
		{name: "underscore", input: "my_app", wantErr: true, errContains: "must match"},
		{name: "64 chars", input: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"[:64], wantErr: true, errContains: "63 characters"},
		{name: "reserved kube-", input: "kube-system", wantErr: true, errContains: "reserved prefix"},
		{name: "reserved kube- prefix", input: "kube-myapp", wantErr: true, errContains: "reserved prefix"},
		{name: "reserved iaf-", input: "iaf-system", wantErr: true, errContains: "reserved prefix"},
		{name: "reserved iaf- prefix", input: "iaf-myapp", wantErr: true, errContains: "reserved prefix"},
		{name: "dot", input: "my.app", wantErr: true, errContains: "must match"},
		{name: "slash", input: "my/app", wantErr: true, errContains: "must match"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validation.ValidateAppName(tt.input)
			if tt.wantErr && err == nil {
				t.Errorf("ValidateAppName(%q) = nil, want error", tt.input)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateAppName(%q) = %v, want nil", tt.input, err)
			}
			if tt.wantErr && tt.errContains != "" && err != nil {
				if !containsSubstring(err.Error(), tt.errContains) {
					t.Errorf("ValidateAppName(%q) error = %q, want it to contain %q", tt.input, err.Error(), tt.errContains)
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
		errContains string
	}{
		// Valid names
		{name: "uppercase", input: "MY_VAR", wantErr: false},
		{name: "lowercase", input: "my_var", wantErr: false},
		{name: "leading underscore", input: "_PRIVATE", wantErr: false},
		{name: "with digits", input: "VAR1", wantErr: false},
		{name: "mixed case with digits", input: "My_Var_2", wantErr: false},
		{name: "single letter", input: "X", wantErr: false},

		// Invalid names
		{name: "empty", input: "", wantErr: true, errContains: "required"},
		{name: "starts with digit", input: "1VAR", wantErr: true, errContains: "must match"},
		{name: "starts with hyphen", input: "-VAR", wantErr: true, errContains: "must match"},
		{name: "contains hyphen", input: "MY-VAR", wantErr: true, errContains: "must match"},
		{name: "contains space", input: "MY VAR", wantErr: true, errContains: "must match"},
		{name: "contains dot", input: "MY.VAR", wantErr: true, errContains: "must match"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validation.ValidateEnvVarName(tt.input)
			if tt.wantErr && err == nil {
				t.Errorf("ValidateEnvVarName(%q) = nil, want error", tt.input)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateEnvVarName(%q) = %v, want nil", tt.input, err)
			}
			if tt.wantErr && tt.errContains != "" && err != nil {
				if !containsSubstring(err.Error(), tt.errContains) {
					t.Errorf("ValidateEnvVarName(%q) error = %q, want it to contain %q", tt.input, err.Error(), tt.errContains)
				}
			}
		})
	}
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || containsAt(s, sub))
}

func containsAt(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
