package spr

// spr_coverage_test.go — additional tests to raise spr package coverage.
// Extends the harness in spr_test.go (same package) without touching production code.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ejoffe/spr/config"
	"github.com/ejoffe/spr/git"
	"github.com/ejoffe/spr/github"
	"github.com/ejoffe/spr/github/githubclient/gen/genclient"
	"github.com/ejoffe/spr/github/mockclient"
	"github.com/stretchr/testify/require"
)

// silenceStdout redirects os.Stdout to /dev/null for the duration of a test.
// Some production paths (RunMergeCheck, the remote-pr-branch warning in
// fetchAndGetGitHubInfo, ProfilingSummary) print to the real stdout via
// fmt.Println/Printf rather than the sd.output buffer; this keeps test output
// pristine. Use as: defer silenceStdout(t)()
//
// Note: this only suppresses real-stdout writes. Assertions on sd.output (a
// bytes.Buffer) are unaffected.
func silenceStdout(t *testing.T) func() {
	t.Helper()
	old := os.Stdout
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	require.NoError(t, err)
	os.Stdout = devnull
	return func() {
		os.Stdout = old
		_ = devnull.Close()
	}
}

// ---------------------------------------------------------------------------
// Local git.GitInterface implementations (package spr, not mockgit).
// These record git calls and return canned responses, letting tests drive
// branches that mockgit cannot reach with its existing public API.
// ---------------------------------------------------------------------------

// gitRecorder records all Git/MustGit calls and returns no errors.
type gitRecorder struct {
	calls   []string
	rootdir string
}

func (r *gitRecorder) Git(args string, output *string) error {
	r.calls = append(r.calls, args)
	if output != nil {
		*output = ""
	}
	return nil
}

func (r *gitRecorder) MustGit(args string, output *string) { _ = r.Git(args, output) }
func (r *gitRecorder) GitWithEditor(args string, output *string, editorCmd string) error {
	return r.Git(args, output)
}
func (r *gitRecorder) RootDir() string { return r.rootdir }
func (r *gitRecorder) GitDir() string  { return r.rootdir + "/.git" }
func (r *gitRecorder) DeleteRemoteBranch(_ context.Context, branch string) error {
	r.calls = append(r.calls, "DeleteRemoteBranch("+branch+")")
	return nil
}

// gitLogResponder returns a canned log output for GetLocalCommitStack.
type gitLogResponder struct {
	logResponse string
	calls       []string
}

func (r *gitLogResponder) Git(args string, output *string) error {
	r.calls = append(r.calls, args)
	if strings.HasPrefix(args, "log") && output != nil {
		*output = r.logResponse
	}
	return nil
}

func (r *gitLogResponder) MustGit(args string, output *string) { _ = r.Git(args, output) }
func (r *gitLogResponder) GitWithEditor(args string, output *string, editorCmd string) error {
	return r.Git(args, output)
}
func (r *gitLogResponder) RootDir() string                                           { return "" }
func (r *gitLogResponder) GitDir() string                                            { return "/.git" }
func (r *gitLogResponder) DeleteRemoteBranch(_ context.Context, branch string) error { return nil }

// fetchRecorder records calls and optionally returns an error for rebase.
type fetchRecorder struct {
	calls     []string
	rebaseErr error
}

func (r *fetchRecorder) Git(args string, output *string) error {
	r.calls = append(r.calls, args)
	if strings.HasPrefix(args, "rebase") && r.rebaseErr != nil {
		return r.rebaseErr
	}
	return nil
}

func (r *fetchRecorder) MustGit(args string, output *string) {
	if err := r.Git(args, output); err != nil {
		panic(err)
	}
}
func (r *fetchRecorder) GitWithEditor(args string, output *string, editorCmd string) error {
	return r.Git(args, output)
}
func (r *fetchRecorder) RootDir() string                                           { return "" }
func (r *fetchRecorder) GitDir() string                                            { return "/.git" }
func (r *fetchRecorder) DeleteRemoteBranch(_ context.Context, branch string) error { return nil }

// pushRecorder records MustGit calls (status, push etc.)
type pushRecorder struct {
	calls []string
}

func (r *pushRecorder) Git(args string, output *string) error {
	r.calls = append(r.calls, args)
	if output != nil {
		*output = ""
	}
	return nil
}

func (r *pushRecorder) MustGit(args string, output *string) { _ = r.Git(args, output) }
func (r *pushRecorder) GitWithEditor(args string, output *string, editorCmd string) error {
	return r.Git(args, output)
}
func (r *pushRecorder) RootDir() string                                           { return "" }
func (r *pushRecorder) GitDir() string                                            { return "/.git" }
func (r *pushRecorder) DeleteRemoteBranch(_ context.Context, branch string) error { return nil }

// editErrorRecorder: handles log (with canned response), rebase -i errors,
// and tracks the rootdir for state-file paths.
type editStartErrorRecorder struct {
	tmpDir      string
	logResponse string
	rebaseErr   error
	calls       []string
}

func (r *editStartErrorRecorder) Git(args string, output *string) error {
	r.calls = append(r.calls, args)
	if strings.HasPrefix(args, "log") && output != nil {
		*output = r.logResponse
	}
	if strings.HasPrefix(args, "rebase -i") {
		return r.rebaseErr
	}
	return nil
}

