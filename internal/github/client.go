// Package github provides a minimal client for the GitHub REST API v3.
// Only the operations needed by the setup_github_repo MCP tool are
// implemented. The Client interface is kept narrow so tests can inject
// a mock without a real API call.
package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// RepoInfo holds the fields from a GitHub repository response that IAF cares about.
type RepoInfo struct {
	Name     string
	CloneURL string
	HTMLURL  string
	Private  bool
}

// BranchProtectionConfig describes the branch protection rules to apply.
type BranchProtectionConfig struct {
	// RequiredReviewers is the number of required approving reviewers (0 = none).
	RequiredReviewers int
	// RequiredStatusChecks are the required CI check names (e.g. "CI / ci").
	RequiredStatusChecks []string
}

// Client abstracts the GitHub API calls made by the setup_github_repo tool.
type Client interface {
	// CreateRepo creates a new repository in org. auto_init=true is always set
	// so the repo has an initial commit (required for branch protection).
	CreateRepo(ctx context.Context, org, name string, private bool) (*RepoInfo, error)
	// SetBranchProtection applies branch protection rules to branch.
	SetBranchProtection(ctx context.Context, owner, repo, branch string, cfg BranchProtectionConfig) error
	// CreateFile creates or updates a file in the repository.
	CreateFile(ctx context.Context, owner, repo, path, message string, content []byte) error
}

// HTTPClient implements Client using GitHub REST API v3 and a Bearer token.
type HTTPClient struct {
	token   string
	baseURL string // overridable for tests; default "https://api.github.com"
	http    *http.Client
}

// NewHTTPClient creates an HTTPClient that authenticates with the given token.
// Required scope: repo.
func NewHTTPClient(token string) *HTTPClient {
	return &HTTPClient{
		token:   token,
		baseURL: "https://api.github.com",
		http:    &http.Client{},
	}
}

// SetBaseURL overrides the API base URL. Used in tests to point at a local
// httptest server.
func (c *HTTPClient) SetBaseURL(url string) {
	c.baseURL = url
}

// CreateRepo calls POST /orgs/{org}/repos.
func (c *HTTPClient) CreateRepo(ctx context.Context, org, name string, private bool) (*RepoInfo, error) {
	body, _ := json.Marshal(map[string]any{
		"name":      name,
		"private":   private,
		"auto_init": true, // creates initial commit so branch protection can be set
	})

	resp, err := c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/orgs/%s/repos", org), body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnprocessableEntity {
		return nil, fmt.Errorf("repository %q already exists in org %q — choose a different name", name, org)
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, c.apiError(resp, "create repository")
	}

	var result struct {
		Name     string `json:"name"`
		CloneURL string `json:"clone_url"`
		HTMLURL  string `json:"html_url"`
		Private  bool   `json:"private"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding create-repo response: %w", err)
	}

	return &RepoInfo{
		Name:     result.Name,
		CloneURL: result.CloneURL,
		HTMLURL:  result.HTMLURL,
		Private:  result.Private,
	}, nil
}

// SetBranchProtection calls PUT /repos/{owner}/{repo}/branches/{branch}/protection.
func (c *HTTPClient) SetBranchProtection(ctx context.Context, owner, repo, branch string, cfg BranchProtectionConfig) error {
	// Build required_status_checks.
	var statusChecks any
	if len(cfg.RequiredStatusChecks) > 0 {
		statusChecks = map[string]any{
			"strict":   true,
			"contexts": cfg.RequiredStatusChecks,
		}
	}

	// Build required_pull_request_reviews.
	var prReviews any
	if cfg.RequiredReviewers > 0 {
		prReviews = map[string]any{
			"required_approving_review_count": cfg.RequiredReviewers,
		}
	}

	body, _ := json.Marshal(map[string]any{
		"required_status_checks":        statusChecks,
		"enforce_admins":                false,
		"required_pull_request_reviews": prReviews,
		"restrictions":                  nil,
	})

	resp, err := c.doJSON(ctx, http.MethodPut,
		fmt.Sprintf("/repos/%s/%s/branches/%s/protection", owner, repo, branch), body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.apiError(resp, "set branch protection")
	}
	return nil
}

// CreateFile calls PUT /repos/{owner}/{repo}/contents/{path}.
func (c *HTTPClient) CreateFile(ctx context.Context, owner, repo, path, message string, content []byte) error {
	body, _ := json.Marshal(map[string]any{
		"message": message,
		"content": base64.StdEncoding.EncodeToString(content),
	})

	resp, err := c.doJSON(ctx, http.MethodPut,
		fmt.Sprintf("/repos/%s/%s/contents/%s", owner, repo, path), body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return c.apiError(resp, "create file")
	}

	// Check rate limit header as a courtesy warning.
	if resp.Header.Get("X-RateLimit-Remaining") == "0" {
		return fmt.Errorf("GitHub API rate limit exhausted; try again after the reset window")
	}
	return nil
}

// doJSON performs an authenticated JSON request to the GitHub API.
func (c *HTTPClient) doJSON(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request failed: %w", err)
	}
	return resp, nil
}

// apiError reads the response body and returns a descriptive error.
// The token is never included in the error message.
func (c *HTTPClient) apiError(resp *http.Response, op string) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var ghErr struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &ghErr)

	if ghErr.Message != "" {
		return fmt.Errorf("%s: GitHub API returned %d: %s", op, resp.StatusCode, ghErr.Message)
	}
	return fmt.Errorf("%s: GitHub API returned %d", op, resp.StatusCode)
}
