package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSmoke_EditSequence proves the covdata e2e pipeline: it builds the real
// cmd/spr binary instrumented for coverage, runs its internal `_edit-sequence`
// sequence-editor command (a non-network path handled before any git/config
// init), and asserts both the exit code and the file rewrite. When run via
// `make cover`/CI (SPR_E2E_COVERDIR set), this run emits covdata that is merged
// in to credit cmd/spr coverage.
func TestSmoke_EditSequence(t *testing.T) {
	bin := BuildBinary(t, "./cmd/spr")

	const hash = "abc123"
	todo := filepath.Join(t.TempDir(), "git-rebase-todo")
	if err := os.WriteFile(todo, []byte("pick "+hash+" first subject\npick def456 second subject\n"), 0o600); err != nil {
		t.Fatalf("writing todo file: %v", err)
	}

	_, stderr, code := Run(t, bin, nil, "_edit-sequence", hash, todo)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (stderr: %s)", code, stderr)
	}

	got, err := os.ReadFile(todo)
	if err != nil {
		t.Fatalf("reading todo file back: %v", err)
	}
	want := "edit " + hash + " first subject\npick def456 second subject\n"
	if string(got) != want {
		t.Fatalf("todo file mismatch:\n got: %q\nwant: %q", string(got), want)
	}
}