func (r *editStartErrorRecorder) MustGit(args string, output *string) {
	if err := r.Git(args, output); err != nil {
		panic(err)
	}
}
func (r *editStartErrorRecorder) GitWithEditor(args string, output *string, editorCmd string) error {
	return r.Git(args, output)
}
func (r *editStartErrorRecorder) RootDir() string { return r.tmpDir }
func (r *editStartErrorRecorder) GitDir() string  { return r.tmpDir + "/.git" }
func (r *editStartErrorRecorder) DeleteRemoteBranch(_ context.Context, branch string) error {
	return nil
}

// amendErrorRecorder: responds to "add -u" (ok), "commit --amend" with error.
type amendErrorRecorder struct {
	tmpDir string
	calls  []string
}

func (r *amendErrorRecorder) Git(args string, output *string) error {
	r.calls = append(r.calls, args)
	if args == "commit --amend --no-edit" {
		return errors.New("amend failed")
	}
	return nil
}

func (r *amendErrorRecorder) MustGit(args string, output *string) {
	if err := r.Git(args, output); err != nil {
		panic(err)
	}
}
func (r *amendErrorRecorder) GitWithEditor(args string, output *string, editorCmd string) error {
	return r.Git(args, output)
}
func (r *amendErrorRecorder) RootDir() string                                           { return r.tmpDir }
func (r *amendErrorRecorder) GitDir() string                                            { return r.tmpDir + "/.git" }
func (r *amendErrorRecorder) DeleteRemoteBranch(_ context.Context, branch string) error { return nil }

// abortErrorRecorder: "rebase --abort" returns an error.
type abortErrorRecorder struct {
	tmpDir string
	calls  []string
}

func (r *abortErrorRecorder) Git(args string, output *string) error {
	r.calls = append(r.calls, args)
	if args == "rebase --abort" {
		return errors.New("abort failed")
	}
	return nil
}

func (r *abortErrorRecorder) MustGit(args string, output *string) {
	if err := r.Git(args, output); err != nil {
		panic(err)
	}
}
func (r *abortErrorRecorder) GitWithEditor(args string, output *string, editorCmd string) error {
	return r.Git(args, output)
}
func (r *abortErrorRecorder) RootDir() string                                           { return r.tmpDir }
func (r *abortErrorRecorder) GitDir() string                                            { return r.tmpDir + "/.git" }
func (r *abortErrorRecorder) DeleteRemoteBranch(_ context.Context, branch string) error { return nil }

// conflictContinueErrorRecorder: "rebase --continue" returns an error.
type conflictContinueErrorRecorder struct {
	tmpDir string
	calls  []string
}

func (r *conflictContinueErrorRecorder) Git(args string, output *string) error {
	r.calls = append(r.calls, args)
	if args == "rebase --continue" {
		return errors.New("still conflicted")
	}
	return nil
}

func (r *conflictContinueErrorRecorder) MustGit(args string, output *string) {
	if err := r.Git(args, output); err != nil {
		panic(err)
	}
}
func (r *conflictContinueErrorRecorder) GitWithEditor(args string, output *string, editorCmd string) error {
	return r.Git(args, output)
}
func (r *conflictContinueErrorRecorder) RootDir() string { return r.tmpDir }
func (r *conflictContinueErrorRecorder) GitDir() string  { return r.tmpDir + "/.git" }
func (r *conflictContinueErrorRecorder) DeleteRemoteBranch(_ context.Context, branch string) error {
	return nil
}

// stashErrorRecorder: returns non-empty status (dirty tree) and errors on stash.
type stashErrorRecorder struct{}

func (r *stashErrorRecorder) Git(args string, output *string) error {
	if strings.HasPrefix(args, "status") {
		if output != nil {
			*output = " M some_file.go"
		}
		return nil
	}
	if strings.HasPrefix(args, "stash") && !strings.Contains(args, "pop") {
		return errors.New("stash failed")
	}
	return nil
}

func (r *stashErrorRecorder) MustGit(args string, output *string) {
	if err := r.Git(args, output); err != nil {
		panic(err)
	}
}
func (r *stashErrorRecorder) GitWithEditor(args string, output *string, editorCmd string) error {
	return r.Git(args, output)
}
func (r *stashErrorRecorder) RootDir() string                                           { return "" }
func (r *stashErrorRecorder) GitDir() string                                            { return "/.git" }
func (r *stashErrorRecorder) DeleteRemoteBranch(_ context.Context, branch string) error { return nil }

// dirtyTreeRecorder: simulates a dirty tree and records all git calls.
type dirtyTreeRecorder struct {
	calls []string
}

func (r *dirtyTreeRecorder) Git(args string, output *string) error {
	r.calls = append(r.calls, args)
	if strings.HasPrefix(args, "status") && output != nil {
		*output = " M some_file.go"
	}
	return nil
}

func (r *dirtyTreeRecorder) MustGit(args string, output *string) { _ = r.Git(args, output) }
func (r *dirtyTreeRecorder) GitWithEditor(args string, output *string, editorCmd string) error {
	return r.Git(args, output)
}
func (r *dirtyTreeRecorder) RootDir() string                                           { return "" }
func (r *dirtyTreeRecorder) GitDir() string                                            { return "/.git" }
func (r *dirtyTreeRecorder) DeleteRemoteBranch(_ context.Context, branch string) error { return nil }

// ---------------------------------------------------------------------------
// buildCommitLogOutput produces git log --format=medium output that
// parseLocalCommitStack expects.
// ---------------------------------------------------------------------------

