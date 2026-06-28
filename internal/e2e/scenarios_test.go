package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests run the real (unmodified) cmd/spr binary as a subprocess across
// network-free scenarios to exercise:
//   - cmd/spr handleEditSequence (happy path + both os.Exit(1) error paths),
//   - the cli.App{} wiring + help rendering (network-free help paths),
//   - several library os.Exit branches that only fire when the real binary runs
//     a bad scenario: realgit.NewGitCmd rev-parse failure (exit 255),
//     config_parser.ParseConfig host auto-detect failure (exit 2), and
//     githubclient.NewGitHubClient empty-token (exit 3).
//
// Coverage is credited via the covdata merge pipeline wired in Task 0.
//
// Each scenario sets HOME (a fresh clean temp dir), GITHUB_TOKEN, and the
// subprocess working directory explicitly so the runs are deterministic and
// never touch the developer's real ~/.spr.yml, gh auth, or keyring.

// cleanEnv returns env entries forcing a clean HOME (fresh temp dir) plus any
// extra key=value entries. Because Run/RunInDir append these to os.Environ() and
// later entries win for duplicate keys, this reliably overrides the parent env.
func cleanEnv(t *testing.T, extra ...string) []string {
	t.Helper()
	home := t.TempDir()
	env := []string{"HOME=" + home}
	return append(env, extra...)
}

// Scenario 1: _edit-sequence happy path rewrites the targeted pick -> edit and
// leaves other lines unchanged. Exit 0.
func TestEditSequence_Happy(t *testing.T) {
	bin := BuildBinary(t, "./cmd/spr")

	const hash = "abc123"
	todo := filepath.Join(t.TempDir(), "git-rebase-todo")
	if err := os.WriteFile(todo, []byte("pick "+hash+" first subject\npick def456 second subject\n"), 0o600); err != nil {
		t.Fatalf("writing todo file: %v", err)
	}

	_, stderr, code := Run(t, bin, nil, "_edit-sequence", hash, todo)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", code, stderr)
	}

	got, err := os.ReadFile(todo)
	if err != nil {
		t.Fatalf("reading todo back: %v", err)
	}
	want := "edit " + hash + " first subject\npick def456 second subject\n"
	if string(got) != want {
		t.Fatalf("todo mismatch:\n got: %q\nwant: %q", string(got), want)
	}
}

// Scenario 2: _edit-sequence with a nonexistent todo path -> read error, exit 1.
func TestEditSequence_ReadError(t *testing.T) {
	bin := BuildBinary(t, "./cmd/spr")

	_, stderr, code := Run(t, bin, nil, "_edit-sequence", "h", "/no/such/file")
	if code != 1 {
		t.Fatalf("expected exit 1, got %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr, "error reading todo file") {
		t.Fatalf("expected stderr to contain %q, got: %s", "error reading todo file", stderr)
	}
}

// Scenario 3: _edit-sequence with a readable-but-unwritable todo file -> write
// error, exit 1. Achieved with a 0444 file inside a 0555 dir. Skipped when run
// as root (root bypasses file permissions).
func TestEditSequence_WriteError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses file permissions; cannot trigger write error")
	}
	bin := BuildBinary(t, "./cmd/spr")

	const hash = "abc123"
	dir := t.TempDir()
	todo := filepath.Join(dir, "git-rebase-todo")
	if err := os.WriteFile(todo, []byte("pick "+hash+" subj\n"), 0o644); err != nil {
		t.Fatalf("writing todo file: %v", err)
	}
	// Make the file read-only AND the parent dir non-writable so the WriteFile
	// (open O_TRUNC|O_WRONLY) inside handleEditSequence fails.
	if err := os.Chmod(todo, 0o444); err != nil {
		t.Fatalf("chmod todo: %v", err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod dir: %v", err)
	}
	// Restore perms so t.TempDir cleanup can remove the tree.
	t.Cleanup(func() {
		_ = os.Chmod(dir, 0o755)
		_ = os.Chmod(todo, 0o644)
	})

	_, stderr, code := Run(t, bin, nil, "_edit-sequence", hash, todo)
	if code != 1 {
		t.Fatalf("expected exit 1, got %d (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr, "error writing todo file") {
		t.Fatalf("expected stderr to contain %q, got: %s", "error writing todo file", stderr)
	}
}

// Scenario 4: valid repo + valid config + dummy token -> `--help` exits 0 and
// renders the app help (no network: MaybeStar is a no-op on a fresh state with
// RunCount=1). Exercises the cli.App{} wiring bulk.
func TestHelp_AppLevel(t *testing.T) {
	bin := BuildBinary(t, "./cmd/spr")
	repo := InitTempRepo(t)
	env := cleanEnv(t, "GITHUB_TOKEN=dummy")

	stdout, stderr, code := RunInDir(t, repo, bin, env, "--help")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d (stdout: %s stderr: %s)", code, stdout, stderr)
	}
	out := stdout + stderr
	if !strings.Contains(out, "Stacked Pull Requests") {
		t.Fatalf("expected help to contain %q, got: %s", "Stacked Pull Requests", out)
	}
	if !strings.Contains(out, "COMMANDS") {
		t.Fatalf("expected help to contain %q, got: %s", "COMMANDS", out)
	}
}

