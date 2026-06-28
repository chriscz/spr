package realgit

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ejoffe/spr/config"
	gogit "github.com/go-git/go-git/v5"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Silence realgit's production zerolog debug logging (one line per git command)
// so test output stays pristine. Test-only; changes no production behavior.
func init() {
	zerolog.SetGlobalLevel(zerolog.Disabled)
}

// initTempRepo creates a temporary git repository suitable for testing.
// It initialises the repo, sets minimal user config, writes a file, stages
// and commits it so that HEAD exists and rev-parse works.
func initTempRepo(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = tmp
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}

	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test User")

	// Write a file and commit so HEAD exists.
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "README"), []byte("hello\n"), 0o600))
	run("add", "README")
	run("commit", "-m", "init")

	return tmp
}

// newTestGitCmd builds a gitcmd directly against a temp repo (no NewGitCmd /
// no os.Exit path).
func newTestGitCmd(t *testing.T, tmp string) *gitcmd {
	t.Helper()
	cfg := config.DefaultConfig()
	return &gitcmd{config: cfg, rootdir: tmp}
}

// ── Git / GitWithEditor ───────────────────────────────────────────────────────

func TestGit_SuccessWithOutput(t *testing.T) {
	tmp := initTempRepo(t)
	gc := newTestGitCmd(t, tmp)

	var out string
	err := gc.Git("rev-parse HEAD", &out)
	require.NoError(t, err)
	// A full SHA-1 is 40 hex characters.
	assert.Len(t, out, 40, "expected a 40-char SHA-1, got: %q", out)
}

func TestGit_SuccessWithOutputCleanStatus(t *testing.T) {
	tmp := initTempRepo(t)
	gc := newTestGitCmd(t, tmp)

	var out string
	err := gc.Git("status --porcelain", &out)
	require.NoError(t, err)
	assert.Equal(t, "", out, "expected clean working tree")
}

func TestGit_SuccessNilOutput(t *testing.T) {
	tmp := initTempRepo(t)
	gc := newTestGitCmd(t, tmp)

	// Passing nil for output should still succeed.
	err := gc.Git("status", nil)
	require.NoError(t, err)
}

func TestGit_ErrorPath(t *testing.T) {
	tmp := initTempRepo(t)
	gc := newTestGitCmd(t, tmp)

	// Redirect os.Stderr to /dev/null to keep test output pristine;
	// Git prints "git error: …" via fmt.Fprintf(os.Stderr, …) on failure.
	origStderr := os.Stderr
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	require.NoError(t, err)
	os.Stderr = devnull
	defer func() {
		os.Stderr = origStderr
		devnull.Close()
	}()

	var out string
	err = gc.Git("not-a-real-git-subcommand", &out)
	assert.Error(t, err, "expected an error for an invalid git subcommand")
}

func TestGit_ErrorPathNilOutput(t *testing.T) {
	tmp := initTempRepo(t)
	gc := newTestGitCmd(t, tmp)

	origStderr := os.Stderr
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	require.NoError(t, err)
	os.Stderr = devnull
	defer func() {
		os.Stderr = origStderr
		devnull.Close()
	}()

	err = gc.Git("not-a-real-git-subcommand", nil)
	assert.Error(t, err, "expected an error for an invalid git subcommand with nil output")
}

// ── NoFetch / NoRebase short-circuit paths ────────────────────────────────────

func TestGit_NoFetchSkip(t *testing.T) {
	tmp := initTempRepo(t)
	cfg := config.DefaultConfig()
	cfg.User.NoFetch = true
	gc := &gitcmd{config: cfg, rootdir: tmp}

	// "fetch …" with NoFetch=true must return nil without running git
	// (if it ran git it would fail because there is no remote).
	var out string
	err := gc.Git("fetch origin", &out)
	assert.NoError(t, err, "NoFetch should suppress the fetch and return nil")
	assert.Empty(t, out)
}

func TestGit_NoRebaseSkip(t *testing.T) {
	tmp := initTempRepo(t)
	cfg := config.DefaultConfig()
	cfg.User.NoRebase = true
	gc := &gitcmd{config: cfg, rootdir: tmp}

	var out string
	err := gc.Git("rebase --onto main", &out)
	assert.NoError(t, err, "NoRebase should suppress the rebase and return nil")
	assert.Empty(t, out)
}

// ── LogGitCommands ────────────────────────────────────────────────────────────