func buildCommitLogOutput(commits []*git.Commit) string {
	var b strings.Builder
	for _, c := range commits {
		fmt.Fprintf(&b, "commit %s\n", c.CommitHash)
		fmt.Fprintf(&b, "Author: Test User <test@example.com>\n")
		fmt.Fprintf(&b, "Date:   Fri Jun 11 14:15:49 2021 -0700\n")
		fmt.Fprintf(&b, "\n")
		fmt.Fprintf(&b, "\t%s\n", c.Subject)
		fmt.Fprintf(&b, "\n")
		fmt.Fprintf(&b, "\tcommit-id:%s\n", c.CommitID)
		fmt.Fprintf(&b, "\n")
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// SyncStack
// ---------------------------------------------------------------------------

func TestSyncStackEmpty(t *testing.T) {
	s, _, githubmock, _, output := makeTestObjects(t, true)
	ctx := context.Background()

	githubmock.ExpectGetInfo()
	s.SyncStack(ctx)
	require.Equal(t, "pull request stack is empty\n", output.String())
	githubmock.ExpectationsMet()
}

func TestSyncStackNonEmpty(t *testing.T) {
	s, _, githubmock, _, _ := makeTestObjects(t, true)
	ctx := context.Background()

	c1 := git.Commit{
		CommitID:   "00000001",
		CommitHash: "c100000000000000000000000000000000000000",
		Subject:    "test commit 1",
	}
	c2 := git.Commit{
		CommitID:   "00000002",
		CommitHash: "c200000000000000000000000000000000000000",
		Subject:    "test commit 2",
	}

	githubmock.Info.PullRequests = []*github.PullRequest{
		{Number: 1, Commit: c1},
		{Number: 2, Commit: c2},
	}

	recorder := &gitRecorder{}
	s.gitcmd = recorder

	githubmock.ExpectGetInfo()
	s.SyncStack(ctx)

	// Should have called cherry-pick against the last PR's commit hash.
	require.Len(t, recorder.calls, 1)
	require.Equal(t, "cherry-pick .."+c2.CommitHash, recorder.calls[0])
	githubmock.ExpectationsMet()
}

// ---------------------------------------------------------------------------
// RunMergeCheck
// ---------------------------------------------------------------------------

func TestRunMergeCheckNotConfigured(t *testing.T) {
	defer silenceStdout(t)()
	s, _, _, _, _ := makeTestObjects(t, true)
	ctx := context.Background()
	s.config.Repo.MergeCheck = ""
	// Prints guidance to real stdout, returns immediately. Just assert no panic.
	s.RunMergeCheck(ctx)
}

func TestRunMergeCheckNoLocalCommits(t *testing.T) {
	defer silenceStdout(t)()
	s, _, _, _, _ := makeTestObjects(t, true)
	ctx := context.Background()

	s.config.Repo.MergeCheck = "/usr/bin/true"

	// GetLocalCommitStack calls git log; return empty output → no commits.
	recorder := &gitRecorder{}
	s.gitcmd = recorder

	// Prints "no local commits" to real stdout, returns. No panic.
	s.RunMergeCheck(ctx)
	require.Len(t, recorder.calls, 1)
	require.Contains(t, recorder.calls[0], "log")
}

func TestRunMergeCheckPassed(t *testing.T) {
	defer silenceStdout(t)()
	s, _, githubmock, _, _ := makeTestObjects(t, true)
	ctx := context.Background()

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	s.config.Repo.MergeCheck = "/usr/bin/true" // exits 0 → PASSED

	c1 := git.Commit{
		CommitID:   "00000001",
		CommitHash: "c100000000000000000000000000000000000000",
		Subject:    "test commit 1",
	}

	s.gitcmd = &gitLogResponder{
		logResponse: buildCommitLogOutput([]*git.Commit{&c1}),
	}

	githubmock.ExpectGetInfo()
	s.RunMergeCheck(ctx)

	key := githubmock.Info.Key()
	require.Equal(t, c1.CommitHash, s.config.State.MergeCheckCommit[key])
	githubmock.ExpectationsMet()
}

func TestRunMergeCheckFailed(t *testing.T) {
	defer silenceStdout(t)()
	s, _, githubmock, _, _ := makeTestObjects(t, true)
	ctx := context.Background()

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	s.config.Repo.MergeCheck = "/usr/bin/false" // exits 1 → FAILED

	c1 := git.Commit{
		CommitID:   "00000001",
		CommitHash: "c100000000000000000000000000000000000000",
		Subject:    "test commit 1",
	}

	s.gitcmd = &gitLogResponder{
		logResponse: buildCommitLogOutput([]*git.Commit{&c1}),
	}

	githubmock.ExpectGetInfo()
	s.RunMergeCheck(ctx)

	key := githubmock.Info.Key()
	require.Equal(t, "", s.config.State.MergeCheckCommit[key])
	githubmock.ExpectationsMet()
}

// ---------------------------------------------------------------------------
// ProfilingEnable / ProfilingSummary
// ---------------------------------------------------------------------------

func TestProfilingEnableAndSummary(t *testing.T) {
	defer silenceStdout(t)()
	s, _, _, _, _ := makeTestObjects(t, true)
	s.ProfilingEnable()
	s.profiletimer.Step("TestStep")
	// ShowResults prints to stdout; just assert no panic.
	s.ProfilingSummary()
}

// ---------------------------------------------------------------------------
// header
// ---------------------------------------------------------------------------

func TestHeaderWithEmojis(t *testing.T) {
	cfg := config.EmptyConfig()
	cfg.User.StatusBitsEmojis = true
	h := header(cfg)
	require.Contains(t, h, "github checks pass")
	require.Contains(t, h, "┌─")
}

func TestHeaderWithoutEmojis(t *testing.T) {
	cfg := config.EmptyConfig()
	cfg.User.StatusBitsEmojis = false
	h := header(cfg)
	require.Contains(t, h, "github checks pass")
	require.Contains(t, h, "│┌──")
}

// ---------------------------------------------------------------------------
// check — panic branch (SPR_DEBUG=1)
// ---------------------------------------------------------------------------

func TestCheckPanicsWithSPRDebug(t *testing.T) {
	t.Setenv("SPR_DEBUG", "1")
	require.Panics(t, func() {
		check(errors.New("test error"))
	})
}

func TestCheckNoopWithNilError(t *testing.T) {
	t.Setenv("SPR_DEBUG", "1")
	require.NotPanics(t, func() {
		check(nil)
	})
}

// ---------------------------------------------------------------------------
// fetchAndGetGitHubInfo
// ---------------------------------------------------------------------------

func TestFetchAndGetGitHubInfoForceFetchTags(t *testing.T) {
	s, _, githubmock, _, _ := makeTestObjects(t, true)
	ctx := context.Background()

	s.config.Repo.ForceFetchTags = true

	recorder := &fetchRecorder{}
	s.gitcmd = recorder

	githubmock.ExpectGetInfo()
	info := s.fetchAndGetGitHubInfo(ctx)
	require.NotNil(t, info)

	require.GreaterOrEqual(t, len(recorder.calls), 2)
	require.Equal(t, "fetch --tags --force", recorder.calls[0])
	require.Contains(t, recorder.calls[1], "rebase")
	githubmock.ExpectationsMet()
}

func TestFetchAndGetGitHubInfoRemotePRBranch(t *testing.T) {
	defer silenceStdout(t)()
	s, _, githubmock, _, _ := makeTestObjects(t, true)
	ctx := context.Background()

	// LocalBranch matches BranchNameRegex("spr") → should return nil.
	githubmock.Info.LocalBranch = "spr/master/00000001"

	recorder := &fetchRecorder{}
	s.gitcmd = recorder

	githubmock.ExpectGetInfo()
	info := s.fetchAndGetGitHubInfo(ctx)
	require.Nil(t, info, "should return nil when on a remote PR branch")
	githubmock.ExpectationsMet()
}

func TestFetchAndGetGitHubInfoRebaseError(t *testing.T) {
	s, _, githubmock, _, _ := makeTestObjects(t, true)
	ctx := context.Background()

	recorder := &fetchRecorder{rebaseErr: errors.New("rebase conflict")}
	s.gitcmd = recorder

	info := s.fetchAndGetGitHubInfo(ctx)
	require.Nil(t, info, "should return nil when rebase fails")
	githubmock.ExpectationsMet()
}

// ---------------------------------------------------------------------------
// syncCommitStackToGitHub — BranchPushIndividually path
// ---------------------------------------------------------------------------

func TestSyncCommitStackBranchPushIndividually(t *testing.T) {
	s, _, githubmock, _, _ := makeTestObjects(t, true)
	ctx := context.Background()

	s.config.Repo.BranchPushIndividually = true

	c1 := git.Commit{
		CommitID:   "00000001",
		CommitHash: "c100000000000000000000000000000000000000",
		Subject:    "test commit 1",
	}

	recorder := &pushRecorder{}
	s.gitcmd = recorder

	info := githubmock.Info
	ok := s.syncCommitStackToGitHub(ctx, []git.Commit{c1}, info)
	require.True(t, ok)

	// Verify: status call first, then individual push (not --atomic).
	require.GreaterOrEqual(t, len(recorder.calls), 2)
	require.Contains(t, recorder.calls[0], "status")
	found := false
	for _, call := range recorder.calls[1:] {
		if strings.Contains(call, "push --force origin") && !strings.Contains(call, "--atomic") {
			found = true
		}
	}
	require.True(t, found, "expected individual push without --atomic, got: %v", recorder.calls)
}

// ---------------------------------------------------------------------------
// syncCommitStackToGitHub — dirty tree stash path
// ---------------------------------------------------------------------------

func TestSyncCommitStackDirtyTreeStash(t *testing.T) {
	s, _, githubmock, _, _ := makeTestObjects(t, true)
	ctx := context.Background()

	recorder := &dirtyTreeRecorder{}
	s.gitcmd = recorder

	c1 := git.Commit{
		CommitID:   "00000001",
		CommitHash: "c100000000000000000000000000000000000000",
		Subject:    "test commit 1",
	}

	ok := s.syncCommitStackToGitHub(ctx, []git.Commit{c1}, githubmock.Info)
	require.True(t, ok)

	require.Contains(t, recorder.calls, "stash", "stash should be called")
	require.Contains(t, recorder.calls, "stash pop", "stash pop should be called")
}

// ---------------------------------------------------------------------------
// syncCommitStackToGitHub — stash error → returns false
// ---------------------------------------------------------------------------

func TestSyncCommitStackStashError(t *testing.T) {
	s, _, githubmock, _, _ := makeTestObjects(t, true)
	ctx := context.Background()

	recorder := &stashErrorRecorder{}
	s.gitcmd = recorder

	c1 := git.Commit{
		CommitID:   "00000001",
		CommitHash: "c100000000000000000000000000000000000000",
		Subject:    "test commit 1",
	}

	ok := s.syncCommitStackToGitHub(ctx, []git.Commit{c1}, githubmock.Info)
	require.False(t, ok, "should return false when stash fails")
}

// ---------------------------------------------------------------------------
// EditCommitDone — update=true path
// ---------------------------------------------------------------------------

func TestEditCommitDoneWithUpdate(t *testing.T) {
	s, gitmock, githubmock, _, output := makeTestObjects(t, true)
	ctx := context.Background()

	tmpDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755))
	gitmock.SetRootDir(tmpDir)

	stateFile := filepath.Join(tmpDir, ".git", "spr_edit_state")
	require.NoError(t, os.WriteFile(stateFile, []byte("commit_id=00000001\n"), 0644))

	// No REBASE_HEAD → initial edit stop path.
	gitmock.ExpectEditDoneAmend()

	// UpdatePullRequests calls:
	// 1. fetchAndGetGitHubInfo: fetch, rebase, GetInfo
	// 2. GetLocalCommitStack: log
	// 3. syncCommitStackToGitHub: status (no commits to push since log returns empty)
	// 4. StatusPullRequests: GetInfo
	gitmock.ExpectFetch()
	gitmock.ExpectLogAndRespond([]*git.Commit{})
	gitmock.ExpectStatus()
	githubmock.ExpectGetInfo()
	githubmock.ExpectGetInfo()

	s.EditCommitDone(ctx, true)

	require.Contains(t, output.String(), "Stack restored successfully")
	_, err := os.Stat(stateFile)
	require.True(t, os.IsNotExist(err), "state file should be removed")

	gitmock.ExpectationsMet()
	githubmock.ExpectationsMet()
}

