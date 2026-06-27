package config_fetcher

import (
	"context"
	"testing"

	"github.com/ejoffe/spr/config"
	"github.com/ejoffe/spr/git"
	"github.com/ejoffe/spr/github/template/template_basic"
	"github.com/ejoffe/spr/github/template/template_custom"
	"github.com/ejoffe/spr/github/template/template_stack"
	"github.com/ejoffe/spr/github/template/template_why_what"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubGit is the minimal git.GitInterface needed by config_fetcher (only
// RootDir is called when constructing a CustomTemplatizer).
type stubGit struct{ rootDir string }

func (s *stubGit) Git(args string, output *string) error                             { return nil }
func (s *stubGit) MustGit(args string, output *string)                               {}
func (s *stubGit) GitWithEditor(args string, output *string, editorCmd string) error { return nil }
func (s *stubGit) RootDir() string                                                   { return s.rootDir }
func (s *stubGit) DeleteRemoteBranch(ctx context.Context, branch string) error       { return nil }

var _ git.GitInterface = (*stubGit)(nil)

func newConfig(prTemplateType string) *config.Config {
	return &config.Config{
		Repo: &config.RepoConfig{
			PRTemplateType: prTemplateType,
		},
		User:  &config.UserConfig{},
		State: &config.InternalState{},
	}
}

func TestPRTemplatizer_Stack(t *testing.T) {
	c := newConfig("stack")
	tmpl := PRTemplatizer(c, nil)
	require.NotNil(t, tmpl)
	_, ok := tmpl.(*template_stack.StackTemplatizer)
	assert.True(t, ok, "expected *template_stack.StackTemplatizer for type 'stack'")
}

func TestPRTemplatizer_Basic(t *testing.T) {
	c := newConfig("basic")
	tmpl := PRTemplatizer(c, nil)
	require.NotNil(t, tmpl)
	_, ok := tmpl.(*template_basic.BasicTemplatizer)
	assert.True(t, ok, "expected *template_basic.BasicTemplatizer for type 'basic'")
}

func TestPRTemplatizer_WhyWhat(t *testing.T) {
	c := newConfig("why_what")
	tmpl := PRTemplatizer(c, nil)
	require.NotNil(t, tmpl)
	_, ok := tmpl.(*template_why_what.WhyWhatTemplatizer)
	assert.True(t, ok, "expected *template_why_what.WhyWhatTemplatizer for type 'why_what'")
}

func TestPRTemplatizer_Custom(t *testing.T) {
	c := newConfig("custom")
	stub := &stubGit{rootDir: "/tmp"}
	tmpl := PRTemplatizer(c, stub)
	require.NotNil(t, tmpl)
	_, ok := tmpl.(*template_custom.CustomTemplatizer)
	assert.True(t, ok, "expected *template_custom.CustomTemplatizer for type 'custom'")
}

func TestPRTemplatizer_Default(t *testing.T) {
	c := newConfig("unknown_type")
	tmpl := PRTemplatizer(c, nil)
	require.NotNil(t, tmpl)
	_, ok := tmpl.(*template_basic.BasicTemplatizer)
	assert.True(t, ok, "expected *template_basic.BasicTemplatizer for unknown type (default)")
}

func TestPRTemplatizer_EmptyType(t *testing.T) {
	c := newConfig("")
	tmpl := PRTemplatizer(c, nil)
	require.NotNil(t, tmpl)
	_, ok := tmpl.(*template_basic.BasicTemplatizer)
	assert.True(t, ok, "expected *template_basic.BasicTemplatizer for empty type (default)")
}
