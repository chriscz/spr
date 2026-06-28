package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ejoffe/spr/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// test-local fake GitInterface
//
// mockgit (git/mockgit) imports "git", so using it inside `package git` tests
// would create an import cycle.  We use a minimal sequential fake instead.
// ---------------------------------------------------------------------------

type fakeCmd struct {
	args   string // expected args (without "git " prefix)
	output string // response written to the *string output pointer
	err    error  // error to return; if non-nil MustGit panics
}

type seqGit struct {
	t    testing.TB
	cmds []fakeCmd
	pos  int
}

func (s *seqGit) Git(args string, output *string) error {
	if s.pos >= len(s.cmds) {
		s.t.Fatalf("seqGit: unexpected command: git %s", args)
	}
	fc := s.cmds[s.pos]
	s.pos++
	if fc.args != args {
		s.t.Fatalf("seqGit: expected %q, got %q", fc.args, args)
	}
	if output != nil {
		*output = fc.output
	}
	return fc.err
}

func (s *seqGit) MustGit(args string, output *string) {
	if err := s.Git(args, output); err != nil {
		panic(err)
	}
}

func (s *seqGit) GitWithEditor(args string, output *string, _ string) error {
	return s.Git(args, output)
}

func (s *seqGit) RootDir() string { return "" }
func (s *seqGit) GitDir() string  { return "/.git" }

func (s *seqGit) DeleteRemoteBranch(_ context.Context, branch string) error {
	return s.Git(fmt.Sprintf("DeleteRemoteBranch(%s)", branch), nil)
}

func (s *seqGit) expectationsMet() {
	if s.pos != len(s.cmds) {
		s.t.Errorf("seqGit: %d command(s) left unconsumed", len(s.cmds)-s.pos)
	}
}

// compile-time check
var _ GitInterface = (*seqGit)(nil)

// commitLogEntry produces a git log --format=medium block for a Commit.
func commitLogEntry(c Commit) string {
	var b strings.Builder
	fmt.Fprintf(&b, "commit %s\n", c.CommitHash)
	fmt.Fprintf(&b, "Author: Test Author <test@example.com>\n")
	fmt.Fprintf(&b, "Date:   Mon Jan 01 00:00:00 2024 -0700\n")
	fmt.Fprintf(&b, "\n")
	fmt.Fprintf(&b, "\t%s\n", c.Subject)
	fmt.Fprintf(&b, "\n")
	fmt.Fprintf(&b, "\tcommit-id:%s\n", c.CommitID)
	fmt.Fprintf(&b, "\n")
	return b.String()
}

func logCommand(cfg *config.Config) string {
	return fmt.Sprintf("log --format=medium --no-color --no-abbrev-commit %s/%s..HEAD",
		cfg.Repo.GitHubRemote, cfg.Repo.GitHubBranch)
}

// testCfg returns a minimal config that matches the log command the helpers use.
func testCfg() *config.Config {
	cfg := config.EmptyConfig()
	cfg.Repo.GitHubRemote = "origin"
	cfg.Repo.GitHubBranch = "master"
	return cfg
}

// ---------------------------------------------------------------------------
// check() — error path
// ---------------------------------------------------------------------------

func TestCheck_PanicsOnError(t *testing.T) {
	sg := &seqGit{
		t: t,
		cmds: []fakeCmd{
			{args: "branch --no-color", err: errors.New("injected git error")},
		},
	}
	require.Panics(t, func() {
		GetLocalBranchName(sg)
	}, "check() must panic on non-nil error from Git")
	sg.expectationsMet()
}

// ---------------------------------------------------------------------------
// GetLocalBranchName
// ---------------------------------------------------------------------------

func TestGetLocalBranchName_Happy(t *testing.T) {
	sg := &seqGit{
		t: t,
		cmds: []fakeCmd{
			{args: "branch --no-color", output: "  other\n* main\n  dev"},
		},
	}
	branch := GetLocalBranchName(sg)
	assert.Equal(t, "main", branch)
	sg.expectationsMet()
}

func TestGetLocalBranchName_FirstBranch(t *testing.T) {
	sg := &seqGit{
		t: t,
		cmds: []fakeCmd{
			{args: "branch --no-color", output: "* feature-x\n  main"},
		},
	}
	branch := GetLocalBranchName(sg)
	assert.Equal(t, "feature-x", branch)
	sg.expectationsMet()
}

func TestGetLocalBranchName_NoBranchMarker_Panics(t *testing.T) {
	sg := &seqGit{
		t: t,
		cmds: []fakeCmd{
			{args: "branch --no-color", output: "  other\n  dev"},
		},
	}
	require.Panics(t, func() {
		GetLocalBranchName(sg)
	}, "expected panic when no '* ' line is present")
	sg.expectationsMet()
}