// ---------------------------------------------------------------------------
// EditCommitDone — amend failure path
// ---------------------------------------------------------------------------

func TestEditCommitDoneAmendFails(t *testing.T) {
	s, _, _, _, output := makeTestObjects(t, true)
	ctx := context.Background()

	tmpDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755))

	stateFile := filepath.Join(tmpDir, ".git", "spr_edit_state")
	require.NoError(t, os.WriteFile(stateFile, []byte("commit_id=00000001\n"), 0644))

	s.gitcmd = &amendErrorRecorder{tmpDir: tmpDir}

	s.EditCommitDone(ctx, false)

	require.Contains(t, output.String(), "Failed to amend commit")
	// State file must still exist.
	_, err := os.Stat(stateFile)
	require.NoError(t, err, "state file should still exist after amend failure")
}

// ---------------------------------------------------------------------------
// EditCommitAbort — abort failure path
// ---------------------------------------------------------------------------

func TestEditCommitAbortFails(t *testing.T) {
	s, _, _, _, output := makeTestObjects(t, true)
	ctx := context.Background()

	tmpDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755))

	stateFile := filepath.Join(tmpDir, ".git", "spr_edit_state")
	require.NoError(t, os.WriteFile(stateFile, []byte("commit_id=00000001\n"), 0644))

	s.gitcmd = &abortErrorRecorder{tmpDir: tmpDir}

	s.EditCommitAbort(ctx)

	require.Contains(t, output.String(), "Failed to abort")
	// State file should still exist (abort did not complete).
	_, err := os.Stat(stateFile)
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// EditCommitDone — conflict resolution fails
// ---------------------------------------------------------------------------

