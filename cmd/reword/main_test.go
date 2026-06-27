package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Silence realgit's production zerolog debug logging (one line per git command)
// so test output stays pristine. Test-only; changes no production behavior.
func init() {
	zerolog.SetGlobalLevel(zerolog.Disabled)
}

// ---------------------------------------------------------------------------
// check
// ---------------------------------------------------------------------------

func TestCheck_NilIsNoOp(t *testing.T) {
	// check(nil) must not panic
	require.NotPanics(t, func() {
		check(nil)
	})
}

func TestCheck_NonNilPanics(t *testing.T) {
	require.Panics(t, func() {
		check(errors.New("boom"))
	})
}

// ---------------------------------------------------------------------------
// shouldAppendCommitID
// ---------------------------------------------------------------------------

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// File already contains a commit-id line → missingCommitID=false
func TestShouldAppendCommitID_AlreadyHasCommitID(t *testing.T) {
	tmp := t.TempDir()
	path := writeFile(t, tmp, "COMMIT_EDITMSG", "My commit message\n\ncommit-id:abcd1234\n")
	missingID, missingNL := shouldAppendCommitID(path)
	assert.False(t, missingID, "commit-id present → missingCommitID should be false")
	assert.False(t, missingNL)
}

// Non-empty multi-line message, no commit-id → missingCommitID=true, missingNewLine=false
func TestShouldAppendCommitID_MultiLineNeedsID(t *testing.T) {
	tmp := t.TempDir()
	path := writeFile(t, tmp, "COMMIT_EDITMSG", "My commit message\n\nSome body text\n")
	missingID, missingNL := shouldAppendCommitID(path)
	assert.True(t, missingID, "no commit-id → missingCommitID should be true")
	assert.False(t, missingNL, "multiple non-comment lines → missingNewLine should be false")
}

// Single non-comment line (lineCount==1) → missingNewLine=true
func TestShouldAppendCommitID_SingleLineNeedsNewline(t *testing.T) {
	tmp := t.TempDir()
	path := writeFile(t, tmp, "COMMIT_EDITMSG", "My commit message\n")
	missingID, missingNL := shouldAppendCommitID(path)
	assert.True(t, missingID, "no commit-id → missingCommitID should be true")
	assert.True(t, missingNL, "single non-comment line → missingNewLine should be true")
}

// Only comment lines / empty → nonEmptyCommitMessage=false → missingCommitID=false
func TestShouldAppendCommitID_OnlyComments(t *testing.T) {
	tmp := t.TempDir()
	path := writeFile(t, tmp, "COMMIT_EDITMSG", "# This is a comment\n# Another comment\n")
	missingID, missingNL := shouldAppendCommitID(path)
	assert.False(t, missingID, "only comments → nonEmptyCommitMessage=false → missingCommitID=false")
	assert.False(t, missingNL)
}

// Empty file (no lines at all) → missingCommitID=false
func TestShouldAppendCommitID_EmptyFile(t *testing.T) {
	tmp := t.TempDir()
	path := writeFile(t, tmp, "COMMIT_EDITMSG", "")
	missingID, missingNL := shouldAppendCommitID(path)
	assert.False(t, missingID)
	assert.False(t, missingNL)
}

// ---------------------------------------------------------------------------
// appendCommitID
// ---------------------------------------------------------------------------

func TestAppendCommitID_WithoutMissingNewLine(t *testing.T) {
	tmp := t.TempDir()
	path := writeFile(t, tmp, "COMMIT_EDITMSG", "My commit\n\n")
	appendCommitID(path, false)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(data)

	// Must contain a commit-id: line
	require.True(t, strings.Contains(content, "commit-id:"), "appended content must contain 'commit-id:'")

	// Extract the commit-id line and check length
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "commit-id:") {
			id := strings.TrimPrefix(line, "commit-id:")
			assert.Len(t, id, 8, "commit-id value must be 8 hex chars")
		}
	}
}