// ---------------------------------------------------------------------------
// GetLocalTopCommit
// ---------------------------------------------------------------------------

func TestGetLocalTopCommit_EmptyStack(t *testing.T) {
	cfg := testCfg()
	sg := &seqGit{
		t: t,
		cmds: []fakeCmd{
			{args: logCommand(cfg), output: ""},
		},
	}
	result := GetLocalTopCommit(cfg, sg)
	assert.Nil(t, result, "expected nil for empty commit stack")
	sg.expectationsMet()
}

func TestGetLocalTopCommit_NonEmpty(t *testing.T) {
	cfg := testCfg()
	bottom := Commit{CommitHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", CommitID: "aaaaaaaa", Subject: "bottom commit"}
	top := Commit{CommitHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", CommitID: "bbbbbbbb", Subject: "top commit"}
	// log order is top-to-bottom (most recent first)
	logOutput := commitLogEntry(top) + commitLogEntry(bottom)
	sg := &seqGit{
		t: t,
		cmds: []fakeCmd{
			{args: logCommand(cfg), output: logOutput},
		},
	}
	result := GetLocalTopCommit(cfg, sg)
	require.NotNil(t, result)
	assert.Equal(t, "bbbbbbbb", result.CommitID, "GetLocalTopCommit should return the top (most recent) commit")
	assert.Equal(t, "top commit", result.Subject)
	sg.expectationsMet()
}

// ---------------------------------------------------------------------------
// GetLocalCommitStack
// ---------------------------------------------------------------------------

func TestGetLocalCommitStack_EmptyLog(t *testing.T) {
	cfg := testCfg()
	sg := &seqGit{
		t: t,
		cmds: []fakeCmd{
			{args: logCommand(cfg), output: ""},
		},
	}
	commits := GetLocalCommitStack(cfg, sg)
	assert.Empty(t, commits)
	sg.expectationsMet()
}

func TestGetLocalCommitStack_SingleCommit(t *testing.T) {
	cfg := testCfg()
	c := Commit{CommitHash: "cccccccccccccccccccccccccccccccccccccccc", CommitID: "cccccccc", Subject: "single commit"}
	sg := &seqGit{
		t: t,
		cmds: []fakeCmd{
			{args: logCommand(cfg), output: commitLogEntry(c)},
		},
	}
	commits := GetLocalCommitStack(cfg, sg)
	require.Len(t, commits, 1)
	assert.Equal(t, "cccccccc", commits[0].CommitID)
	assert.Equal(t, "single commit", commits[0].Subject)
	sg.expectationsMet()
}

func TestGetLocalCommitStack_MultipleCommits_OrderBottomFirst(t *testing.T) {
	cfg := testCfg()
	top := Commit{CommitHash: "dddddddddddddddddddddddddddddddddddddddd", CommitID: "dddddddd", Subject: "top"}
	bottom := Commit{CommitHash: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", CommitID: "eeeeeeee", Subject: "bottom"}
	// log order: most recent first
	logOutput := commitLogEntry(top) + commitLogEntry(bottom)
	sg := &seqGit{
		t: t,
		cmds: []fakeCmd{
			{args: logCommand(cfg), output: logOutput},
		},
	}
	commits := GetLocalCommitStack(cfg, sg)
	require.Len(t, commits, 2)
	assert.Equal(t, "eeeeeeee", commits[0].CommitID, "bottom commit should be first")
	assert.Equal(t, "dddddddd", commits[1].CommitID, "top commit should be last")
	sg.expectationsMet()
}

// ---------------------------------------------------------------------------
// parseLocalCommitStack — additional edge cases to cover remaining branches
// ---------------------------------------------------------------------------

func TestParseLocalCommitStack_WIPCommit(t *testing.T) {
	commitLog := `
commit aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa (HEAD -> master)
Author: Test Author <test@example.com>
Date:   Mon Jan 01 00:00:00 2024 -0700

	WIP some unfinished work

	commit-id:aaaaaaaa
`
	commits, valid := parseLocalCommitStack(commitLog)
	require.True(t, valid)
	require.Len(t, commits, 1)
	assert.True(t, commits[0].WIP, "commit prefixed with 'WIP' should have WIP=true")
	assert.Equal(t, "WIP some unfinished work", commits[0].Subject)
	assert.Equal(t, "aaaaaaaa", commits[0].CommitID)
}

func TestParseLocalCommitStack_MissingCommitIDMidStream(t *testing.T) {
	// Two commits where the first (most recent in log order) has no commit-id
	// before the next hash is encountered.  This hits the
	// "commitScanOn == true when new hash seen" branch (lines 110-113).
	commitLog := `
commit aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa (HEAD -> master)
Author: Test Author <test@example.com>
Date:   Mon Jan 01 00:00:00 2024 -0700

	Missing id commit

commit bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
Author: Test Author <test@example.com>
Date:   Mon Jan 01 00:00:00 2024 -0700

	Second commit

	commit-id:bbbbbbbb
`
	commits, valid := parseLocalCommitStack(commitLog)
	assert.False(t, valid, "stack with missing commit-id mid-stream should be invalid")
	assert.Nil(t, commits)
}

func TestParseLocalCommitStack_EmptyLog(t *testing.T) {
	commits, valid := parseLocalCommitStack("")
	assert.True(t, valid, "empty log should be valid (no commits)")
	assert.Empty(t, commits)
}

// TestParseLocalCommitStack_PreservesBodyIndentation reproduces the reported
// bug where indented body lines lose their indentation in the resulting PR.
//
// `git log --format=medium` prefixes every message line with exactly four
// spaces. A user who indents a body line by four spaces (e.g. a fenced code
// block) therefore appears with EIGHT leading spaces in the raw log. The parser
// must strip only git's four-space prefix and preserve the user's relative
// indentation. The old code did strings.TrimSpace(line) per line, destroying
// all leading whitespace.
func TestParseLocalCommitStack_PreservesBodyIndentation(t *testing.T) {
	// Faithful to real `git log --format=medium` output: 4-space prefix per
	// line. The user indented two body lines by an extra 4 spaces (8 total).
	commitLog := "" +
		"commit aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n" +
		"Author: Test Author <test@example.com>\n" +
		"Date:   Mon Jan 01 00:00:00 2024 -0700\n" +
		"\n" +
		"    Subject line\n" +
		"    \n" +
		"    Example:\n" +
		"        indented code line\n" +
		"        second indented line\n" +
		"    back to normal\n" +
		"    \n" +
		"    commit-id:aaaaaaaa\n" +
		"\n"

	commits, valid := parseLocalCommitStack(commitLog)
	require.True(t, valid)
	require.Len(t, commits, 1)
	assert.Equal(t, "Subject line", commits[0].Subject)

	// The user's relative indentation (4 spaces) must survive parsing.
	assert.Contains(t, commits[0].Body, "    indented code line",
		"4-space user indentation must be preserved (only git's prefix stripped)")
	assert.Contains(t, commits[0].Body, "    second indented line")
	// Non-indented lines must NOT gain spurious leading whitespace.
	assert.Contains(t, commits[0].Body, "\nback to normal")
	assert.True(t, strings.HasPrefix(commits[0].Body, "Example:"),
		"body should start at the first non-blank content line, no leading blank/space")
}

func TestParseLocalCommitStack_BodyLineAtSubjectPlusOne(t *testing.T) {
	// Craft a commit log where a body line appears at exactly subjectIndex+1
	// (i.e., no blank line separator between subject and first body line).
	// This exercises the branch:
	//   } else if index == (subjectIndex+1) && line != "\n" {
	//
	// After strings.Split(log, "\n") the indices are:
	//   [0] ""                              <- leading empty from raw string
	//   [1] "commit <hash>"                <- hash at index 1; subjectIndex = 1+4 = 5
	//   [2] "Author: A <a@b.com>"
	//   [3] "Date: Mon Jan 1"
	//   [4] ""                             <- blank between header and message
	//   [5] "\tSubject line"              <- subjectIndex = 5
	//   [6] "\tbody line"                 <- subjectIndex+1, non-empty -> branch hit
	//   [7] "\tcommit-id:cccccccc"
	//   [8] ""
	commitLog := "\ncommit cccccccccccccccccccccccccccccccccccccccc\nAuthor: A <a@b.com>\nDate: Mon Jan 1\n\n\tSubject line\n\tbody line\n\tcommit-id:cccccccc\n"
	commits, valid := parseLocalCommitStack(commitLog)
	require.True(t, valid)
	require.Len(t, commits, 1)
	assert.Equal(t, "Subject line", commits[0].Subject)
	// The body line at subjectIndex+1 should be captured in Body
	assert.Contains(t, commits[0].Body, "body line")
}

// ---------------------------------------------------------------------------
// parseLocalCommitStack — abbreviated commit hashes (#213)
//
// With git config `log.abbrevCommit=true` (or `core.abbrev`) `git log` prints
// short hashes, e.g. `commit ca06022`.  The old `^commit ([a-f0-9]{40})` regex
// required exactly 40 hex chars, so nothing matched and spr reported an empty
// stack.  The parser must accept abbreviated (7..40 char) hashes.
// ---------------------------------------------------------------------------

func TestParseLocalCommitStack_AbbreviatedHash(t *testing.T) {
	// `git log` output as emitted with log.abbrevCommit=true: a short 7-char
	// hash on the `commit ` line (matches the example in the upstream issue).
	commitLog := `
commit ca06022
Author: Test Author <test@example.com>
Date:   Mon Jan 01 00:00:00 2024 -0700

	abbreviated hash commit

	commit-id:aaaaaaaa
`
	commits, valid := parseLocalCommitStack(commitLog)
	require.True(t, valid, "abbreviated-hash log should parse as valid")
	require.Len(t, commits, 1, "abbreviated hash must still be recognized as a commit")
	assert.Equal(t, "ca06022", commits[0].CommitHash)
	assert.Equal(t, "aaaaaaaa", commits[0].CommitID)
	assert.Equal(t, "abbreviated hash commit", commits[0].Subject)
}

// ---------------------------------------------------------------------------
// GetLocalCommitStack — git invocation immunizes against abbrev config (#213)
//
// Relaxing the parse regex alone still leaves spr at the mercy of `core.abbrev`
// (which can shorten hashes to as few as 4 chars).  GetLocalCommitStack must
// pass `--no-abbrev-commit` so git always emits full 40-hex hashes regardless
// of the user's config.
// ---------------------------------------------------------------------------

func TestGetLocalCommitStack_PassesNoAbbrevCommit(t *testing.T) {
	cfg := testCfg()
	c := Commit{CommitHash: "cccccccccccccccccccccccccccccccccccccccc", CommitID: "cccccccc", Subject: "single commit"}
	sg := &seqGit{
		t: t,
		cmds: []fakeCmd{
			{args: logCommand(cfg), output: commitLogEntry(c)},
		},
	}
	commits := GetLocalCommitStack(cfg, sg)
	require.Len(t, commits, 1)
	assert.Contains(t, logCommand(cfg), "--no-abbrev-commit",
		"the git log command must pass --no-abbrev-commit to immunize against log.abbrevCommit/core.abbrev")
	sg.expectationsMet()
}

// ---------------------------------------------------------------------------
// GetLocalCommitStack — rebase-recovery path
//
// When the first log parse is invalid (missing commit-id), GetLocalCommitStack
// calls exec.LookPath("spr_reword_helper") then GitWithEditor then MustGit
// a second time.  We place a stub executable in a temp dir and prepend it to
// PATH so LookPath succeeds without touching production code.
// ---------------------------------------------------------------------------

// stubRewordHelper creates a no-op shell script named "spr_reword_helper" in
// dir and prepends dir to PATH for the duration of the test.
func stubRewordHelper(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	stub := filepath.Join(dir, "spr_reword_helper")
	err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0o755)
	require.NoError(t, err)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestGetLocalCommitStack_RebaseRecovery_Success(t *testing.T) {
	// First log returns invalid (no commit-id), rebase runs, second log is valid.
	stubRewordHelper(t)

	cfg := testCfg()
	logCmd := logCommand(cfg)
	rebaseCmd := fmt.Sprintf("rebase %s/%s -i --autosquash --autostash",
		cfg.Repo.GitHubRemote, cfg.Repo.GitHubBranch)

	validCommit := Commit{
		CommitHash: "ffffffffffffffffffffffffffffffffffffffff",
		CommitID:   "ffffffff",
		Subject:    "fixed commit",
	}

	sg := &seqGit{
		t: t,
		cmds: []fakeCmd{
			// First log — no commit-id → invalid parse
			{args: logCmd, output: "commit ffffffffffffffffffffffffffffffffffffffff\nAuthor: A\nDate: D\n\n\tno id here\n\n"},
			// GitWithEditor for the rebase (seqGit.GitWithEditor calls Git)
			{args: rebaseCmd, output: ""},
			// Second log — now valid
			{args: logCmd, output: commitLogEntry(validCommit)},
		},
	}

	commits := GetLocalCommitStack(cfg, sg)
	require.Len(t, commits, 1)
	assert.Equal(t, "ffffffff", commits[0].CommitID)
	assert.Equal(t, "fixed commit", commits[0].Subject)
	sg.expectationsMet()
}

func TestGetLocalCommitStack_RebaseRecovery_StillInvalid_Panics(t *testing.T) {
	// Both log calls return invalid — expect the final panic.
	stubRewordHelper(t)

	cfg := testCfg()
	logCmd := logCommand(cfg)
	rebaseCmd := fmt.Sprintf("rebase %s/%s -i --autosquash --autostash",
		cfg.Repo.GitHubRemote, cfg.Repo.GitHubBranch)

	invalidLog := "commit ffffffffffffffffffffffffffffffffffffffff\nAuthor: A\nDate: D\n\n\tno id here\n\n"

	sg := &seqGit{
		t: t,
		cmds: []fakeCmd{
			{args: logCmd, output: invalidLog},
			{args: rebaseCmd, output: ""},
			{args: logCmd, output: invalidLog},
		},
	}

	require.Panics(t, func() {
		GetLocalCommitStack(cfg, sg)
	}, "GetLocalCommitStack should panic when stack is still invalid after rebase")
	sg.expectationsMet()
}