func TestEditCommitDoneConflictResolutionFails(t *testing.T) {
	s, _, _, _, output := makeTestObjects(t, true)
	ctx := context.Background()

	tmpDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755))

	stateFile := filepath.Join(tmpDir, ".git", "spr_edit_state")
	require.NoError(t, os.WriteFile(stateFile, []byte("commit_id=00000001\n"), 0644))

	// REBASE_HEAD exists → conflict resolution path.
	rebaseHead := filepath.Join(tmpDir, ".git", "REBASE_HEAD")
	require.NoError(t, os.WriteFile(rebaseHead, []byte("abc123\n"), 0644))

	s.gitcmd = &conflictContinueErrorRecorder{tmpDir: tmpDir}

	s.EditCommitDone(ctx, false)

	require.Contains(t, output.String(), "Rebase conflict detected")
	// State file should still exist.
	_, err := os.Stat(stateFile)
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// EditCommit — GitWithEditor fails → cleanup
// ---------------------------------------------------------------------------

func TestEditCommitGitWithEditorFails(t *testing.T) {
	s, _, _, input, output := makeTestObjects(t, true)
	ctx := context.Background()

	tmpDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755))

	c1 := git.Commit{
		CommitID:   "00000001",
		CommitHash: "c100000000000000000000000000000000000000",
		Subject:    "test commit 1",
	}

	s.gitcmd = &editStartErrorRecorder{
		tmpDir:      tmpDir,
		logResponse: buildCommitLogOutput([]*git.Commit{&c1}),
		rebaseErr:   errors.New("rebase -i failed"),
	}

	input.WriteString("1\n")
	s.EditCommit(ctx)

	require.Contains(t, output.String(), "Failed to start edit session")
	// State file should be cleaned up.
	stateFile := filepath.Join(tmpDir, ".git", "spr_edit_state")
	_, err := os.Stat(stateFile)
	require.True(t, os.IsNotExist(err), "state file should be cleaned up on failure")
}

// ---------------------------------------------------------------------------
// MergePullRequests — MergeCheck not yet run (no key in map)
// ---------------------------------------------------------------------------

