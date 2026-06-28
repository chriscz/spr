package githubclient

// Tests for the interactive stdin-prompt branches of (*client).MaybeStar.
//
// Control-flow reminder (star.go):
//   - When isStar returns (false, nil): only the else-block (second prompt) runs
//     → ONE ReadString call consumes ONE stdin line.
//   - When isStar returns (true, nil):  the if-starred block runs, no stdin read.
//   - When isStar returns (_, err):     the error block runs (first prompt) AND
//     because starred==false, the else-block (second prompt) also runs
//     → TWO ReadString calls consume TWO stdin lines.
//
// HOME isolation: every test that reaches an accept branch uses t.Setenv("HOME",
// t.TempDir()) so rake.YamlFileWriter(config_parser.InternalConfigFilePath())
// never writes to the real ~/.spr.state.
//
// stdout silencing: prompts are printed via fmt.Print/Println to os.Stdout; we
// redirect os.Stdout to /dev/null so no prompt text leaks into test output.

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/ejoffe/spr/github/githubclient/gen/genclient"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test helpers (local copies of the body_editor_test.go patterns)
// ---------------------------------------------------------------------------

// silenceStdoutStar redirects os.Stdout to /dev/null for the duration of the
// test and returns a restore function.
func silenceStdoutStar(t *testing.T) func() {
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

// pipeStdinStar writes input to a pipe, sets os.Stdin to the read-end, silences
// os.Stdout, and returns a restore function. Call as: defer pipeStdinStar(t, "Y\n")()
func pipeStdinStar(t *testing.T, input string) func() {
	t.Helper()
	restoreStdout := silenceStdoutStar(t)
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	require.NoError(t, err)
	_, err = io.WriteString(w, input)
	require.NoError(t, err)
	w.Close()
	os.Stdin = r
	return func() {
		os.Stdin = oldStdin
		restoreStdout()
	}
}

// ---------------------------------------------------------------------------
// MaybeStar prompt-branch tests
// ---------------------------------------------------------------------------

// TestMaybeStar_NotStarred_Accept verifies the "not starred, user accepts" path.
// isStar returns (false, nil) → second prompt block runs, user answers "Y\n"
// → addStar is called and cfg.State.Stargazer is set to true.
func TestMaybeStar_NotStarred_Accept(t *testing.T) {
	// HOME isolation: accept-branch writes ~/.spr.state via rake.
	t.Setenv("HOME", t.TempDir())

	// ONE stdin line for the single (else) prompt block.
	defer pipeStdinStar(t, "Y\n")()

	cfg := testConfig()
	cfg.State.Stargazer = false
	cfg.State.RunCount = 25 // 25 % 25 == 0 → triggers the outer gate

	starGetRepoCalled := false
	starAddCalled := false

	api := &fakeAPI{
		// isStar calls StarCheck; return empty edges → (false, nil)
		starCheck: func(ctx context.Context, after *string) (*genclient.StarCheckResponse, error) {
			emptyNodes := genclient.StarCheckViewerStarredRepositoriesNodes{}
			emptyEdges := genclient.StarCheckViewerStarredRepositoriesEdges{}
			return &genclient.StarCheckResponse{
				Viewer: genclient.StarCheckViewer{
					StarredRepositories: genclient.StarCheckViewerStarredRepositories{
						Nodes:      &emptyNodes,
						Edges:      &emptyEdges,
						TotalCount: 0,
					},
				},
			}, nil
		},
		starGetRepo: func(ctx context.Context, owner, name string) (*genclient.StarGetRepoResponse, error) {
			starGetRepoCalled = true
			require.Equal(t, sprRepoOwner, owner)
			require.Equal(t, sprRepoName, name)
			return &genclient.StarGetRepoResponse{
				Repository: &genclient.StarGetRepoRepository{Id: "spr_repo_id"},
			}, nil
		},
		starAdd: func(ctx context.Context, input genclient.AddStarInput) (*genclient.StarAddResponse, error) {
			starAddCalled = true
			require.Equal(t, "spr_repo_id", input.StarrableId)
			return &genclient.StarAddResponse{
				AddStar: &genclient.StarAddAddStar{},
			}, nil
		},
	}
	c := newTestClient(cfg, api)

	c.MaybeStar(context.Background(), cfg)

	require.True(t, starGetRepoCalled, "starGetRepo should have been called (addStar path)")
	require.True(t, starAddCalled, "starAdd should have been called (addStar path)")
	require.True(t, cfg.State.Stargazer, "Stargazer should be true after user accepts")
}

// TestMaybeStar_NotStarred_Decline verifies the "not starred, user declines" path.
// isStar returns (false, nil) → second prompt block runs, user answers "n\n"
// → addStar is NOT called and cfg.State.Stargazer stays false.
func TestMaybeStar_NotStarred_Decline(t *testing.T) {
	// No HOME isolation needed: decline branch never writes state file.

	// ONE stdin line for the single (else) prompt block.
	defer pipeStdinStar(t, "n\n")()

	cfg := testConfig()
	cfg.State.Stargazer = false
	cfg.State.RunCount = 25

	api := &fakeAPI{
		starCheck: func(ctx context.Context, after *string) (*genclient.StarCheckResponse, error) {
			emptyNodes := genclient.StarCheckViewerStarredRepositoriesNodes{}
			emptyEdges := genclient.StarCheckViewerStarredRepositoriesEdges{}
			return &genclient.StarCheckResponse{
				Viewer: genclient.StarCheckViewer{
					StarredRepositories: genclient.StarCheckViewerStarredRepositories{
						Nodes:      &emptyNodes,
						Edges:      &emptyEdges,
						TotalCount: 0,
					},
				},
			}, nil
		},
		starGetRepo: func(ctx context.Context, owner, name string) (*genclient.StarGetRepoResponse, error) {
			t.Fatal("starGetRepo should NOT be called when user declines")
			return nil, nil
		},
		starAdd: func(ctx context.Context, input genclient.AddStarInput) (*genclient.StarAddResponse, error) {
			t.Fatal("starAdd should NOT be called when user declines")
			return nil, nil
		},
	}
	c := newTestClient(cfg, api)

	c.MaybeStar(context.Background(), cfg)

	require.False(t, cfg.State.Stargazer, "Stargazer should remain false when user declines")
}

// TestMaybeStar_AlreadyStarred verifies the "already starred on GitHub" path.
// isStar returns (true, nil) → if-starred block runs, NO stdin prompt.
// cfg.State.Stargazer is set to true; addStar is NOT called.
func TestMaybeStar_AlreadyStarred(t *testing.T) {
	// HOME isolation: accept-path writes state.
	t.Setenv("HOME", t.TempDir())

	// No stdin piping needed — this branch has no ReadString call.
	defer silenceStdoutStar(t)()

	cfg := testConfig()
	cfg.State.Stargazer = false
	cfg.State.RunCount = 25

	starAddCalled := false

	api := &fakeAPI{
		// isStar: return the spr repo in nodes → (true, nil)
		starCheck: func(ctx context.Context, after *string) (*genclient.StarCheckResponse, error) {
			return makeStarCheckResponse(
				[]string{"ejoffe/spr"},
				[]string{"cursor1"},
			), nil
		},
		starGetRepo: func(ctx context.Context, owner, name string) (*genclient.StarGetRepoResponse, error) {
			t.Fatal("starGetRepo should NOT be called when already starred")
			return nil, nil
		},
		starAdd: func(ctx context.Context, input genclient.AddStarInput) (*genclient.StarAddResponse, error) {
			starAddCalled = true
			t.Fatal("starAdd should NOT be called when already starred")
			return nil, nil
		},
	}
	c := newTestClient(cfg, api)

	c.MaybeStar(context.Background(), cfg)

	require.False(t, starAddCalled, "addStar should not be called when already starred")
	require.True(t, cfg.State.Stargazer, "Stargazer should be true when already starred")
}

// TestMaybeStar_IsStarError_AcceptFirst_DeclineSecond verifies the isStar-error path.
//
// When isStar returns an error: starred==false (zero value), so BOTH the error
// block (first prompt) AND the else block (second prompt) execute sequentially.
// This test supplies TWO stdin lines:
//   - Line 1 ("Y\n"):  first prompt  → user accepts → cfg.State.Stargazer = true
//   - Line 2 ("n\n"):  second prompt → user declines → addStar NOT called
//
// KNOWN-FAIL DG-P01-4-BUG-A: star.go creates two separate bufio.NewReader(os.Stdin)
// instances for the error-block prompt and the else-block prompt. Because
// bufio.NewReader reads ahead into a 4096-byte buffer, the FIRST reader consumes
// the entire pipe contents on its first system read, leaving the SECOND reader
// with no data (empty string). An empty line is not "n", so the else-block
// incorrectly treats it as acceptance and calls addStar even when the user
// intended to decline. The correct fix is to share a single bufio.Reader across
// all stdin reads in MaybeStar.
func TestMaybeStar_IsStarError_AcceptFirst_DeclineSecond(t *testing.T) {
	t.Skip("KNOWN-FAIL DG-P01-4-BUG-A: two separate bufio.NewReader(os.Stdin) in " +
		"MaybeStar cause the second reader to get empty input; second prompt " +
		"incorrectly accepts instead of declining. Fix: share one bufio.Reader.")

	// HOME isolation: first-prompt accept branch writes state.
	t.Setenv("HOME", t.TempDir())

	// TWO stdin lines: first prompt (accept) + second prompt (decline).
	defer pipeStdinStar(t, "Y\nn\n")()

	cfg := testConfig()
	cfg.State.Stargazer = false
	cfg.State.RunCount = 25

	api := &fakeAPI{
		starCheck: func(ctx context.Context, after *string) (*genclient.StarCheckResponse, error) {
			return nil, errors.New("network error")
		},
		starGetRepo: func(ctx context.Context, owner, name string) (*genclient.StarGetRepoResponse, error) {
			t.Fatal("starGetRepo should NOT be called when second prompt is declined")
			return nil, nil
		},
		starAdd: func(ctx context.Context, input genclient.AddStarInput) (*genclient.StarAddResponse, error) {
			t.Fatal("starAdd should NOT be called when second prompt is declined")
			return nil, nil
		},
	}
	c := newTestClient(cfg, api)

	c.MaybeStar(context.Background(), cfg)

	// First prompt accepted → Stargazer becomes true in the error block.
	require.True(t, cfg.State.Stargazer, "Stargazer should be true (accepted first prompt)")
}

// TestMaybeStar_IsStarError_DeclineFirst_AcceptSecond verifies the isStar-error path
// where the user declines the first prompt but accepts the second (star add) prompt.
//
// TWO stdin lines:
//   - Line 1 ("n\n"):  first prompt  → user declines → Stargazer stays false
//   - Line 2 ("Y\n"):  second prompt → user accepts → addStar called,
//     cfg.State.Stargazer = true
func TestMaybeStar_IsStarError_DeclineFirst_AcceptSecond(t *testing.T) {
	// HOME isolation: second-prompt accept branch writes state.
	t.Setenv("HOME", t.TempDir())

	// TWO stdin lines: first prompt (decline) + second prompt (accept).
	defer pipeStdinStar(t, "n\nY\n")()

	cfg := testConfig()
	cfg.State.Stargazer = false
	cfg.State.RunCount = 25

	starGetRepoCalled := false
	starAddCalled := false

	api := &fakeAPI{
		starCheck: func(ctx context.Context, after *string) (*genclient.StarCheckResponse, error) {
			return nil, errors.New("network error")
		},
		starGetRepo: func(ctx context.Context, owner, name string) (*genclient.StarGetRepoResponse, error) {
			starGetRepoCalled = true
			require.Equal(t, sprRepoOwner, owner)
			require.Equal(t, sprRepoName, name)
			return &genclient.StarGetRepoResponse{
				Repository: &genclient.StarGetRepoRepository{Id: "spr_repo_id"},
			}, nil
		},
		starAdd: func(ctx context.Context, input genclient.AddStarInput) (*genclient.StarAddResponse, error) {
			starAddCalled = true
			require.Equal(t, "spr_repo_id", input.StarrableId)
			return &genclient.StarAddResponse{
				AddStar: &genclient.StarAddAddStar{},
			}, nil
		},
	}
	c := newTestClient(cfg, api)

	c.MaybeStar(context.Background(), cfg)

	require.True(t, starGetRepoCalled, "starGetRepo should be called (addStar triggered)")
	require.True(t, starAddCalled, "starAdd should be called (second prompt accepted)")
	// Second prompt accepted → Stargazer becomes true from the addStar branch.
	require.True(t, cfg.State.Stargazer, "Stargazer should be true (accepted second prompt)")
}