func TestAppendCommitID_WithMissingNewLine(t *testing.T) {
	tmp := t.TempDir()
	// Single-line message (no trailing newline after body)
	path := writeFile(t, tmp, "COMMIT_EDITMSG", "My commit")
	appendCommitID(path, true)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(data)

	require.True(t, strings.Contains(content, "commit-id:"), "must contain 'commit-id:'")
	// When missingNewLine=true an extra '\n' is prepended before the blank line
	// so the final text must have at least two consecutive newlines before commit-id.
	assert.True(t, strings.Contains(content, "\n\ncommit-id:"), "expected double-newline before commit-id")
}

func TestAppendCommitID_IDIs8Chars(t *testing.T) {
	tmp := t.TempDir()
	path := writeFile(t, tmp, "COMMIT_EDITMSG", "msg\n\n")
	appendCommitID(path, false)

	data, _ := os.ReadFile(path)
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "commit-id:") {
			id := strings.TrimPrefix(line, "commit-id:")
			assert.Len(t, id, 8)
			return
		}
	}
	t.Fatal("no commit-id: line found")
}

// ---------------------------------------------------------------------------
// main — COMMIT_EDITMSG branch
// ---------------------------------------------------------------------------

func TestMain_CommitEditmsg_AppendsCommitID(t *testing.T) {
	tmp := t.TempDir()
	// Name must end in COMMIT_EDITMSG to trigger that branch
	path := filepath.Join(tmp, "COMMIT_EDITMSG")
	require.NoError(t, os.WriteFile(path, []byte("Add feature X\n"), 0o600))

	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"reword", path}

	main()

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(data), "commit-id:"), "main must append commit-id to COMMIT_EDITMSG")
}

// File already has commit-id — main must not append a second one.
func TestMain_CommitEditmsg_AlreadyHasID_NoDoubleAppend(t *testing.T) {
	tmp := t.TempDir()
	original := "Add feature X\n\ncommit-id:deadbeef\n"
	path := filepath.Join(tmp, "COMMIT_EDITMSG")
	require.NoError(t, os.WriteFile(path, []byte(original), 0o600))

	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"reword", path}

	main()

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(data)
	count := strings.Count(content, "commit-id:")
	assert.Equal(t, 1, count, "must not double-append commit-id")
}

// ---------------------------------------------------------------------------
// main — rebase-todo branch (pick lines processed via git log)
// ---------------------------------------------------------------------------

// pickHash is a real commit in this repo whose body lacks a commit-id.
// main will rewrite "pick <hash> ..." → "reword <hash> ...".
const pickHash = "7f5f21a569b9fbc35802359cd88dd3a74d89c8c9"

// keepHash is a real commit whose body contains commit-id → stays "pick".
const keepHash = "0767a458e50fa1f7ae203b73e50298ab201c80bb"

func TestMain_RebaseTodo_PickRewrittenToReword(t *testing.T) {
	tmp := t.TempDir()
	// A filename that does NOT end in COMMIT_EDITMSG triggers the rebase-todo branch.
	path := filepath.Join(tmp, "git-rebase-todo")
	content := "pick " + pickHash + " docs: record P10 gaps\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"reword", path}

	main()

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	result := string(data)
	assert.True(t, strings.HasPrefix(result, "reword "), "pick should be rewritten to reword when commit has no commit-id")
	assert.True(t, strings.Contains(result, pickHash), "hash must be preserved")
}

func TestMain_RebaseTodo_PickStaysPickWhenHasCommitID(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "git-rebase-todo")
	content := "pick " + keepHash + " fix git spr edit\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"reword", path}

	main()

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	result := string(data)
	assert.True(t, strings.HasPrefix(result, "pick "), "pick must stay pick when commit already has commit-id")
}

func TestMain_RebaseTodo_NonPickLinePassesThrough(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "git-rebase-todo")
	content := "# comment line\nexec echo hi\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"reword", path}

	main()

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	result := string(data)
	assert.Contains(t, result, "# comment line")
	assert.Contains(t, result, "exec echo hi")
}