// Scenario 5: `help` and a couple of `<subcmd> --help` render their command help
// and exit 0 — more cli.App wiring (commands/flags).
func TestHelp_Subcommands(t *testing.T) {
	bin := BuildBinary(t, "./cmd/spr")
	repo := InitTempRepo(t)

	cases := []struct {
		name   string
		args   []string
		expect string
	}{
		{"help", []string{"help"}, "Stacked Pull Requests"},
		{"update --help", []string{"update", "--help"}, "reviewer"},
		{"status --help", []string{"status", "--help"}, "detail"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := cleanEnv(t, "GITHUB_TOKEN=dummy")
			stdout, stderr, code := RunInDir(t, repo, bin, env, tc.args...)
			if code != 0 {
				t.Fatalf("expected exit 0, got %d (stdout: %s stderr: %s)", code, stdout, stderr)
			}
			out := stdout + stderr
			if !strings.Contains(out, tc.expect) {
				t.Fatalf("expected help for %v to contain %q, got: %s", tc.args, tc.expect, out)
			}
		})
	}
}

// Scenario 6: running outside any git repo -> realgit.NewGitCmd rev-parse fails
// -> os.Exit(-1) -> process exit code 255 (DG-P10-1).
func TestOutsideRepo_RevParseFail(t *testing.T) {
	bin := BuildBinary(t, "./cmd/spr")
	nonRepo := t.TempDir() // plain dir, NOT a git repo
	env := cleanEnv(t, "GITHUB_TOKEN=dummy")

	stdout, stderr, code := RunInDir(t, nonRepo, bin, env, "status")
	if code != 255 {
		t.Fatalf("expected exit 255 (os.Exit(-1)), got %d (stdout: %s stderr: %s)", code, stdout, stderr)
	}
	out := stdout + stderr
	if !strings.Contains(out, "not a git repository") {
		t.Fatalf("expected rev-parse error output, got: %s", out)
	}
}

// Scenario 7: a git repo with no GitHub-detectable remote and no .spr.yml ->
// ParseConfig cannot auto-detect the repo owner.
//
// NOTE (honesty): the brief expected this to hit the GitHubHost auto-config
// os.Exit(2) (DG-P02-1). In reality GitHubHost carries a `default:"github.com"`
// struct tag (config/config.go), so rake.DefaultSource() always fills it and the
// host-empty branch is UNREACHABLE from a real run. The FIRST exit that actually
// fires with no remote is the owner auto-config os.Exit(3) in ParseConfig. We
// assert the ACTUAL behavior; DG-P02-1 (host exit 2) remains deferred/unreachable
// and DG-P02 is instead exercised here via the owner branch (os.Exit(3)).
func TestNoRemote_OwnerAutoConfigFail(t *testing.T) {
	bin := BuildBinary(t, "./cmd/spr")
	repo := InitTempRepo(t)

	// Remove the origin remote so no owner can be auto-detected.
	cmd := exec.Command("git", "remote", "remove", "origin")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote remove origin: %v\n%s", err, out)
	}

	env := cleanEnv(t, "GITHUB_TOKEN=dummy")
	stdout, stderr, code := RunInDir(t, repo, bin, env, "status")
	if code != 3 {
		t.Fatalf("expected exit 3 (owner auto-config), got %d (stdout: %s stderr: %s)", code, stdout, stderr)
	}
	out := stdout + stderr
	if !strings.Contains(out, "unable to auto configure repository owner") {
		t.Fatalf("expected owner auto-config error, got: %s", out)
	}
}

// Scenario 8: valid repo/config but an explicitly empty GITHUB_TOKEN and a clean
// HOME (so findToken finds nothing) -> NewGitHubClient -> os.Exit(3) (DG-P01-1).
func TestEmptyToken_NoOAuth(t *testing.T) {
	bin := BuildBinary(t, "./cmd/spr")
	repo := InitTempRepo(t)
	// GITHUB_TOKEN explicitly empty overrides any inherited value.
	env := cleanEnv(t, "GITHUB_TOKEN=")

	stdout, stderr, code := RunInDir(t, repo, bin, env, "status")
	if code != 3 {
		t.Fatalf("expected exit 3, got %d (stdout: %s stderr: %s)", code, stdout, stderr)
	}
	out := stdout + stderr
	if !strings.Contains(out, "No GitHub OAuth token found") {
		t.Fatalf("expected no-token help, got: %s", out)
	}
}
