package validation_test

import (
	"strings"
	"testing"

	"github.com/dlapiduz/iaf/internal/validation"
)

func TestValidateAppName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid simple", "myapp", false},
		{"valid with hyphens", "my-app-123", false},
		{"valid single char", "a", false},
		{"valid 63 chars", strings.Repeat("a", 63), false},
		{"empty", "", true},
		{"too long", strings.Repeat("a", 64), true},
		{"uppercase", "MyApp", true},
		{"starts with hyphen", "-myapp", true},
		{"starts with digit", "1app", false},
		{"reserved kube prefix", "kube-system", true},
		{"reserved iaf prefix", "iaf-agent", true},
		{"spaces", "my app", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validation.ValidateAppName(tc.input)
			if tc.wantErr && err == nil {
				t.Errorf("expected error for input %q, got nil", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error for input %q: %v", tc.input, err)
			}
		})
	}
}

func TestValidateEnvVarName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid upper snake", "MY_VAR", false},
		{"valid lower", "my_var", false},
		{"valid starts with underscore", "_VAR", false},
		{"valid with digits", "VAR_123", false},
		{"empty", "", true},
		{"starts with digit", "1VAR", true},
		{"contains hyphen", "MY-VAR", true},
		{"contains space", "MY VAR", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validation.ValidateEnvVarName(tc.input)
			if tc.wantErr && err == nil {
				t.Errorf("expected error for input %q, got nil", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error for input %q: %v", tc.input, err)
			}
		})
	}
}

func TestValidateGitHubRepoName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid simple", "my-repo", false},
		{"valid underscores", "my_repo", false},
		{"valid dots", "my.repo", false},
		{"valid mixed", "My-Repo_1.0", false},
		{"valid 100 chars", strings.Repeat("a", 100), false},
		{"empty", "", true},
		{"too long 101", strings.Repeat("a", 101), true},
		{"starts with dot", ".hidden", true},
		{"starts with hyphen", "-repo", true},
		{"ends with dot", "repo.", true},
		{"ends with hyphen", "repo-", true},
		{"double dot", "my..repo", true},
		{"spaces", "my repo", true},
		{"slash", "my/repo", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validation.ValidateGitHubRepoName(tc.input)
			if tc.wantErr && err == nil {
				t.Errorf("expected error for input %q, got nil", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error for input %q: %v", tc.input, err)
			}
		})
	}
}
