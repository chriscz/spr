package config_parser

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ejoffe/spr/config"
	"github.com/ejoffe/spr/git"
	"github.com/ejoffe/spr/git/mockgit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeGit is a minimal git.GitInterface that serves canned responses for
// specific commands, used when the mock's public API doesn't expose the
// needed expectation helper (e.g. "git status -b --porcelain -u no").
type fakeGit struct {
	rootDir  string
	commands []fakeCmd
	pos      int
	t        testing.TB
}

type fakeCmd struct {
	cmd    string
	output string
	err    error
}

func (f *fakeGit) RootDir() string { return f.rootDir }

func (f *fakeGit) Git(args string, output *string) error {
	if f.pos >= len(f.commands) {
		f.t.Fatalf("fakeGit: unexpected command: git %s", args)
	}
	fc := f.commands[f.pos]
	f.pos++
	if fc.cmd != "git "+args {
		f.t.Fatalf("fakeGit: expected %q, got %q", fc.cmd, "git "+args)
	}
	if output != nil {
		*output = fc.output
	}
	return fc.err
}

func (f *fakeGit) MustGit(args string, output *string) {
	if err := f.Git(args, output); err != nil {
		panic(err)
	}
}

func (f *fakeGit) GitWithEditor(args string, output *string, _ string) error {
	return f.Git(args, output)
}

func (f *fakeGit) DeleteRemoteBranch(_ context.Context, branch string) error {
	return f.Git(fmt.Sprintf("DeleteRemoteBranch(%s)", branch), nil)
}

func (f *fakeGit) expectationsMet() {
	if f.pos != len(f.commands) {
		f.t.Errorf("fakeGit: %d command(s) left unconsumed", len(f.commands)-f.pos)
	}
}

// newFakeGit constructs a fakeGit seeded with a remote-v and status-b response,
// which is the pair that ParseConfig and remoteBranch.Load require.
func newFakeGit(t testing.TB, rootDir, remote, statusBLine string) *fakeGit {
	remoteOutput := fmt.Sprintf("origin  %s (fetch)\norigin  %s (push)\n", remote, remote)
	return &fakeGit{
		t:       t,
		rootDir: rootDir,
		commands: []fakeCmd{
			{cmd: "git remote -v", output: remoteOutput},
			{cmd: "git status -b --porcelain -u no", output: statusBLine},
		},
	}
}

// compile-time check
var _ git.GitInterface = (*fakeGit)(nil)

func TestGetRepoDetailsFromRemote(t *testing.T) {
	type testCase struct {
		remote     string
		githubHost string
		repoOwner  string
		repoName   string
		match      bool
	}
	testCases := []testCase{
		{"origin  https://github.com/r2/d2.git (push)", "github.com", "r2", "d2", true},
		{"origin  https://github.com/r2/d2.git (fetch)", "", "", "", false},
		{"origin  https://github.com/r2/d2 (push)", "github.com", "r2", "d2", true},
		{"origin  https://github.com/r-2/d-2 (push)", "github.com", "r-2", "d-2", true},

		{"origin  ssh://git@github.com/r2/d2.git (push)", "github.com", "r2", "d2", true},
		{"origin  ssh://git@github.com/r2/d2.git (fetch)", "", "", "", false},
		{"origin  ssh://git@github.com/r2/d2 (push)", "github.com", "r2", "d2", true},
		{"origin  ssh://git@github.com/r-2/d-2 (push)", "github.com", "r-2", "d-2", true},

		{"origin  git@github.com:r2/d2.git (push)", "github.com", "r2", "d2", true},
		{"origin  git@github.com:r2/d2.git (fetch)", "", "", "", false},
		{"origin  git@github.com:r2/d2 (push)", "github.com", "r2", "d2", true},
		{"origin  git@github.com:r-2/d-2 (push)", "github.com", "r-2", "d-2", true},

		{"origin  git@gh.enterprise.com:r2/d2.git (push)", "gh.enterprise.com", "r2", "d2", true},
		{"origin  git@gh.enterprise.com:r2/d2.git (fetch)", "", "", "", false},
		{"origin  git@gh.enterprise.com:r2/d2 (push)", "gh.enterprise.com", "r2", "d2", true},
		{"origin  git@gh.enterprise.com:r-2/d-2 (push)", "gh.enterprise.com", "r-2", "d-2", true},

		{"origin  https://github.com/r2/d2-a.git (push)", "github.com", "r2", "d2-a", true},
		{"origin  https://github.com/r-2/d2-a.git (push)", "github.com", "r-2", "d2-a", true},
		{"origin  https://github.com/r2/d2_a.git (push)", "github.com", "r2", "d2_a", true},
		{"origin  https://github.com/r-2/d2_a.git (push)", "github.com", "r-2", "d2_a", true},

		// GitHub names are case-sensitive
		{"origin  https://github.com/R2/D2.git (push)", "github.com", "R2", "D2", true},

		// GitHub allows "." in repo names (issue #431). The optional ".git"
		// suffix must NOT be swallowed into the repo name when a "." is present.
		{"origin  https://github.com/r2/my.repo (push)", "github.com", "r2", "my.repo", true},
		{"origin  https://github.com/r2/my.repo.git (push)", "github.com", "r2", "my.repo", true},
		{"origin  ssh://git@github.com/r2/my.repo.git (push)", "github.com", "r2", "my.repo", true},
		{"origin  git@github.com:r2/my.repo.git (push)", "github.com", "r2", "my.repo", true},
		{"origin  git@github.com:r2/my.repo (push)", "github.com", "r2", "my.repo", true},
		{"origin  git@gh.enterprise.com:r2/a.b.c.git (push)", "gh.enterprise.com", "r2", "a.b.c", true},
		// A repo literally named "spr.git" (trailing ".git" plus suffix) must
		// strip only the suffix, not the name.
		{"origin  https://github.com/r2/spr.git.git (push)", "github.com", "r2", "spr.git", true},
	}
	for i, testCase := range testCases {
		t.Logf("Testing %v %q", i, testCase.remote)
		githubHost, repoOwner, repoName, match := getRepoDetailsFromRemote(testCase.remote)
		if githubHost != testCase.githubHost {
			t.Fatalf("Wrong \"githubHost\" returned for test case %v, expected %q, got %q", i, testCase.githubHost, githubHost)
		}
		if repoOwner != testCase.repoOwner {
			t.Fatalf("Wrong \"repoOwner\" returned for test case %v, expected %q, got %q", i, testCase.repoOwner, repoOwner)
		}
		if repoName != testCase.repoName {
			t.Fatalf("Wrong \"repoName\" returned for test case %v, expected %q, got %q", i, testCase.repoName, repoName)
		}
		if match != testCase.match {
			t.Fatalf("Wrong \"match\" returned for test case %v, expected %t, got %t", i, testCase.match, match)
		}
	}
}