func TestMergePullRequestsMergeCheckRequired(t *testing.T) {
	s, _, githubmock, _, _ := makeTestObjects(t, true)
	ctx := context.Background()

	s.config.Repo.MergeCheck = "/usr/bin/true"
	// MergeCheckCommit map is empty → "not found" branch → check(err) fires.

	c1 := git.Commit{
		CommitID:   "00000001",
		CommitHash: "c100000000000000000000000000000000000000",
		Subject:    "test commit 1",
	}
	githubmock.Info.PullRequests = []*github.PullRequest{
		{
			Number: 1, Commit: c1,
			MergeStatus: github.PullRequestMergeStatus{
				ChecksPass:     github.CheckStatusPass,
				ReviewApproved: true,
				NoConflicts:    true,
				Stacked:        true,
			},
		},
	}
	githubmock.ExpectGetInfo()

	s.gitcmd = &gitLogResponder{
		logResponse: buildCommitLogOutput([]*git.Commit{&c1}),
	}

	t.Setenv("SPR_DEBUG", "1")
	require.Panics(t, func() {
		s.MergePullRequests(ctx, nil)
	})
	githubmock.ExpectationsMet()
}

// TestMergePullRequestsMergeCheckSkip verifies that SKIP bypasses the hash check.
func TestMergePullRequestsMergeCheckSkip(t *testing.T) {
	s, _, githubmock, _, output := makeTestObjects(t, true)
	ctx := context.Background()

	s.config.Repo.MergeCheck = "/usr/bin/true"
	key := githubmock.Info.Key()
	s.config.State.MergeCheckCommit[key] = "SKIP"

	c1 := git.Commit{
		CommitID:   "00000001",
		CommitHash: "c100000000000000000000000000000000000000",
		Subject:    "test commit 1",
	}
	githubmock.Info.PullRequests = []*github.PullRequest{
		{
			Number: 1, Commit: c1, FromBranch: "from_branch",
			MergeStatus: github.PullRequestMergeStatus{
				ChecksPass:     github.CheckStatusPass,
				ReviewApproved: true,
				NoConflicts:    true,
				Stacked:        true,
			},
		},
	}
	githubmock.ExpectGetInfo()

	s.gitcmd = &gitLogResponder{
		logResponse: buildCommitLogOutput([]*git.Commit{&c1}),
	}

	githubmock.ExpectUpdatePullRequest(c1, nil)
	githubmock.ExpectMergePullRequest(c1, genclient.PullRequestMergeMethod_REBASE)

	s.MergePullRequests(ctx, nil)
	require.Contains(t, output.String(), "MERGED")
	githubmock.ExpectationsMet()
}

// TestMergePullRequestsMergeCheckHashMismatch: stored hash ≠ current → check fires.
func TestMergePullRequestsMergeCheckHashMismatch(t *testing.T) {
	s, _, githubmock, _, _ := makeTestObjects(t, true)
	ctx := context.Background()

	s.config.Repo.MergeCheck = "/usr/bin/true"
	key := githubmock.Info.Key()
	s.config.State.MergeCheckCommit[key] = "staleHash"

	c1 := git.Commit{
		CommitID:   "00000001",
		CommitHash: "c100000000000000000000000000000000000000",
		Subject:    "test commit 1",
	}
	githubmock.Info.PullRequests = []*github.PullRequest{
		{
			Number: 1, Commit: c1,
			MergeStatus: github.PullRequestMergeStatus{
				ChecksPass:     github.CheckStatusPass,
				ReviewApproved: true,
				NoConflicts:    true,
				Stacked:        true,
			},
		},
	}
	githubmock.ExpectGetInfo()

	s.gitcmd = &gitLogResponder{
		logResponse: buildCommitLogOutput([]*git.Commit{&c1}),
	}

	t.Setenv("SPR_DEBUG", "1")
	require.Panics(t, func() {
		s.MergePullRequests(ctx, nil)
	})
	githubmock.ExpectationsMet()
}

// ---------------------------------------------------------------------------
// MergePullRequests — no mergeable PRs (prIndex stays -1)
// ---------------------------------------------------------------------------

func TestMergePullRequestsNoMergeablePRs(t *testing.T) {
	s, _, githubmock, _, _ := makeTestObjects(t, true)
	ctx := context.Background()

	c1 := git.Commit{
		CommitID:   "00000001",
		CommitHash: "c100000000000000000000000000000000000000",
		Subject:    "test commit 1",
	}
	// ChecksPass = Fail → not mergeable.
	githubmock.Info.PullRequests = []*github.PullRequest{
		{
			Number: 1, Commit: c1,
			MergeStatus: github.PullRequestMergeStatus{
				ChecksPass:     github.CheckStatusFail,
				ReviewApproved: true,
				NoConflicts:    true,
				Stacked:        true,
			},
		},
	}
	githubmock.ExpectGetInfo()

	t.Setenv("SPR_DEBUG", "1")
	require.Panics(t, func() {
		s.MergePullRequests(ctx, nil)
	})
	githubmock.ExpectationsMet()
}

// TestMergePullRequestsEmptyStack covers the prIndex == -1 path when there are no PRs.
func TestMergePullRequestsEmptyStack(t *testing.T) {
	s, _, githubmock, _, _ := makeTestObjects(t, true)
	ctx := context.Background()

	githubmock.Info.PullRequests = nil
	githubmock.ExpectGetInfo()

	t.Setenv("SPR_DEBUG", "1")
	require.Panics(t, func() {
		s.MergePullRequests(ctx, nil)
	})
	githubmock.ExpectationsMet()
}

// ---------------------------------------------------------------------------
// StatusPullRequests — DetailEnabled (covers header() call)
// ---------------------------------------------------------------------------

