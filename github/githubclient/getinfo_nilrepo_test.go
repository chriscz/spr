package githubclient

// Regression tests for the GetInfo nil-pointer crash cluster.
//
// When GitHub returns a null `Repository` node (auth/token not resolved, repo
// not found, owner/host mismatch, GHE quirks), GetInfo used to do
// `repoID = resp.Repository.Id` with no nil guard and panic with SIGSEGV
// instead of emitting a clean, actionable error. This happens on BOTH the
// normal PullRequests path (client.go:213) and the merge-queue
// PullRequestsWithMergeQueue path (client.go:205).
//
// These tests drive a faithful `Repository: nil` response (the real fezzik
// types make Repository a pointer, so null is exactly representable) through
// GetInfo and assert it exits cleanly via the shared check()/osExit path
// rather than dereferencing nil.

import (
	"context"
	"io"
	"os"
	"testing"

	"github.com/ejoffe/spr/github/githubclient/fezzik_types"
	"github.com/ejoffe/spr/github/githubclient/gen/genclient"
	"github.com/stretchr/testify/require"
)

// nilRepoPRResponse mirrors a real GitHub response where the viewer/login is
// present but the Repository node resolved to null.
func nilRepoPRResponse(loginName string) *genclient.PullRequestsResponse {
	nodes := fezzik_types.PullRequestsViewerPullRequestsNodes{}
	return &genclient.PullRequestsResponse{
		Viewer: genclient.PullRequestsViewer{
			Login: loginName,
			PullRequests: fezzik_types.PullRequestConnection{
				Nodes: &nodes,
			},
		},
		Repository: nil,
	}
}

func nilRepoPRWithMQResponse(loginName string) *genclient.PullRequestsWithMergeQueueResponse {
	nodes := fezzik_types.PullRequestsViewerPullRequestsNodes{}
	return &genclient.PullRequestsWithMergeQueueResponse{
		Viewer: genclient.PullRequestsWithMergeQueueViewer{
			Login: loginName,
			PullRequests: fezzik_types.PullRequestConnection{
				Nodes: &nodes,
			},
		},
		Repository: nil,
	}
}

// withCapturedExit replaces the package-level osExit seam with a function that
// records the exit code and panics a sentinel so the caller's stack is unwound
// (exactly as os.Exit would terminate it) without killing the test process.
type exitCalled struct{ code int }

func withCapturedExit(t *testing.T, fn func()) (called bool, code int) {
	t.Helper()
	orig := osExit
	osExit = func(c int) {
		code = c
		called = true
		panic(exitCalled{c})
	}

	// Silence the clean-exit stderr message so test output stays pristine.
	origStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	done := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, r); close(done) }()

	restore := func() {
		osExit = orig
		_ = w.Close()
		os.Stderr = origStderr
		<-done
		_ = r.Close()
	}

	defer func() {
		restore()
		if rec := recover(); rec != nil {
			if _, ok := rec.(exitCalled); !ok {
				panic(rec) // a real (nil-pointer) panic: re-raise so the test fails
			}
		}
	}()
	fn()
	return called, code
}

func TestGetInfo_NilRepository_ExitsCleanly(t *testing.T) {
	cfg := testConfig()
	api := &fakeAPI{
		pullRequests: func(ctx context.Context, repoOwner, repoName string) (*genclient.PullRequestsResponse, error) {
			return nilRepoPRResponse("testuser"), nil
		},
	}
	c := newTestClient(cfg, api)
	// GetInfo reads the local stack/branch before the (post-fix) guard would
	// fire on the older code path order; provide the mock git expectations so
	// the only thing left to crash on is the nil Repository deref.
	gitcmd := setupMockGitForGetInfo(t, nil)

	called, code := withCapturedExit(t, func() {
		c.GetInfo(context.Background(), gitcmd)
	})

	require.True(t, called, "GetInfo must exit cleanly (not nil-pointer panic) when Repository is null")
	require.NotEqual(t, 0, code, "clean exit on unresolved repo should use a non-zero status")
}

func TestGetInfo_MergeQueue_NilRepository_ExitsCleanly(t *testing.T) {
	cfg := testConfig()
	cfg.Repo.MergeQueue = true
	api := &fakeAPI{
		pullRequestsWithMergeQueue: func(ctx context.Context, repoOwner, repoName string) (*genclient.PullRequestsWithMergeQueueResponse, error) {
			return nilRepoPRWithMQResponse("mquser"), nil
		},
	}
	c := newTestClient(cfg, api)
	gitcmd := setupMockGitForGetInfo(t, nil)

	called, code := withCapturedExit(t, func() {
		c.GetInfo(context.Background(), gitcmd)
	})

	require.True(t, called, "GetInfo (merge-queue path) must exit cleanly when Repository is null")
	require.NotEqual(t, 0, code, "clean exit on unresolved repo should use a non-zero status")
}

// TestCheck_RepoNotResolved_ExitsCleanly pins the shared error-routing
// behavior: a repo-not-resolved error must take check()'s clean-exit branch
// (stderr message + osExit) and NOT the generic panic branch, so the user
// never sees a Go stack trace.
func TestCheck_RepoNotResolved_ExitsCleanly(t *testing.T) {
	called, code := withCapturedExit(t, func() {
		check(errRepoNotResolved("testowner", "testrepo"))
	})
	require.True(t, called, "repo-not-resolved error must route through clean osExit, not panic")
	require.NotEqual(t, 0, code)
}
