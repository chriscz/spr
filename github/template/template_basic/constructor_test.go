package template_basic

import (
	"strings"
	"testing"

	"github.com/ejoffe/spr/git"
	"github.com/ejoffe/spr/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewBasicTemplatizer exercises the constructor path and verifies that the
// returned value behaves correctly as a full *BasicTemplatizer — covering the
// NewBasicTemplatizer() function that was previously uncovered.
func TestNewBasicTemplatizer(t *testing.T) {
	tmpl := NewBasicTemplatizer()
	require.NotNil(t, tmpl)
	// Confirm concrete type.
	_, ok := interface{}(tmpl).(*BasicTemplatizer)
	require.True(t, ok, "NewBasicTemplatizer must return a *BasicTemplatizer")

	info := &github.GitHubInfo{}
	commit := git.Commit{
		Subject: "Add login endpoint",
		Body:    "Implements JWT-based login for the API.",
	}

	// Title delegates to commit.Subject.
	assert.Equal(t, "Add login endpoint", tmpl.Title(info, commit))

	// Body = commit.Body + "\n\n" + ManualMergeNotice().
	body := tmpl.Body(info, commit, nil)
	assert.True(t, strings.HasPrefix(body, commit.Body),
		"body should start with commit.Body")
	assert.Contains(t, body, "\n\n")
	assert.True(t, strings.HasSuffix(body, "⚠️ *Part of a stack created by [spr](https://github.com/ejoffe/spr). Do not merge manually using the UI - doing so may have unexpected results.*"),
		"body should end with ManualMergeNotice()")
}