func TestStatusPullRequestsDetailEnabled(t *testing.T) {
	s, _, githubmock, _, output := makeTestObjects(t, true)
	ctx := context.Background()

	s.DetailEnabled = true
	s.config.User.StatusBitsEmojis = true

	c1 := git.Commit{
		CommitID:   "00000001",
		CommitHash: "c100000000000000000000000000000000000000",
		Subject:    "test commit 1",
	}
	githubmock.Info.PullRequests = []*github.PullRequest{
		{Number: 1, Title: "test commit 1", Commit: c1},
	}

	githubmock.ExpectGetInfo()
	s.StatusPullRequests(ctx)

	out := output.String()
	require.Contains(t, out, "github checks pass", "header should appear when DetailEnabled=true")
	require.Contains(t, out, "test commit 1")
	githubmock.ExpectationsMet()
}

func TestStatusPullRequestsDetailEnabledNoEmojis(t *testing.T) {
	s, _, githubmock, _, output := makeTestObjects(t, true)
	ctx := context.Background()

	s.DetailEnabled = true
	s.config.User.StatusBitsEmojis = false

	c1 := git.Commit{
		CommitID:   "00000001",
		CommitHash: "c100000000000000000000000000000000000000",
		Subject:    "test commit 1",
	}
	githubmock.Info.PullRequests = []*github.PullRequest{
		{Number: 1, Title: "test commit 1", Commit: c1},
	}

	githubmock.ExpectGetInfo()
	s.StatusPullRequests(ctx)

	out := output.String()
	require.Contains(t, out, "github checks pass")
	require.Contains(t, out, "│┌──")
	githubmock.ExpectationsMet()
}

// ---------------------------------------------------------------------------
// addReviewers — unknown user → check() fires
// ---------------------------------------------------------------------------

func TestAddReviewersUnknownUser(t *testing.T) {
	s, _, _, _, _ := makeTestObjects(t, true)
	ctx := context.Background()

	pr := &github.PullRequest{Number: 1}
	assignable := []github.RepoAssignee{
		{ID: "U1", Login: "alice"},
	}

	t.Setenv("SPR_DEBUG", "1")
	require.Panics(t, func() {
		s.addReviewers(ctx, pr, []string{"unknownuser"}, assignable)
	})
}

// ---------------------------------------------------------------------------
// UpdatePullRequests — WIP commit stops loop before PR creation
// ---------------------------------------------------------------------------

func TestUpdatePullRequestsWIPCommit(t *testing.T) {
	s, gitmock, githubmock, _, output := makeTestObjects(t, true)
	ctx := context.Background()

	c1 := git.Commit{
		CommitID:   "00000001",
		CommitHash: "c100000000000000000000000000000000000000",
		Subject:    "WIP: work in progress",
		WIP:        true,
	}

	githubmock.ExpectGetInfo()
	gitmock.ExpectFetch()
	gitmock.ExpectLogAndRespond([]*git.Commit{&c1})
	// WIP commit: no push (nothing in updatedCommits).
	// syncCommitStackToGitHub still calls status.
	gitmock.ExpectStatus()
	githubmock.ExpectGetInfo()
	s.UpdatePullRequests(ctx, nil, nil)

	require.Contains(t, output.String(), "pull request stack is empty")
	gitmock.ExpectationsMet()
	githubmock.ExpectationsMet()
}

// ---------------------------------------------------------------------------
// UpdatePullRequests — count limiting
// ---------------------------------------------------------------------------

func TestUpdatePullRequestsWithCount(t *testing.T) {
	s, gitmock, githubmock, _, output := makeTestObjects(t, true)
	ctx := context.Background()

	c1 := git.Commit{
		CommitID:   "00000001",
		CommitHash: "c100000000000000000000000000000000000000",
		Subject:    "test commit 1",
	}
	c2 := git.Commit{
		CommitID:   "00000002",
		CommitHash: "c200000000000000000000000000000000000000",
		Subject:    "test commit 2",
	}

	githubmock.ExpectGetInfo()
	gitmock.ExpectFetch()
	gitmock.ExpectLogAndRespond([]*git.Commit{&c2, &c1})
	// count=1 limits PR creation to 1, but both commits are still pushed.
	gitmock.ExpectPushCommits([]*git.Commit{&c1, &c2})
	githubmock.ExpectCreatePullRequest(c1, nil)
	githubmock.ExpectGetAssignableUsers()
	githubmock.ExpectAddReviewers([]string{mockclient.NobodyUserID})
	githubmock.ExpectUpdatePullRequest(c1, nil)
	githubmock.ExpectGetInfo()

	count := uint(1)
	s.UpdatePullRequests(ctx, []string{mockclient.NobodyLogin}, &count)

	out := output.String()
	require.Contains(t, out, "test commit 1")
	// c2 PR was not created because count=1; but c2 may still appear in status if any PR was created for it.
	// The status shows all PRs from GetInfo. Since we only created PR for c1, c2 does not appear.

	gitmock.ExpectationsMet()
	githubmock.ExpectationsMet()
}

// ---------------------------------------------------------------------------
// RunMergeCheck — multi-word command (else branch in exec.Command)
// ---------------------------------------------------------------------------