func TestGitHubRemoteSource(t *testing.T) {
	mock := mockgit.NewMockGit(t)
	mock.ExpectRemote("https://github.com/r2/d2.git")

	expect := config.Config{
		Repo: &config.RepoConfig{
			GitHubRepoOwner: "r2",
			GitHubRepoName:  "d2",
			GitHubHost:      "github.com",
			RequireChecks:   false,
			RequireApproval: false,
			MergeMethod:     "",
		},
		User: &config.UserConfig{
			ShowPRLink:       false,
			LogGitCommands:   false,
			LogGitHubCalls:   false,
			StatusBitsHeader: false,
		},
	}

	actual := config.Config{
		Repo: &config.RepoConfig{},
		User: &config.UserConfig{},
	}
	source := NewGitHubRemoteSource(&actual, mock)
	source.Load(nil)
	assert.Equal(t, expect, actual)
	mock.ExpectationsMet()
}

// ---------------------------------------------------------------------------
// RepoConfigFilePath
// ---------------------------------------------------------------------------

func TestRepoConfigFilePath(t *testing.T) {
	mock := mockgit.NewMockGit(t)
	rootDir := t.TempDir()
	mock.SetRootDir(rootDir)

	got := RepoConfigFilePath(mock)

	want := filepath.Join(rootDir, ".spr.yml")
	assert.Equal(t, want, got)
}

// ---------------------------------------------------------------------------
// UserConfigFilePath
// ---------------------------------------------------------------------------

func TestUserConfigFilePath(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	got := UserConfigFilePath()

	want := filepath.Join(homeDir, ".spr.yml")
	assert.Equal(t, want, got)
}

// ---------------------------------------------------------------------------
// InternalConfigFilePath
// ---------------------------------------------------------------------------

func TestInternalConfigFilePath(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	got := InternalConfigFilePath()

	want := filepath.Join(homeDir, ".spr.state")
	assert.Equal(t, want, got)
}

// ---------------------------------------------------------------------------
// CheckConfig
// ---------------------------------------------------------------------------

func TestCheckConfig_ValidBranch(t *testing.T) {
	cfg := config.EmptyConfig()
	cfg.Repo.GitHubBranch = "main"

	err := CheckConfig(cfg)

	assert.NoError(t, err)
}

func TestCheckConfig_BranchWithSlash(t *testing.T) {
	cfg := config.EmptyConfig()
	cfg.Repo.GitHubBranch = "feature/main"

	err := CheckConfig(cfg)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "branch")
}

