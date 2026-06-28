package e2e

import (
	"strings"
	"testing"
)

// These tests pin the fix for the "stack trace on every normal error" bug
// (GitHub #512 and the wider MustGit panic cluster). When an ordinary git
// command fails (non-zero exit), the command logic panics via
// git.GitInterface.MustGit. Before the fix that panic escaped main() and the
// Go runtime printed a full goroutine stack trace ("goroutine 1 [running]:" +
// internal file paths) that buried the real git error the user needs to read.
//
// After the fix a top-level handler turns the panic into a clean one-line
// error and a non-zero exit, and only re-raises the stack trace when the user
// asks for it (--verbose / --debug / SPR_DEBUG=1).
//
// The trigger used here is faithful to a real expected failure: a repo whose
// configured remote-tracking branch (origin/master) does not exist makes the
// very first MustGit call in git.GetLocalCommitStack
// ("git log ... origin/master..HEAD") exit non-zero — the same class of
// expected failure as `git spr amend` with nothing staged.

// assertNoStackTrace fails if the combined output contains a Go runtime panic
// stack trace. This is the core regression assertion: a "normal" failure must
// not dump goroutine traces at the user.
func assertNoStackTrace(t *testing.T, out string) {
	t.Helper()
	for _, marker := range []string{
		"goroutine 1 [running]",
		"git/realgit/realcmd.go",
		"git/helpers.go",
		"runtime.gopanic",
	} {
		if strings.Contains(out, marker) {
			t.Fatalf("output contained Go stack trace marker %q (the bug): \n%s", marker, out)
		}
	}
}

// configureMissingUpstream points the repo's spr config at origin/master while
// the repo's only branch is something else, so origin/master..HEAD is an
// unknown revision and the first MustGit in GetLocalCommitStack fails.
func sprBin(t *testing.T) string {
	t.Helper()
	return BuildBinary(t, "./cmd/spr")
}

// TestAmend_NormalGitFailure_NoStackTrace runs `spr amend` in a repo where the
// configured upstream branch is missing, which makes GetLocalCommitStack's
// MustGit fail. On the unfixed binary this panics and the runtime prints a
// goroutine stack trace; the fix must instead exit non-zero cleanly with no
// stack trace, while still surfacing the underlying git error.
func TestAmend_NormalGitFailure_NoStackTrace(t *testing.T) {
	bin := sprBin(t)
	// InitTempRepo creates a repo with default branch (main on modern git) and
	// an origin remote, but no origin/master tracking ref exists. spr's repo
	// config defaults GitHubBranch to "master", so origin/master..HEAD is an
	// unknown revision and the first MustGit fails.
	repo := InitTempRepo(t)

	env := cleanEnv(t, "GITHUB_TOKEN=dummy")
	// Feed "1" to the amend prompt in case execution gets that far; with the
	// missing upstream the failure happens earlier, but this keeps stdin from
	// blocking on any build of the binary.
	stdout, stderr, code := RunInDir(t, repo, bin, env, "amend")

	out := stdout + stderr
	if code == 0 {
		t.Fatalf("expected non-zero exit for a failing git command, got 0\nstdout: %s\nstderr: %s", stdout, stderr)
	}
	assertNoStackTrace(t, out)
	if !strings.Contains(out, "error") {
		t.Fatalf("expected a clean error message in output, got:\n%s", out)
	}
}

// TestAmend_NormalGitFailure_VerboseShowsTrace confirms the stack trace is
// still available on demand: with --verbose the original Go stack trace is
// printed (so real spr bugs remain debuggable).
func TestAmend_NormalGitFailure_VerboseShowsTrace(t *testing.T) {
	bin := sprBin(t)
	repo := InitTempRepo(t)

	env := cleanEnv(t, "GITHUB_TOKEN=dummy", "SPR_DEBUG=1")
	stdout, stderr, code := RunInDir(t, repo, bin, env, "amend")

	out := stdout + stderr
	if code == 0 {
		t.Fatalf("expected non-zero exit, got 0\nstdout: %s\nstderr: %s", stdout, stderr)
	}
	if !strings.Contains(out, "goroutine") {
		t.Fatalf("expected a Go stack trace when SPR_DEBUG=1, got:\n%s", out)
	}
}

// TestCmdAmend_NormalGitFailure_NoStackTrace covers the standalone cmd/amend
// binary (a second main()) for the same defect, ensuring both entry points get
// the top-level handler.
func TestCmdAmend_NormalGitFailure_NoStackTrace(t *testing.T) {
	bin := amendBin(t)
	repo := InitTempRepo(t)

	env := cleanEnv(t, "GITHUB_TOKEN=dummy")
	stdout, stderr, code := RunInDir(t, repo, bin, env)

	out := stdout + stderr
	if code == 0 {
		t.Fatalf("expected non-zero exit for a failing git command, got 0\nstdout: %s\nstderr: %s", stdout, stderr)
	}
	assertNoStackTrace(t, out)
}
