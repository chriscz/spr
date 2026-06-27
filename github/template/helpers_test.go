package template

import (
	"strings"
	"testing"

	"github.com/ejoffe/spr/git"
	"github.com/ejoffe/spr/github"
	"github.com/stretchr/testify/assert"
)

func TestManualMergeNotice(t *testing.T) {
	notice := ManualMergeNotice()
	assert.Contains(t, notice, "created by [spr]")
	assert.Contains(t, notice, "⚠️")
	assert.Contains(t, notice, "https://github.com/ejoffe/spr")
	assert.Contains(t, notice, "Do not merge manually using the UI")
	// Should be a single-line string (no leading/trailing newlines).
	assert.False(t, strings.HasPrefix(notice, "\n"), "notice should not start with newline")
	assert.False(t, strings.HasSuffix(notice, "\n"), "notice should not end with newline")
}

func TestFormatStackMarkdown_WithTitles(t *testing.T) {
	commit1 := git.Commit{CommitID: "aaa", Subject: "feat: first"}
	commit2 := git.Commit{CommitID: "bbb", Subject: "feat: second"}
	commit3 := git.Commit{CommitID: "ccc", Subject: "feat: third"}

	pr1 := &github.PullRequest{Number: 10, Title: "feat: first", Commit: commit1}
	pr2 := &github.PullRequest{Number: 11, Title: "feat: second", Commit: commit2}
	pr3 := &github.PullRequest{Number: 12, Title: "feat: third", Commit: commit3}
	stack := []*github.PullRequest{pr1, pr2, pr3}

	// showPrTitlesInStack = true → titles appear before the PR number.
	// commit2 is current → gets the ⬅ suffix.
	result := FormatStackMarkdown(commit2, stack, true)

	lines := strings.Split(strings.TrimRight(result, "\n"), "\n")
	assert.Len(t, lines, 3, "three PRs in stack → three lines")

	// Stack is rendered in reverse order (highest PR first).
	assert.Equal(t, "- feat: third #12", lines[0])
	assert.Equal(t, "- feat: second #11 ⬅", lines[1])
	assert.Equal(t, "- feat: first #10", lines[2])
}

func TestFormatStackMarkdown_WithoutTitles(t *testing.T) {
	commit1 := git.Commit{CommitID: "aaa", Subject: "feat: first"}
	commit2 := git.Commit{CommitID: "bbb", Subject: "feat: second"}

	pr1 := &github.PullRequest{Number: 10, Title: "feat: first", Commit: commit1}
	pr2 := &github.PullRequest{Number: 11, Title: "feat: second", Commit: commit2}
	stack := []*github.PullRequest{pr1, pr2}

	// showPrTitlesInStack = false → no titles, only #number.
	// commit1 is current → gets ⬅.
	result := FormatStackMarkdown(commit1, stack, false)

	lines := strings.Split(strings.TrimRight(result, "\n"), "\n")
	assert.Len(t, lines, 2)
	assert.Equal(t, "- #11", lines[0])
	assert.Equal(t, "- #10 ⬅", lines[1])
}

func TestFormatStackMarkdown_CurrentCommitSuffix(t *testing.T) {
	commit := git.Commit{CommitID: "xyz", Subject: "fix: something"}
	pr := &github.PullRequest{Number: 5, Title: "fix: something", Commit: commit}
	stack := []*github.PullRequest{pr}

	result := FormatStackMarkdown(commit, stack, false)
	assert.Equal(t, "- #5 ⬅\n", result)
}

func TestFormatStackMarkdown_NonCurrentCommitNoSuffix(t *testing.T) {
	commitA := git.Commit{CommitID: "aaa", Subject: "fix: a"}
	commitB := git.Commit{CommitID: "bbb", Subject: "fix: b"}

	prA := &github.PullRequest{Number: 1, Title: "fix: a", Commit: commitA}
	prB := &github.PullRequest{Number: 2, Title: "fix: b", Commit: commitB}
	stack := []*github.PullRequest{prA, prB}

	// commitA is current; prB (#2) is not current → no ⬅.
	result := FormatStackMarkdown(commitA, stack, false)
	assert.NotContains(t, result, "#2 ⬅")
	assert.Contains(t, result, "#1 ⬅")
}