func TestCheckConfig_EmptyBranch(t *testing.T) {
	cfg := config.EmptyConfig()
	cfg.Repo.GitHubBranch = ""

	err := CheckConfig(cfg)

	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// NewRemoteBranchSource / remoteBranch.Load
// ---------------------------------------------------------------------------

func TestRemoteBranchSource_MatchesAndSetsFields(t *testing.T) {
	fg := &fakeGit{
		t:       t,
		rootDir: t.TempDir(),
		commands: []fakeCmd{
			{cmd: "git status -b --porcelain -u no", output: "## main...origin/main\n"},
		},
	}

	source := NewRemoteBranchSource(fg)
	repoCfg := &config.RepoConfig{}
	source.Load(repoCfg)

	assert.Equal(t, "origin", repoCfg.GitHubRemote)
	assert.Equal(t, "main", repoCfg.GitHubBranch)
	fg.expectationsMet()
}

func TestRemoteBranchSource_NoMatch(t *testing.T) {
	fg := &fakeGit{
		t:       t,
		rootDir: t.TempDir(),
		commands: []fakeCmd{
			{cmd: "git status -b --porcelain -u no", output: "?? untracked\n"},
		},
	}

	source := NewRemoteBranchSource(fg)
	repoCfg := &config.RepoConfig{}
	source.Load(repoCfg)

	// Fields should remain at their zero values when regex doesn't match.
	assert.Equal(t, "", repoCfg.GitHubRemote)
	assert.Equal(t, "", repoCfg.GitHubBranch)
	fg.expectationsMet()
}

func TestRemoteBranchSource_DoesNotOverwriteExistingBranch(t *testing.T) {
	fg := &fakeGit{
		t:       t,
		rootDir: t.TempDir(),
		commands: []fakeCmd{
			{cmd: "git status -b --porcelain -u no", output: "## main...origin/auto-detected\n"},
		},
	}

	source := NewRemoteBranchSource(fg)
	repoCfg := &config.RepoConfig{GitHubBranch: "already-set"}
	source.Load(repoCfg)

	// Remote should be set but branch must not be overwritten.
	assert.Equal(t, "origin", repoCfg.GitHubRemote)
	assert.Equal(t, "already-set", repoCfg.GitHubBranch)
	fg.expectationsMet()
}

// ---------------------------------------------------------------------------
// ParseConfig — happy path
// ---------------------------------------------------------------------------

func TestParseConfig_HappyPath(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	rootDir := t.TempDir()
	// ParseConfig calls rake.LoadSources(cfg.Repo, ...) which invokes:
	//   1. rake.DefaultSource()                   — no git calls
	//   2. NewGitHubRemoteSource(...)             — git remote -v
	//   3. rake.YamlFileSource(RepoConfigFilePath) — no git calls (file absent)
	//   4. NewRemoteBranchSource(...)             — git status -b --porcelain -u no
	// Then user + state yaml sources (files absent → no git calls).
	// Then state is written via YamlFileWriter — no git calls.
	// Then repo/user yamls may be created — no git calls.
	fg := newFakeGit(t, rootDir,
		"https://github.com/r2/d2.git",
		"## main...origin/main\n",
	)

	cfg := ParseConfig(fg)

	require.NotNil(t, cfg)
	assert.Equal(t, "github.com", cfg.Repo.GitHubHost)
	assert.Equal(t, "r2", cfg.Repo.GitHubRepoOwner)
	assert.Equal(t, "d2", cfg.Repo.GitHubRepoName)
	// RunCount must have been incremented from 0 to 1.
	assert.Equal(t, 1, cfg.State.RunCount)

	// State file must have been written.
	statePath := filepath.Join(homeDir, ".spr.state")
	_, err := os.Stat(statePath)
	assert.NoError(t, err, "state file should have been created")

	// Repo config file must have been created (init case).
	repoConfigPath := filepath.Join(rootDir, ".spr.yml")
	_, err = os.Stat(repoConfigPath)
	assert.NoError(t, err, "repo config file should have been created")

	// User config file must have been created (init case).
	userConfigPath := filepath.Join(homeDir, ".spr.yml")
	_, err = os.Stat(userConfigPath)
	assert.NoError(t, err, "user config file should have been created")

	fg.expectationsMet()
}

func TestParseConfig_RunCountIncrements(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	rootDir := t.TempDir()

	// First call — RunCount goes 0 → 1.
	fg1 := newFakeGit(t, rootDir,
		"https://github.com/r2/d2.git",
		"## main...origin/main\n",
	)
	cfg1 := ParseConfig(fg1)
	require.Equal(t, 1, cfg1.State.RunCount)
	fg1.expectationsMet()

	// Second call — RunCount goes 1 → 2 (reads persisted state file).
	fg2 := newFakeGit(t, rootDir,
		"https://github.com/r2/d2.git",
		"## main...origin/main\n",
	)
	cfg2 := ParseConfig(fg2)
	require.Equal(t, 2, cfg2.State.RunCount)
	fg2.expectationsMet()
}
