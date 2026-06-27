package template_why_what

import (
	"testing"

	"github.com/ejoffe/spr/git"
	"github.com/ejoffe/spr/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewWhyWhatTemplatizer exercises the constructor, covering the
// NewWhyWhatTemplatizer() call that was previously uncovered (0%).
func TestNewWhyWhatTemplatizer(t *testing.T) {
	tmpl := NewWhyWhatTemplatizer()
	require.NotNil(t, tmpl)

	_, ok := interface{}(tmpl).(*WhyWhatTemplatizer)
	require.True(t, ok, "NewWhyWhatTemplatizer must return a *WhyWhatTemplatizer")

	info := &github.GitHubInfo{}
	commit := git.Commit{
		Subject: "Refactor auth layer",
		Body:    "We needed better separation of concerns.\n\nMoved auth logic to its own package.\n\nRan all unit tests.",
	}

	// Title returns commit.Subject.
	assert.Equal(t, "Refactor auth layer", tmpl.Title(info, commit))

	// Body renders the why/what/test template with the provided sections.
	body := tmpl.Body(info, commit, nil)
	assert.Contains(t, body, "Why\n===")
	assert.Contains(t, body, "We needed better separation of concerns.")
	assert.Contains(t, body, "What changed\n============")
	assert.Contains(t, body, "Moved auth logic to its own package.")
	assert.Contains(t, body, "Test plan\n=========")
	assert.Contains(t, body, "Ran all unit tests.")
	assert.Contains(t, body, "⚠️ *Part of a stack created by [spr]")
}
