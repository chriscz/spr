package template_custom

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/ejoffe/spr/config"
	"github.com/ejoffe/spr/git"
	"github.com/ejoffe/spr/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helper builds a CustomTemplatizer whose readPRTemplate will succeed by writing
// a template file at tmpDir/<relPath>.
func newTemplatizerWithTemplate(t *testing.T, templateContent string) (*CustomTemplatizer, *github.GitHubInfo) {
	t.Helper()
	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "pr_template.md")
	require.NoError(t, os.WriteFile(templatePath, []byte(templateContent), 0644))

	repoConfig := &config.RepoConfig{
		PRTemplatePath: "pr_template.md",
		// no custom anchors → use defaults
	}
	gitcmd := &mockGit{rootDir: tmpDir}
	templatizer := NewCustomTemplatizer(repoConfig, gitcmd)

	info := &github.GitHubInfo{
		PullRequests: []*github.PullRequest{},
	}
	return templatizer, info
}

// ---------------------------------------------------------------------------
// Body — pr != nil (update path: no editor prompt)
// ---------------------------------------------------------------------------

func TestBody_WithExistingPR(t *testing.T) {
	templateContent := "<!-- SPR-STACK-START -->\nold\n<!-- SPR-STACK-END -->\n"
	templatizer, info := newTemplatizerWithTemplate(t, templateContent)

	commit := git.Commit{
		CommitHash: "abcdef1234567",
		CommitID:   "commit1",
		Subject:    "My subject",
		Body:       "My body",
	}
	pr := &github.PullRequest{
		Number: 42,
		Body:   "<!-- SPR-STACK-START -->\nold\n<!-- SPR-STACK-END -->\n",
	}

	got := templatizer.Body(info, commit, pr)
	// When pr != nil Body returns without opening editor; body is inserted into
	// the existing PR body between the default anchors.
	assert.Contains(t, got, "<!-- SPR-STACK-START -->")
	assert.Contains(t, got, "My body")
}

// TestBody_WithExistingPR_NoBody tests the case where pr is non-nil but has an
// empty body, so the template is used instead.
func TestBody_WithExistingPR_NoBody(t *testing.T) {
	templateContent := "# Template\n<!-- SPR-STACK-START -->\nplaceholder\n<!-- SPR-STACK-END -->\n"
	templatizer, info := newTemplatizerWithTemplate(t, templateContent)

	commit := git.Commit{
		CommitHash: "abcdef1234567",
		CommitID:   "commit1",
		Subject:    "My subject",
		Body:       "Commit body text",
	}
	// pr != nil but Body is empty → template is used
	pr := &github.PullRequest{Number: 5, Body: ""}

	got := templatizer.Body(info, commit, pr)
	assert.Contains(t, got, "<!-- SPR-STACK-START -->")
	assert.Contains(t, got, "Commit body text")
}

// ---------------------------------------------------------------------------
// Body — pr == nil, user declines editor (promptUserToEdit → false)
// ---------------------------------------------------------------------------

func TestBody_NewPR_UserDeclinesEditor(t *testing.T) {
	templateContent := "<!-- SPR-STACK-START -->\nold\n<!-- SPR-STACK-END -->\n"
	templatizer, info := newTemplatizerWithTemplate(t, templateContent)

	commit := git.Commit{
		CommitHash: "abcdef1234567",
		CommitID:   "commit1",
		Subject:    "My subject",
		Body:       "Declined body",
	}

	defer silenceStdout(t)()
	// Swap os.Stdin so promptUserToEdit reads "n\n"
	old := os.Stdin
	defer func() { os.Stdin = old }()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	_, err = io.WriteString(w, "n\n")
	require.NoError(t, err)
	w.Close()
	os.Stdin = r

	// pr == nil → will prompt; user says no → no editor
	got := templatizer.Body(info, commit, nil)
	assert.Contains(t, got, "Declined body")
}

// ---------------------------------------------------------------------------
// Body — pr == nil, user accepts editor, editor rewrites the file
// ---------------------------------------------------------------------------

func TestBody_NewPR_UserAcceptsEditor(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping editor subprocess test in short mode")
	}

	templateContent := "<!-- SPR-STACK-START -->\nold\n<!-- SPR-STACK-END -->\n"
	templatizer, info := newTemplatizerWithTemplate(t, templateContent)

	commit := git.Commit{
		CommitHash: "abcdef1234567",
		CommitID:   "commit1",
		Subject:    "My subject",
		Body:       "Original body",
	}

	// Install a fake editor that replaces the file with a known string.
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "fake-editor.sh")
	scriptContent := "#!/bin/sh\necho EDITED_BY_FAKE_EDITOR > \"$1\"\n"
	require.NoError(t, os.WriteFile(scriptPath, []byte(scriptContent), 0755))
	t.Setenv("EDITOR", scriptPath)

	defer silenceStdout(t)()
	// Swap os.Stdin so promptUserToEdit reads "y\n"
	old := os.Stdin
	defer func() { os.Stdin = old }()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	_, err = io.WriteString(w, "y\n")
	require.NoError(t, err)
	w.Close()
	os.Stdin = r

	got := templatizer.Body(info, commit, nil)
	assert.Contains(t, got, "EDITED_BY_FAKE_EDITOR")
}