func TestGit_LogGitCommands(t *testing.T) {
	tmp := initTempRepo(t)
	cfg := config.DefaultConfig()
	cfg.User.LogGitCommands = true
	gc := &gitcmd{config: cfg, rootdir: tmp}

	// Redirect os.Stdout so "> git …" noise doesn't pollute the test runner.
	origStdout := os.Stdout
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	require.NoError(t, err)
	os.Stdout = devnull
	defer func() {
		os.Stdout = origStdout
		devnull.Close()
	}()

	var out string
	err = gc.Git("rev-parse HEAD", &out)
	require.NoError(t, err)
	assert.Len(t, out, 40)
}

// ── GitWithEditor ─────────────────────────────────────────────────────────────

func TestGitWithEditor_Success(t *testing.T) {
	tmp := initTempRepo(t)
	gc := newTestGitCmd(t, tmp)

	var out string
	// Use /usr/bin/true as the editor (same as the Git wrapper).
	err := gc.GitWithEditor("rev-parse HEAD", &out, "/usr/bin/true")
	require.NoError(t, err)
	assert.Len(t, out, 40)
}

// stageAutosquashRebase prepares an autosquash rebase scenario in a temp repo:
// two real commits plus a `fixup!` commit targeting the first, so that
// `git rebase -i --autosquash` has a todo that git must process through the
// configured sequence/commit editor. This mirrors what spr.AmendCommit drives.
func stageAutosquashRebase(t *testing.T) string {
	t.Helper()
	tmp := initTempRepo(t)

	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = tmp
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
		return strings.TrimSpace(string(out))
	}

	// initTempRepo already made an "init" commit. Add a second real commit,
	// then a fixup! of it so autosquash has work to do.
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "README"), []byte("hello\nworld\n"), 0o600))
	run("add", "README")
	run("commit", "-m", "second")
	target := run("rev-parse", "HEAD")

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "README"), []byte("hello\nworld\nagain\n"), 0o600))
	run("add", "README")
	run("commit", "--fixup", target)

	return tmp
}

// TestGit_AutosquashRebaseUsesResolvableEditor is the regression test for
// issue #362: the no-op rebase editor must not be a hardcoded absolute path
// (`/usr/bin/true`) that is absent on systems where `true` is only a shell
// builtin (zsh / minimal images). When git has to launch the editor for an
// autosquash rebase and that absolute path does not exist, git aborts with
// "unable to start editor" and spr panics.
//
// The test drives a real `git rebase -i --autosquash` through gitcmd.Git (the
// exact path AmendCommit uses) and proves the discriminator both ways:
//   - pre-fix behaviour (an absolute editor path that does not exist) is RED:
//     git fails with "unable to start editor", reproducing the bug;
//   - the production editor (gitNoopEditor) is GREEN: the rebase completes.
func TestGit_AutosquashRebaseUsesResolvableEditor(t *testing.T) {
	const rebaseCmd = "rebase -i --autosquash --autostash HEAD~2"

	// Silence the "git error: …" stderr that Git prints on failure.
	origStderr := os.Stderr
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	require.NoError(t, err)
	os.Stderr = devnull
	defer func() {
		os.Stderr = origStderr
		devnull.Close()
	}()

	// RED proof: a hardcoded absolute editor path that does not exist (exactly
	// the failure mode of /usr/bin/true on a zsh-builtin-only host) makes the
	// autosquash rebase abort. This is what the bug looks like.
	t.Run("absent absolute editor path aborts the rebase (the bug)", func(t *testing.T) {
		tmp := stageAutosquashRebase(t)
		gc := newTestGitCmd(t, tmp)

		absent := filepath.Join(t.TempDir(), "nonexistent", "true")
		defer swapNoopEditor(absent)()

		err := gc.Git(rebaseCmd, nil)
		require.Error(t, err, "an absent absolute editor path must make the autosquash rebase fail")
	})

	// GREEN proof: the production no-op editor (gitNoopEditor) completes the
	// autosquash rebase. With the hardcoded "/usr/bin/true" this fails wherever
	// that file is absent; "/usr/bin/env true" resolves `true` via PATH instead.
	t.Run("production no-op editor completes the rebase", func(t *testing.T) {
		tmp := stageAutosquashRebase(t)
		gc := newTestGitCmd(t, tmp)

		err := gc.Git(rebaseCmd, nil)
		require.NoError(t, err, "production no-op editor must complete the autosquash rebase")
	})
}

// swapNoopEditor temporarily overrides the package no-op editor and returns a
// restore func (defer it). Test-only.
func swapNoopEditor(v string) func() {
	prev := gitNoopEditor
	gitNoopEditor = v
	return func() { gitNoopEditor = prev }
}

// ── MustGit ───────────────────────────────────────────────────────────────────