func TestRunMergeCheckMultiWordCommandPassed(t *testing.T) {
	defer silenceStdout(t)()
	s, _, githubmock, _, _ := makeTestObjects(t, true)
	ctx := context.Background()

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	// Multi-word command: "/usr/bin/env true" → len(splitCmd) > 1 → else branch.
	s.config.Repo.MergeCheck = "/usr/bin/env true"

	c1 := git.Commit{
		CommitID:   "00000001",
		CommitHash: "c100000000000000000000000000000000000000",
		Subject:    "test commit 1",
	}

	s.gitcmd = &gitLogResponder{
		logResponse: buildCommitLogOutput([]*git.Commit{&c1}),
	}

	githubmock.ExpectGetInfo()
	s.RunMergeCheck(ctx)

	key := githubmock.Info.Key()
	require.Equal(t, c1.CommitHash, s.config.State.MergeCheckCommit[key])
	githubmock.ExpectationsMet()
}

func TestRunMergeCheckMultiWordCommandFailed(t *testing.T) {
	defer silenceStdout(t)()
	s, _, githubmock, _, _ := makeTestObjects(t, true)
	ctx := context.Background()

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	// Multi-word command: "/usr/bin/env false" → fails.
	s.config.Repo.MergeCheck = "/usr/bin/env false"

	c1 := git.Commit{
		CommitID:   "00000001",
		CommitHash: "c100000000000000000000000000000000000000",
		Subject:    "test commit 1",
	}

	s.gitcmd = &gitLogResponder{
		logResponse: buildCommitLogOutput([]*git.Commit{&c1}),
	}

	githubmock.ExpectGetInfo()
	s.RunMergeCheck(ctx)

	key := githubmock.Info.Key()
	require.Equal(t, "", s.config.State.MergeCheckCommit[key])
	githubmock.ExpectationsMet()
}

// ---------------------------------------------------------------------------
// UpdatePullRequests — syncCommitStackToGitHub returns false → early return
// ---------------------------------------------------------------------------

// updateStashErrorRecorder: returns non-empty status (dirty tree), errors on stash,
// but also handles the fetch + rebase + log calls from fetchAndGetGitHubInfo.
type updateStashErrorRecorder struct {
	calls []string
}

func (r *updateStashErrorRecorder) Git(args string, output *string) error {
	r.calls = append(r.calls, args)
	if strings.HasPrefix(args, "fetch") {
		return nil
	}
	if strings.HasPrefix(args, "rebase") {
		return nil
	}
	if strings.HasPrefix(args, "log") && output != nil {
		// Return one commit so syncCommitStackToGitHub has something to push.
		*output = buildCommitLogOutput([]*git.Commit{{
			CommitID:   "00000001",
			CommitHash: "c100000000000000000000000000000000000000",
			Subject:    "test commit 1",
		}})
		return nil
	}
	if strings.HasPrefix(args, "status") {
		if output != nil {
			*output = " M dirty.go"
		}
		return nil
	}
	if args == "stash" {
		return errors.New("stash failed")
	}
	return nil
}

func (r *updateStashErrorRecorder) MustGit(args string, output *string) {
	if err := r.Git(args, output); err != nil {
		panic(err)
	}
}
func (r *updateStashErrorRecorder) GitWithEditor(args string, output *string, editorCmd string) error {
	return r.Git(args, output)
}
func (r *updateStashErrorRecorder) RootDir() string { return "" }
func (r *updateStashErrorRecorder) GitDir() string  { return "/.git" }
func (r *updateStashErrorRecorder) DeleteRemoteBranch(_ context.Context, branch string) error {
	return nil
}

func TestUpdatePullRequestsSyncStackFails(t *testing.T) {
	s, _, githubmock, _, output := makeTestObjects(t, true)
	ctx := context.Background()

	recorder := &updateStashErrorRecorder{}
	s.gitcmd = recorder

	githubmock.ExpectGetInfo()
	s.UpdatePullRequests(ctx, nil, nil)

	// When syncCommitStackToGitHub returns false, UpdatePullRequests returns early.
	// No status output is printed.
	require.Empty(t, output.String())
	githubmock.ExpectationsMet()
}

// ---------------------------------------------------------------------------
// UpdatePullRequests — fetchAndGetGitHubInfo returns nil (rebase error path)
// covers line 294: if githubInfo == nil { return }
// ---------------------------------------------------------------------------

// rebaseErrorRecorder returns an error for the rebase command to cause
// fetchAndGetGitHubInfo to return nil, which makes UpdatePullRequests return early.
type rebaseErrorRecorder struct {
	calls []string
}

func (r *rebaseErrorRecorder) Git(args string, output *string) error {
	r.calls = append(r.calls, args)
	if strings.HasPrefix(args, "rebase") {
		return errors.New("rebase conflict")
	}
	return nil
}

func (r *rebaseErrorRecorder) MustGit(args string, output *string) {
	if err := r.Git(args, output); err != nil {
		panic(err)
	}
}
func (r *rebaseErrorRecorder) GitWithEditor(args string, output *string, editorCmd string) error {
	return r.Git(args, output)
}
func (r *rebaseErrorRecorder) RootDir() string { return "" }
func (r *rebaseErrorRecorder) GitDir() string  { return "/.git" }
func (r *rebaseErrorRecorder) DeleteRemoteBranch(_ context.Context, branch string) error {
	return nil
}

func TestUpdatePullRequestsFetchReturnsNil(t *testing.T) {
	s, _, githubmock, _, output := makeTestObjects(t, true)
	ctx := context.Background()

	// Rebase error → fetchAndGetGitHubInfo returns nil → UpdatePullRequests returns early.
	s.gitcmd = &rebaseErrorRecorder{}

	// No github calls should happen (GetInfo is NOT called since rebase fails before it).
	s.UpdatePullRequests(ctx, nil, nil)

	require.Empty(t, output.String())
	githubmock.ExpectationsMet()
}