// ---------------------------------------------------------------------------
// promptUserToEdit — various input branches
// ---------------------------------------------------------------------------

// silenceStdout redirects os.Stdout to /dev/null so the interactive prompts
// printed by promptUserToEdit don't pollute test output. Returns a restore func.
func silenceStdout(t *testing.T) func() {
	t.Helper()
	old := os.Stdout
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	require.NoError(t, err)
	os.Stdout = devnull
	return func() {
		os.Stdout = old
		devnull.Close()
	}
}

// pipeStdin replaces os.Stdin with a pipe containing the given input and
// silences os.Stdout (promptUserToEdit prints prompts). Returns a restore
// function that undoes both. Call as: defer pipeStdin(t, "n\n")()
func pipeStdin(t *testing.T, input string) func() {
	t.Helper()
	restoreStdout := silenceStdout(t)
	old := os.Stdin
	r, w, err := os.Pipe()
	require.NoError(t, err)
	_, err = io.WriteString(w, input)
	require.NoError(t, err)
	w.Close()
	os.Stdin = r
	return func() {
		os.Stdin = old
		restoreStdout()
	}
}

func makeCommit() git.Commit {
	return git.Commit{
		CommitHash: "abcdef1234567",
		Subject:    "Test subject",
	}
}

func TestPromptUserToEdit_Yes(t *testing.T) {
	defer pipeStdin(t, "y\n")()
	assert.True(t, promptUserToEdit(makeCommit()))
}

func TestPromptUserToEdit_YesFull(t *testing.T) {
	defer pipeStdin(t, "yes\n")()
	assert.True(t, promptUserToEdit(makeCommit()))
}

func TestPromptUserToEdit_No(t *testing.T) {
	defer pipeStdin(t, "n\n")()
	assert.False(t, promptUserToEdit(makeCommit()))
}

func TestPromptUserToEdit_NoFull(t *testing.T) {
	defer pipeStdin(t, "no\n")()
	assert.False(t, promptUserToEdit(makeCommit()))
}

func TestPromptUserToEdit_EmptyDefaultsToYes(t *testing.T) {
	defer pipeStdin(t, "\n")()
	assert.True(t, promptUserToEdit(makeCommit()))
}

func TestPromptUserToEdit_InvalidThenNo(t *testing.T) {
	// "maybe" is invalid → loops; then "n" → false
	defer pipeStdin(t, "maybe\nn\n")()
	assert.False(t, promptUserToEdit(makeCommit()))
}

func TestPromptUserToEdit_InvalidThenYes(t *testing.T) {
	// "what" is invalid → loops; then "yes" → true
	defer pipeStdin(t, "what\nyes\n")()
	assert.True(t, promptUserToEdit(makeCommit()))
}

func TestPromptUserToEdit_EOFDefaultsToTrue(t *testing.T) {
	defer silenceStdout(t)()
	// Close the write end immediately (EOF with no data) → scanner.Scan() == false → true
	old := os.Stdin
	defer func() { os.Stdin = old }()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	w.Close() // EOF immediately
	os.Stdin = r
	assert.True(t, promptUserToEdit(makeCommit()))
}

// ---------------------------------------------------------------------------
// EditWithEditor
// ---------------------------------------------------------------------------

func TestEditWithEditor_HappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping editor subprocess test in short mode")
	}

	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "fake-editor.sh")
	// The editor writes "EDITED CONTENT\n" into the file passed as $1.
	scriptContent := "#!/bin/sh\nprintf 'EDITED CONTENT\\n' > \"$1\"\n"
	require.NoError(t, os.WriteFile(scriptPath, []byte(scriptContent), 0755))
	t.Setenv("EDITOR", scriptPath)

	got, err := EditWithEditor("initial content")
	require.NoError(t, err)
	assert.Equal(t, "EDITED CONTENT\n", got)
}

func TestEditWithEditor_EditorLeavesContentUnchanged(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping editor subprocess test in short mode")
	}

	// /usr/bin/true exits 0 and does not touch the file → content unchanged.
	t.Setenv("EDITOR", "/usr/bin/true")

	got, err := EditWithEditor("unchanged content")
	require.NoError(t, err)
	assert.Equal(t, "unchanged content", got)
}

func TestEditWithEditor_ErrorPath(t *testing.T) {
	t.Setenv("EDITOR", "/nonexistent-editor-xyz-abc")

	_, err := EditWithEditor("some content")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "editor command failed")
}

// ---------------------------------------------------------------------------
// getSectionOfPRTemplate — error branches (callable from internal test package)
// ---------------------------------------------------------------------------

func TestGetSectionOfPRTemplate_InvalidEnum(t *testing.T) {
	// Exactly one match but an out-of-range returnMatch hits the default branch.
	_, err := getSectionOfPRTemplate("aXb", "X", 99)
	assert.EqualError(t, err, "invalid enum value")
}

func TestGetSectionOfPRTemplate_MultipleMatches(t *testing.T) {
	// Two occurrences → ErrMultipleMatchesFound.
	_, err := getSectionOfPRTemplate("aXbXc", "X", BeforeMatch)
	assert.ErrorIs(t, err, ErrMultipleMatchesFound)
}