func TestMustGit_Success(t *testing.T) {
	tmp := initTempRepo(t)
	gc := newTestGitCmd(t, tmp)

	var out string
	// Should not panic.
	require.NotPanics(t, func() {
		gc.MustGit("rev-parse HEAD", &out)
	})
	assert.Len(t, out, 40)
}

func TestMustGit_Panic(t *testing.T) {
	tmp := initTempRepo(t)
	gc := newTestGitCmd(t, tmp)

	origStderr := os.Stderr
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	require.NoError(t, err)
	os.Stderr = devnull
	defer func() {
		os.Stderr = origStderr
		devnull.Close()
	}()

	assert.Panics(t, func() {
		gc.MustGit("not-a-real-git-subcommand", nil)
	})
}

// ── RootDir ───────────────────────────────────────────────────────────────────

func TestRootDir(t *testing.T) {
	tmp := initTempRepo(t)
	gc := newTestGitCmd(t, tmp)

	assert.Equal(t, tmp, gc.RootDir())
}

// ── maybeAdjustPathPerPlatform ────────────────────────────────────────────────

func TestMaybeAdjustPathPerPlatform_NonCygdrive(t *testing.T) {
	// On Linux (non-cygwin) any path that does not start with /cygdrive
	// must be returned verbatim.
	cases := []string{
		"/home/user/repo",
		"/tmp/git-repo",
		"relative/path",
		"",
	}
	for _, c := range cases {
		got := maybeAdjustPathPerPlatform(c)
		assert.Equal(t, c, got, "expected path unchanged for input %q", c)
	}
}

// ── NewGitCmd (in-repo happy path) ───────────────────────────────────────────

func TestNewGitCmd_InRepo(t *testing.T) {
	// The test process runs inside the spr git repository, so NewGitCmd should
	// succeed and return a non-empty RootDir.
	cfg := config.DefaultConfig()

	// Silence the LogGitCommands noise just in case.
	cfg.User.LogGitCommands = false

	gc := NewGitCmd(cfg)
	require.NotNil(t, gc)
	assert.NotEmpty(t, gc.RootDir(), "RootDir should be the repo root")
	assert.True(t, filepath.IsAbs(gc.RootDir()), "RootDir should be an absolute path")
}

// ── DeleteRemoteBranch — error path (no such remote) ─────────────────────────

func TestDeleteRemoteBranch_NoRemoteError(t *testing.T) {
	tmp := initTempRepo(t)
	cfg := config.DefaultConfig()
	// Point at a remote name that does not exist in the temp repo.
	cfg.Repo.GitHubRemote = "nonexistent-remote"

	repo, err := gogit.PlainOpen(tmp)
	require.NoError(t, err)

	gc := &gitcmd{config: cfg, repo: repo, rootdir: tmp}

	err = gc.DeleteRemoteBranch(context.Background(), "some-branch")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "getting remote")
}

// ── DeleteRemoteBranch — success path (via a temp bare remote) ───────────────

func TestDeleteRemoteBranch_Success(t *testing.T) {
	// Create a local repo with a commit.
	local := initTempRepo(t)

	// Create a bare remote repo.
	bare := t.TempDir()
	cmd := exec.Command("git", "init", "--bare", bare)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git init --bare: %s", out)

	run := func(dir string, args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		o, e := c.CombinedOutput()
		require.NoError(t, e, "git %v: %s", args, o)
	}

	// Add bare as remote "origin" and push main/master branch.
	run(local, "remote", "add", "origin", bare)

	// Determine default branch name (could be "main" or "master").
	var headRef string
	{
		c := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
		c.Dir = local
		o, e := c.CombinedOutput()
		require.NoError(t, e, "rev-parse HEAD: %s", o)
		headRef = strings.TrimSpace(string(o))
	}

	run(local, "push", "origin", headRef)

	// Create and push an extra branch that we will delete.
	const branchToDelete = "spr/test-delete-branch"
	run(local, "checkout", "-b", branchToDelete)
	run(local, "push", "origin", branchToDelete)
	run(local, "checkout", headRef)

	// Build gitcmd with the go-git repo object.
	cfg := config.DefaultConfig()
	cfg.Repo.GitHubRemote = "origin"

	repo, err := gogit.PlainOpen(local)
	require.NoError(t, err)

	gc := &gitcmd{config: cfg, repo: repo, rootdir: local}

	err = gc.DeleteRemoteBranch(context.Background(), branchToDelete)
	require.NoError(t, err, "expected DeleteRemoteBranch to succeed")

	// Verify the branch is gone from the bare remote.
	c := exec.Command("git", "branch")
	c.Dir = bare
	o, e := c.CombinedOutput()
	require.NoError(t, e)
	assert.NotContains(t, string(o), branchToDelete, "remote branch should have been deleted")
}
