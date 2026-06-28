package e2e

import (
	"os/exec"
	"strings"
	"testing"
)

// These scenarios pin the fix for the bug where `--version`, `--help`, and the
// `version` subcommand FAILED whenever spr was run outside a git repo, or inside
// a repo without `.spr.yml` / without a GitHub token. The defect was that
// cmd/spr/main.go ran git/config/github initialization (`git status`,
// ParseConfig, CheckConfig, NewGitHubClient) BEFORE urfave/cli ever got to
// process the version/help flags, so a failing init aborted the process (exit
// 255 outside a repo, exit 2/3 without config/token) before version/help could
// be shown.
//
// These are informational commands that must not require a git repo, config, or
// token. They are now short-circuited before any init, like _edit-sequence.

// versionRe matches the version string spr prints. The dev build prints
// "dev : unknown : dversion" (the package vars in main.go); a released build
// prints "<ver> : <date> : <commit>". We only need to confirm spr produced its
// own version line (and exited 0), not the exact contents, so assert on the
// stable " : " shape plus a 0 exit code.
const versionSep = " : "

// Scenario: `--version` OUTSIDE any git repo must print the version and exit 0.
// On the unfixed binary this exits 255 because `git status --porcelain` fails
// before cli ever sees --version.
func TestVersion_OutsideRepo(t *testing.T) {
	bin := BuildBinary(t, "./cmd/spr")
	nonRepo := t.TempDir() // plain dir, NOT a git repo
	env := cleanEnv(t)     // no GITHUB_TOKEN either: version must not need one

	stdout, stderr, code := RunInDir(t, nonRepo, bin, env, "--version")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stdout: %s stderr: %s)", code, stdout, stderr)
	}
	out := stdout + stderr
	if !strings.Contains(out, versionSep) {
		t.Fatalf("expected version output containing %q, got: %s", versionSep, out)
	}
	if strings.Contains(out, "not a git repository") {
		t.Fatalf("version must not require a git repo, but git error leaked: %s", out)
	}
}

// Scenario: the `version` SUBCOMMAND outside any git repo must also work. It is
// routed through cli.App, so it likewise must not require init to have run.
func TestVersionSubcommand_OutsideRepo(t *testing.T) {
	bin := BuildBinary(t, "./cmd/spr")
	nonRepo := t.TempDir()
	env := cleanEnv(t)

	stdout, stderr, code := RunInDir(t, nonRepo, bin, env, "version")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stdout: %s stderr: %s)", code, stdout, stderr)
	}
	out := stdout + stderr
	if !strings.Contains(out, versionSep) {
		t.Fatalf("expected version output containing %q, got: %s", versionSep, out)
	}
}

// Scenario: `--help` OUTSIDE any git repo must render help and exit 0. On the
// unfixed binary this exits 255 (git init fails first).
func TestHelp_OutsideRepo(t *testing.T) {
	bin := BuildBinary(t, "./cmd/spr")
	nonRepo := t.TempDir()
	env := cleanEnv(t)

	stdout, stderr, code := RunInDir(t, nonRepo, bin, env, "--help")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stdout: %s stderr: %s)", code, stdout, stderr)
	}
	out := stdout + stderr
	if !strings.Contains(out, "Stacked Pull Requests") {
		t.Fatalf("expected help output, got: %s", out)
	}
	if strings.Contains(out, "not a git repository") {
		t.Fatalf("help must not require a git repo, but git error leaked: %s", out)
	}
}

// Scenario: `<subcommand> --help` (here `update --help`) must route to the
// SUBCOMMAND's help — not the top-level help — and must work outside a repo. The
// early short-circuit must replay the real argv through cli so the subcommand is
// resolved; a naive "saw --help, print app help" short-circuit regresses this.
func TestSubcommandHelp_OutsideRepo(t *testing.T) {
	bin := BuildBinary(t, "./cmd/spr")
	nonRepo := t.TempDir()
	env := cleanEnv(t)

	stdout, stderr, code := RunInDir(t, nonRepo, bin, env, "update", "--help")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stdout: %s stderr: %s)", code, stdout, stderr)
	}
	out := stdout + stderr
	// The update command exposes a --reviewer flag; top-level help does not.
	if !strings.Contains(out, "reviewer") {
		t.Fatalf("expected update subcommand help (containing %q), got: %s", "reviewer", out)
	}
	if strings.Contains(out, "not a git repository") {
		t.Fatalf("subcommand help must not require a git repo, but git error leaked: %s", out)
	}
}

// Scenario: `--version` INSIDE a git repo that has no .spr.yml and no remote and
// no token must still print the version and exit 0. On the unfixed binary this
// exits 3 (ParseConfig owner auto-config / NewGitHubClient empty-token) because
// init ran before --version was handled.
func TestVersion_RepoWithoutConfig(t *testing.T) {
	bin := BuildBinary(t, "./cmd/spr")
	repo := InitTempRepo(t)

	// Strip the origin remote so owner auto-detection would fail, and give an
	// empty token so NewGitHubClient would fail too. Both are init-time failures
	// that the unfixed binary hits before showing the version.
	cmd := exec.Command("git", "remote", "remove", "origin")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote remove origin: %v\n%s", err, out)
	}
	env := cleanEnv(t, "GITHUB_TOKEN=")

	stdout, stderr, code := RunInDir(t, repo, bin, env, "--version")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stdout: %s stderr: %s)", code, stdout, stderr)
	}
	out := stdout + stderr
	if !strings.Contains(out, versionSep) {
		t.Fatalf("expected version output containing %q, got: %s", versionSep, out)
	}
	if strings.Contains(out, "auto configure repository owner") || strings.Contains(out, "No GitHub OAuth token") {
		t.Fatalf("version must not require config/token, but init error leaked: %s", out)
	}
}
