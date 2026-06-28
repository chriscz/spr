package e2e

import (
	"os/exec"
	"strings"
	"testing"
)

// These tests run the real (unmodified) cmd/amend binary as a subprocess across
// network-free scenarios to exercise the pre-network half of cmd/amend/main.go:
//
//   - flags.Parse + --version os.Exit(0)
//   - realgit.NewGitCmd rev-parse failure (outside repo -> os.Exit(-1) -> 255)
//   - config_parser.ParseConfig owner auto-config failure (no remote -> os.Exit(3))
//   - githubclient.NewGitHubClient empty-token (-> os.Exit(3))
//
// The network AmendCommit/UpdatePullRequests bodies are deferred (DG-CMDAMEND-1).
// Coverage is credited via the covdata merge pipeline wired in Task 0.
//
// Each scenario sets HOME (a fresh clean temp dir), GITHUB_TOKEN, and the
// subprocess working directory explicitly so the runs are deterministic and never
// touch the developer's real ~/.spr.yml, gh auth, or keyring.

// amendBin is a package-level helper that builds the cmd/amend binary once and
// returns its path. Callers must pass t so BuildBinary can log fatal errors.
func amendBin(t *testing.T) string {
	t.Helper()
	return BuildBinary(t, "./cmd/amend")
}

// TestAmend_Version runs `amend --version` from a non-repo directory. It must
// exit 0 and print "amend version : dev : unknown : dversion" on stdout.
// This exercises flags.Parse, the opts.Version branch, and os.Exit(0) before any
// git/config initialisation — so it works anywhere, including outside a git repo.
func TestAmend_Version(t *testing.T) {
	bin := amendBin(t)
	nonRepo := t.TempDir() // plain dir, NOT a git repo
	env := cleanEnv(t)     // no GITHUB_TOKEN needed; we never reach that code path

	stdout, stderr, code := RunInDir(t, nonRepo, bin, env, "--version")
	if code != 0 {
		t.Fatalf("expected exit 0 from --version, got %d (stdout: %s stderr: %s)", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "amend version") {
		t.Fatalf("expected stdout to contain %q, got: %s", "amend version", stdout)
	}
	if !strings.Contains(stdout, "dversion") {
		t.Fatalf("expected stdout to contain %q, got: %s", "dversion", stdout)
	}
}

// TestAmend_OutsideRepo runs `amend` (no args) from a plain directory that is NOT
// a git repository. realgit.NewGitCmd calls `git rev-parse --git-dir` which
// fails, producing os.Exit(-1) -> process exit code 255.
func TestAmend_OutsideRepo(t *testing.T) {
	bin := amendBin(t)
	nonRepo := t.TempDir()
	env := cleanEnv(t, "GITHUB_TOKEN=dummy")

	stdout, stderr, code := RunInDir(t, nonRepo, bin, env)
	if code != 255 {
		t.Fatalf("expected exit 255 (os.Exit(-1) from rev-parse), got %d (stdout: %s stderr: %s)", code, stdout, stderr)
	}
	out := stdout + stderr
	if !strings.Contains(out, "not a git repository") {
		t.Fatalf("expected rev-parse error output to contain %q, got: %s", "not a git repository", out)
	}
}

// TestAmend_NoRemote runs `amend` inside a git repo that has no GitHub-detectable
// remote (origin removed) and no .spr.yml. config_parser.ParseConfig cannot
// auto-detect the repository owner and calls os.Exit(3).
//
// NOTE (honesty): although the brief mentions a possible host os.Exit(2) path,
// GitHubHost carries a `default:"github.com"` struct tag so that branch is
// unreachable from a real run. The FIRST exit that actually fires is the owner
// auto-config os.Exit(3) in ParseConfig. We assert the ACTUAL behavior.
func TestAmend_NoRemote(t *testing.T) {
	bin := amendBin(t)
	repo := InitTempRepo(t)

	// Remove the origin remote so no owner can be auto-detected.
	cmd := exec.Command("git", "remote", "remove", "origin")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote remove origin: %v\n%s", err, out)
	}

	env := cleanEnv(t, "GITHUB_TOKEN=dummy")
	stdout, stderr, code := RunInDir(t, repo, bin, env)
	if code != 3 {
		t.Fatalf("expected exit 3 (owner auto-config), got %d (stdout: %s stderr: %s)", code, stdout, stderr)
	}
	out := stdout + stderr
	if !strings.Contains(out, "unable to auto configure repository owner") {
		t.Fatalf("expected owner auto-config error to contain %q, got: %s", "unable to auto configure repository owner", out)
	}
}

// TestAmend_EmptyToken runs `amend` inside a valid git repo with a GitHub-shaped
// remote, but with GITHUB_TOKEN explicitly empty and a clean HOME (so no keyring
// or ~/.config/gh/hosts.yml exists). githubclient.NewGitHubClient finds no token
// and calls os.Exit(3).
func TestAmend_EmptyToken(t *testing.T) {
	bin := amendBin(t)
	repo := InitTempRepo(t) // has origin remote -> owner auto-detect succeeds

	// GITHUB_TOKEN explicitly empty overrides any inherited value; clean HOME
	// ensures no keyring / gh auth file is found either.
	env := cleanEnv(t, "GITHUB_TOKEN=")

	stdout, stderr, code := RunInDir(t, repo, bin, env)
	if code != 3 {
		t.Fatalf("expected exit 3 (no OAuth token), got %d (stdout: %s stderr: %s)", code, stdout, stderr)
	}
	out := stdout + stderr
	if !strings.Contains(out, "No GitHub OAuth token found") {
		t.Fatalf("expected no-token error to contain %q, got: %s", "No GitHub OAuth token found", out)
	}
}
