package github

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ejoffe/spr/config"
	"github.com/ejoffe/spr/git"
	"github.com/stretchr/testify/assert"
)

// TestGitHubInfoKey covers interface.go:49 Key() (was 0%).
func TestGitHubInfoKey(t *testing.T) {
	info := &GitHubInfo{RepositoryID: "R", LocalBranch: "b"}
	assert.Equal(t, "R_b", info.Key())

	info2 := &GitHubInfo{RepositoryID: "owner/repo", LocalBranch: "feature/my-branch"}
	assert.Equal(t, "owner/repo_feature/my-branch", info2.Key())
}

// TestReadyRequireChecksFailBranch covers the RequireChecks early-return in Ready()
// that existing tests missed (they reached NoConflicts=false first).
func TestReadyRequireChecksFailBranch(t *testing.T) {
	cfg := &config.Config{
		Repo: &config.RepoConfig{
			RequireChecks:   true,
			RequireApproval: false,
		},
	}

	// WIP=false, NoConflicts=true, RequireChecks=true, ChecksPass=Fail → false
	pr := &PullRequest{
		Commit: git.Commit{WIP: false},
		MergeStatus: PullRequestMergeStatus{
			NoConflicts:    true,
			ChecksPass:     CheckStatusFail,
			ReviewApproved: false,
			Stacked:        true,
		},
	}
	assert.False(t, pr.Ready(cfg), "RequireChecks=true with ChecksPass=Fail should return false")

	// Same but Pending
	pr2 := &PullRequest{
		Commit: git.Commit{WIP: false},
		MergeStatus: PullRequestMergeStatus{
			NoConflicts:    true,
			ChecksPass:     CheckStatusPending,
			ReviewApproved: false,
			Stacked:        true,
		},
	}
	assert.False(t, pr2.Ready(cfg), "RequireChecks=true with ChecksPass=Pending should return false")

	// All good: RequireChecks=true, Pass → true
	pr3 := &PullRequest{
		Commit: git.Commit{WIP: false},
		MergeStatus: PullRequestMergeStatus{
			NoConflicts:    true,
			ChecksPass:     CheckStatusPass,
			ReviewApproved: false,
			Stacked:        true,
		},
	}
	assert.True(t, pr3.Ready(cfg), "RequireChecks=true with ChecksPass=Pass should return true")
}

// TestStatusBitIconsEmoji covers statusBitIcons() emoji branch (was 66.7%).
func TestStatusBitIconsEmoji(t *testing.T) {
	emojiCfg := &config.Config{
		User: &config.UserConfig{StatusBitsEmojis: true},
	}
	icons := statusBitIcons(emojiCfg)
	assert.Equal(t, "✅", icons["checkmark"])
	assert.Equal(t, "❌", icons["crossmark"])
	assert.Equal(t, "⌛", icons["pending"])
	assert.Equal(t, "❓", icons["questionmark"])
	assert.Equal(t, "➖", icons["empty"])
	assert.Equal(t, "⚠️", icons["warning"])

	asciiCfg := &config.Config{
		User: &config.UserConfig{StatusBitsEmojis: false},
	}
	asciiIcons := statusBitIcons(asciiCfg)
	assert.Equal(t, "v", asciiIcons["checkmark"])
	assert.Equal(t, "x", asciiIcons["crossmark"])
	assert.Equal(t, ".", asciiIcons["pending"])
	assert.Equal(t, "?", asciiIcons["questionmark"])
	assert.Equal(t, "-", asciiIcons["empty"])
	assert.Equal(t, "!", asciiIcons["warning"])
}

// TestCheckStatusStringDefault covers the default case in CheckStatus.String().
func TestCheckStatusStringDefault(t *testing.T) {
	cfg := &config.Config{
		Repo: &config.RepoConfig{RequireChecks: true},
		User: &config.UserConfig{StatusBitsEmojis: false},
	}
	// CheckStatus value that is not one of the defined constants — hits default branch
	cs := CheckStatus(99)
	assert.Equal(t, "?", cs.String(cfg))
}

// TestStringMerged covers the Merged=true branch in PullRequest.String() (was missed).
func TestStringMerged(t *testing.T) {
	cfg := &config.Config{
		Repo: &config.RepoConfig{
			RequireChecks:   true,
			RequireApproval: true,
		},
		User: &config.UserConfig{
			StatusBitsEmojis: false,
		},
	}
	pr := &PullRequest{
		Number: 5,
		Title:  "Merged commit",
		Merged: true,
		MergeStatus: PullRequestMergeStatus{
			ChecksPass:     CheckStatusPass,
			ReviewApproved: true,
			NoConflicts:    true,
			Stacked:        true,
		},
	}
	result := pr.String(cfg)
	assert.Equal(t, "MERGED   5 : Merged commit", result)
}

// TestStringTerminalWidthTrim covers the terminal-width trim branch in String().
// terminal.Width() fails in tests (stdin is not a tty) so terminalWidth defaults to 1000.
// A title with >1000 runes causes lineLength > terminalWidth, triggering the trim.
func TestStringTerminalWidthTrim(t *testing.T) {
	cfg := &config.Config{
		Repo: &config.RepoConfig{
			RequireChecks:   false,
			RequireApproval: false,
		},
		User: &config.UserConfig{
			StatusBitsEmojis: false,
		},
	}

	// "[-xx] " (6) + "  0 : " (6) = 12 overhead, so we need >988 rune title
	longTitle := strings.Repeat("a", 1100)
	pr := &PullRequest{
		Number: 0,
		Title:  longTitle,
		MergeStatus: PullRequestMergeStatus{
			NoConflicts: false,
			Stacked:     false,
		},
	}
	result := pr.String(cfg)
	// Should be trimmed to terminalWidth-3 runes + "..."
	assert.True(t, strings.HasSuffix(result, "..."), "long title should be trimmed with ..., got len=%d", len(result))
	// terminalWidth defaults to 1000 (no TTY in tests); the trim slices to
	// terminalWidth-3 then appends "..." → exactly terminalWidth runes.
	// Title is ASCII so byte length == rune length; asserting the exact width
	// catches any off-by-one in the trim formula.
	assert.Equal(t, 1000, utf8.RuneCountInString(result))
}

// TestStringEmojiLineLength covers the StatusBitsEmojis=true lineLength correction branch.
func TestStringEmojiLineLength(t *testing.T) {
	cfg := &config.Config{
		Repo: &config.RepoConfig{
			RequireChecks:   true,
			RequireApproval: true,
		},
		User: &config.UserConfig{
			StatusBitsEmojis: true,
		},
	}
	pr := &PullRequest{
		Number: 1,
		Title:  "emoji test",
		MergeStatus: PullRequestMergeStatus{
			ChecksPass:     CheckStatusPass,
			ReviewApproved: true,
			NoConflicts:    true,
			Stacked:        true,
		},
	}
	// Verify it runs and produces a result with emoji status bits
	result := pr.String(cfg)
	assert.True(t, strings.HasPrefix(result, "[✅"), "emoji cfg should produce emoji status bits, got: %q", result)
	assert.Contains(t, result, "emoji test")
}
