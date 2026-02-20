package github

import "context"

// MockClient is a test double for Client. Set per-method function fields
// to control behaviour in each test case; unset fields return nil error.
type MockClient struct {
	CreateRepoFn          func(ctx context.Context, org, name string, private bool) (*RepoInfo, error)
	SetBranchProtectionFn func(ctx context.Context, owner, repo, branch string, cfg BranchProtectionConfig) error
	CreateFileFn          func(ctx context.Context, owner, repo, path, message string, content []byte) error
}

func (m *MockClient) CreateRepo(ctx context.Context, org, name string, private bool) (*RepoInfo, error) {
	if m.CreateRepoFn != nil {
		return m.CreateRepoFn(ctx, org, name, private)
	}
	return &RepoInfo{
		Name:     name,
		CloneURL: "https://github.com/" + org + "/" + name + ".git",
		HTMLURL:  "https://github.com/" + org + "/" + name,
		Private:  private,
	}, nil
}

func (m *MockClient) SetBranchProtection(ctx context.Context, owner, repo, branch string, cfg BranchProtectionConfig) error {
	if m.SetBranchProtectionFn != nil {
		return m.SetBranchProtectionFn(ctx, owner, repo, branch, cfg)
	}
	return nil
}

func (m *MockClient) CreateFile(ctx context.Context, owner, repo, path, message string, content []byte) error {
	if m.CreateFileFn != nil {
		return m.CreateFileFn(ctx, owner, repo, path, message, content)
	}
	return nil
}
