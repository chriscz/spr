package githubclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/ejoffe/spr/config"
	"github.com/ejoffe/spr/git"
	"github.com/ejoffe/spr/git/mockgit"
	"github.com/ejoffe/spr/github"
	"github.com/ejoffe/spr/github/githubclient/fezzik_types"
	"github.com/ejoffe/spr/github/githubclient/gen/genclient"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func testConfig() *config.Config {
	cfg := config.DefaultConfig()
	cfg.Repo.GitHubRepoOwner = "testowner"
	cfg.Repo.GitHubRepoName = "testrepo"
	cfg.Repo.GitHubBranch = "master"
	cfg.Repo.GitHubRemote = "origin"
	cfg.User.BranchPrefix = "spr"
	return cfg
}

// newTestClient builds an internal client with no-op http client and test API.
func newTestClient(cfg *config.Config, api genclient.Client) *client {
	return &client{
		config:          cfg,
		api:             api,
		graphqlEndpoint: "http://fake-endpoint/graphql",
		httpClient:      &http.Client{},
	}
}

// ---------------------------------------------------------------------------
// In-memory HTTP transport (avoids real network, safe in sandboxed environments)
// ---------------------------------------------------------------------------

// inMemoryTransport intercepts HTTP calls and routes them through an in-process handler.
type inMemoryTransport struct {
	handler http.Handler
}

type inMemoryResponseWriter struct {
	code   int
	body   *bytes.Buffer
	header http.Header
}

func (r *inMemoryResponseWriter) Header() http.Header         { return r.header }
func (r *inMemoryResponseWriter) Write(b []byte) (int, error) { return r.body.Write(b) }
func (r *inMemoryResponseWriter) WriteHeader(code int)        { r.code = code }

func (t *inMemoryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	w := &inMemoryResponseWriter{
		code:   200,
		body:   &bytes.Buffer{},
		header: make(http.Header),
	}
	t.handler.ServeHTTP(w, req)
	return &http.Response{
		StatusCode: w.code,
		Status:     fmt.Sprintf("%d OK", w.code),
		Header:     w.header,
		Body:       io.NopCloser(bytes.NewReader(w.body.Bytes())),
	}, nil
}

// newInMemoryClient returns an http.Client that routes all requests through handler.
func newInMemoryClient(handler http.Handler) *http.Client {
	return &http.Client{Transport: &inMemoryTransport{handler: handler}}
}

// ---------------------------------------------------------------------------
// Fake genclient.Client implementation
// ---------------------------------------------------------------------------

type fakeAPI struct {
	pullRequests               func(ctx context.Context, repoOwner, repoName string) (*genclient.PullRequestsResponse, error)
	pullRequestsWithMergeQueue func(ctx context.Context, repoOwner, repoName string) (*genclient.PullRequestsWithMergeQueueResponse, error)
	assignableUsers            func(ctx context.Context, repoOwner, repoName string, endCursor *string) (*genclient.AssignableUsersResponse, error)
	createPullRequest          func(ctx context.Context, input genclient.CreatePullRequestInput) (*genclient.CreatePullRequestResponse, error)
	updatePullRequest          func(ctx context.Context, input genclient.UpdatePullRequestInput) (*genclient.UpdatePullRequestResponse, error)
	addReviewers               func(ctx context.Context, input genclient.RequestReviewsInput) (*genclient.AddReviewersResponse, error)
	commentPullRequest         func(ctx context.Context, input genclient.AddCommentInput) (*genclient.CommentPullRequestResponse, error)
	mergePullRequest           func(ctx context.Context, input genclient.MergePullRequestInput) (*genclient.MergePullRequestResponse, error)
	autoMergePullRequest       func(ctx context.Context, input genclient.EnablePullRequestAutoMergeInput) (*genclient.AutoMergePullRequestResponse, error)
	closePullRequest           func(ctx context.Context, input genclient.ClosePullRequestInput) (*genclient.ClosePullRequestResponse, error)
	starCheck                  func(ctx context.Context, after *string) (*genclient.StarCheckResponse, error)
	starGetRepo                func(ctx context.Context, owner, name string) (*genclient.StarGetRepoResponse, error)
	starAdd                    func(ctx context.Context, input genclient.AddStarInput) (*genclient.StarAddResponse, error)
}

func (f *fakeAPI) PullRequests(ctx context.Context, repoOwner, repoName string) (*genclient.PullRequestsResponse, error) {
	return f.pullRequests(ctx, repoOwner, repoName)
}

func (f *fakeAPI) PullRequestsWithMergeQueue(ctx context.Context, repoOwner, repoName string) (*genclient.PullRequestsWithMergeQueueResponse, error) {
	return f.pullRequestsWithMergeQueue(ctx, repoOwner, repoName)
}

func (f *fakeAPI) AssignableUsers(ctx context.Context, repoOwner, repoName string, endCursor *string) (*genclient.AssignableUsersResponse, error) {
	return f.assignableUsers(ctx, repoOwner, repoName, endCursor)
}

func (f *fakeAPI) CreatePullRequest(ctx context.Context, input genclient.CreatePullRequestInput) (*genclient.CreatePullRequestResponse, error) {
	return f.createPullRequest(ctx, input)
}

func (f *fakeAPI) UpdatePullRequest(ctx context.Context, input genclient.UpdatePullRequestInput) (*genclient.UpdatePullRequestResponse, error) {
	return f.updatePullRequest(ctx, input)
}

func (f *fakeAPI) AddReviewers(ctx context.Context, input genclient.RequestReviewsInput) (*genclient.AddReviewersResponse, error) {
	return f.addReviewers(ctx, input)
}

func (f *fakeAPI) CommentPullRequest(ctx context.Context, input genclient.AddCommentInput) (*genclient.CommentPullRequestResponse, error) {
	return f.commentPullRequest(ctx, input)
}

func (f *fakeAPI) MergePullRequest(ctx context.Context, input genclient.MergePullRequestInput) (*genclient.MergePullRequestResponse, error) {
	return f.mergePullRequest(ctx, input)
}

func (f *fakeAPI) AutoMergePullRequest(ctx context.Context, input genclient.EnablePullRequestAutoMergeInput) (*genclient.AutoMergePullRequestResponse, error) {
	return f.autoMergePullRequest(ctx, input)
}

func (f *fakeAPI) ClosePullRequest(ctx context.Context, input genclient.ClosePullRequestInput) (*genclient.ClosePullRequestResponse, error) {
	return f.closePullRequest(ctx, input)
}

func (f *fakeAPI) StarCheck(ctx context.Context, after *string) (*genclient.StarCheckResponse, error) {
	return f.starCheck(ctx, after)
}

func (f *fakeAPI) StarGetRepo(ctx context.Context, owner, name string) (*genclient.StarGetRepoResponse, error) {
	return f.starGetRepo(ctx, owner, name)
}

func (f *fakeAPI) StarAdd(ctx context.Context, input genclient.AddStarInput) (*genclient.StarAddResponse, error) {
	return f.starAdd(ctx, input)
}

// ---------------------------------------------------------------------------
// Helper: build minimal PullRequestsResponse with no PRs
// ---------------------------------------------------------------------------

func emptyPRResponse(loginName, repoID string) *genclient.PullRequestsResponse {
	nodes := fezzik_types.PullRequestsViewerPullRequestsNodes{}
	return &genclient.PullRequestsResponse{
		Viewer: genclient.PullRequestsViewer{
			Login: loginName,
			PullRequests: fezzik_types.PullRequestConnection{
				Nodes: &nodes,
			},
		},
		Repository: &genclient.PullRequestsRepository{Id: repoID},
	}
}

func emptyPRWithMQResponse(loginName, repoID string) *genclient.PullRequestsWithMergeQueueResponse {
	nodes := fezzik_types.PullRequestsViewerPullRequestsNodes{}
	return &genclient.PullRequestsWithMergeQueueResponse{
		Viewer: genclient.PullRequestsWithMergeQueueViewer{
			Login: loginName,
			PullRequests: fezzik_types.PullRequestConnection{
				Nodes: &nodes,
			},
		},
		Repository: &genclient.PullRequestsWithMergeQueueRepository{Id: repoID},
	}
}

// setupMockGitForGetInfo sets up the mock git with the expected calls that
// GetInfo makes (log + branch).
func setupMockGitForGetInfo(t *testing.T, commits []*git.Commit) *mockgit.Mock {
	t.Helper()
	m := mockgit.NewMockGit(t)
	m.ExpectLogAndRespond(commits)
	m.ExpectLocalBranch("* master\n")
	return m
}

// buildGQLJSONResponse builds a JSON graphql response body for fetchRequiredChecksStatus.
func buildGQLJSONResponse(t *testing.T, prNumber int, checkName string, status string, conclusion string) []byte {
	t.Helper()
	gqlRespData := map[string]interface{}{
		fmt.Sprintf("pr_%d", prNumber): map[string]interface{}{
			"number": prNumber,
			"commits": map[string]interface{}{
				"nodes": []interface{}{
					map[string]interface{}{
						"commit": map[string]interface{}{
							"statusCheckRollup": map[string]interface{}{
								"contexts": map[string]interface{}{
									"nodes": []interface{}{
										map[string]interface{}{
											"__typename": "CheckRun",
											"name":       checkName,
											"status":     status,
											"conclusion": conclusion,
											"context":    "",
											"state":      "",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	b, err := json.Marshal(map[string]interface{}{"data": gqlRespData})
	require.NoError(t, err)
	return b
}

// ---------------------------------------------------------------------------
// GetInfo tests
// ---------------------------------------------------------------------------

func TestGetInfo_NoPRs(t *testing.T) {
	cfg := testConfig()
	api := &fakeAPI{
		pullRequests: func(ctx context.Context, repoOwner, repoName string) (*genclient.PullRequestsResponse, error) {
			require.Equal(t, "testowner", repoOwner)
			require.Equal(t, "testrepo", repoName)
			return emptyPRResponse("testuser", "repo123"), nil
		},
	}
	c := newTestClient(cfg, api)
	gitcmd := setupMockGitForGetInfo(t, nil)

	info := c.GetInfo(context.Background(), gitcmd)

	require.NotNil(t, info)
	require.Equal(t, "testuser", info.UserName)
	require.Equal(t, "repo123", info.RepositoryID)
	require.Equal(t, "master", info.LocalBranch)
	require.Empty(t, info.PullRequests)
	gitcmd.ExpectationsMet()
}

func TestGetInfo_MergeQueue(t *testing.T) {
	cfg := testConfig()
	cfg.Repo.MergeQueue = true

	api := &fakeAPI{
		pullRequestsWithMergeQueue: func(ctx context.Context, repoOwner, repoName string) (*genclient.PullRequestsWithMergeQueueResponse, error) {
			require.Equal(t, "testowner", repoOwner)
			require.Equal(t, "testrepo", repoName)
			return emptyPRWithMQResponse("mquser", "repo456"), nil
		},
	}
	c := newTestClient(cfg, api)
	gitcmd := setupMockGitForGetInfo(t, nil)

	info := c.GetInfo(context.Background(), gitcmd)

	require.NotNil(t, info)
	require.Equal(t, "mquser", info.UserName)
	require.Equal(t, "repo456", info.RepositoryID)
	require.Equal(t, "master", info.LocalBranch)
	require.Empty(t, info.PullRequests)
	gitcmd.ExpectationsMet()
}

func TestGetInfo_WithPR(t *testing.T) {
	cfg := testConfig()

	commitID := "abcd1234"
	commitHash := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	headRefName := fmt.Sprintf("spr/master/%s", commitID)

	commits := []*git.Commit{
		{CommitID: commitID, CommitHash: commitHash, Subject: "test commit"},
	}

	prNodes := fezzik_types.PullRequestsViewerPullRequestsNodes{
		{
			Id:          "pr_id_1",
			Number:      42,
			Title:       "test commit",
			Body:        "pr body",
			HeadRefName: headRefName,
			BaseRefName: "master",
			Mergeable:   fezzik_types.MergeableState_MERGEABLE,
			Commits: fezzik_types.PullRequestsViewerPullRequestsNodesCommits{
				Nodes: &fezzik_types.PullRequestsViewerPullRequestsNodesCommitsNodes{
					{
						Commit: fezzik_types.PullRequestsViewerPullRequestsNodesCommitsNodesCommit{
							Oid:             commitHash,
							MessageHeadline: "test commit",
							MessageBody:     "",
						},
					},
				},
			},
		},
	}

	api := &fakeAPI{
		pullRequests: func(ctx context.Context, repoOwner, repoName string) (*genclient.PullRequestsResponse, error) {
			return &genclient.PullRequestsResponse{
				Viewer: genclient.PullRequestsViewer{
					Login: "testuser",
					PullRequests: fezzik_types.PullRequestConnection{
						Nodes: &prNodes,
					},
				},
				Repository: &genclient.PullRequestsRepository{Id: "repo789"},
			}, nil
		},
	}
	c := newTestClient(cfg, api)
	gitcmd := setupMockGitForGetInfo(t, commits)

	info := c.GetInfo(context.Background(), gitcmd)

	require.NotNil(t, info)
	require.Equal(t, "testuser", info.UserName)
	require.Len(t, info.PullRequests, 1)
	require.Equal(t, 42, info.PullRequests[0].Number)
	require.Equal(t, "pr_id_1", info.PullRequests[0].ID)
	require.Equal(t, headRefName, info.PullRequests[0].FromBranch)
	require.Equal(t, "master", info.PullRequests[0].ToBranch)
	gitcmd.ExpectationsMet()
}

func TestGetInfo_RequiredChecks_FetchesStatus(t *testing.T) {
	cfg := testConfig()
	cfg.Repo.RequireChecks = true
	cfg.Repo.RequiredChecks = []string{"ci"}

	commitID := "00000001"
	commitHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	headRefName := fmt.Sprintf("spr/master/%s", commitID)
	prID := "pr_node_id_42"
	prNumber := 42

	// Build JSON response for the in-memory handler.
	respJSON := buildGQLJSONResponse(t, prNumber, "ci", "COMPLETED", "SUCCESS")

	prNodes := fezzik_types.PullRequestsViewerPullRequestsNodes{
		{
			Id:          prID,
			Number:      prNumber,
			Title:       "test commit",
			Body:        "pr body",
			HeadRefName: headRefName,
			BaseRefName: "master",
			Mergeable:   fezzik_types.MergeableState_MERGEABLE,
			Commits: fezzik_types.PullRequestsViewerPullRequestsNodesCommits{
				Nodes: &fezzik_types.PullRequestsViewerPullRequestsNodesCommitsNodes{
					{
						Commit: fezzik_types.PullRequestsViewerPullRequestsNodesCommitsNodesCommit{
							Oid:             commitHash,
							MessageHeadline: "test commit",
							MessageBody:     "",
						},
					},
				},
			},
		},
	}

	api := &fakeAPI{
		pullRequests: func(ctx context.Context, repoOwner, repoName string) (*genclient.PullRequestsResponse, error) {
			return &genclient.PullRequestsResponse{
				Viewer: genclient.PullRequestsViewer{
					Login: "testuser",
					PullRequests: fezzik_types.PullRequestConnection{
						Nodes: &prNodes,
					},
				},
				Repository: &genclient.PullRequestsRepository{Id: "repoXXX"},
			}, nil
		},
	}

	commits := []*git.Commit{
		{CommitID: commitID, CommitHash: commitHash, Subject: "test commit"},
	}
	gitcmd := setupMockGitForGetInfo(t, commits)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(respJSON)
	})

	c := newTestClient(cfg, api)
	c.graphqlEndpoint = "http://fake-endpoint/graphql"
	c.httpClient = newInMemoryClient(handler)

	info := c.GetInfo(context.Background(), gitcmd)

	require.NotNil(t, info)
	require.Len(t, info.PullRequests, 1)
	// The fetchRequiredChecksStatus should have set ChecksPass to Pass
	require.Equal(t, github.CheckStatusPass, info.PullRequests[0].MergeStatus.ChecksPass)
	gitcmd.ExpectationsMet()
}

// ---------------------------------------------------------------------------
// GetAssignableUsers tests
// ---------------------------------------------------------------------------

func TestGetAssignableUsers_SinglePage(t *testing.T) {
	cfg := testConfig()

	nameAlice := "Alice"
	nodes := genclient.AssignableUsersRepositoryAssignableUsersNodes{
		{Id: "U1", Login: "alice", Name: &nameAlice},
		{Id: "U2", Login: "bob", Name: nil},
	}

	api := &fakeAPI{
		assignableUsers: func(ctx context.Context, repoOwner, repoName string, endCursor *string) (*genclient.AssignableUsersResponse, error) {
			require.Nil(t, endCursor)
			return &genclient.AssignableUsersResponse{
				Repository: &genclient.AssignableUsersRepository{
					AssignableUsers: genclient.AssignableUsersRepositoryAssignableUsers{
						Nodes: &nodes,
						PageInfo: genclient.AssignableUsersRepositoryAssignableUsersPageInfo{
							HasNextPage: false,
							EndCursor:   nil,
						},
					},
				},
			}, nil
		},
	}
	c := newTestClient(cfg, api)

	users := c.GetAssignableUsers(context.Background())

	require.Len(t, users, 2)
	require.Equal(t, "U1", users[0].ID)
	require.Equal(t, "alice", users[0].Login)
	require.Equal(t, "Alice", users[0].Name)
	require.Equal(t, "U2", users[1].ID)
	require.Equal(t, "bob", users[1].Login)
	require.Equal(t, "", users[1].Name)
}

func TestGetAssignableUsers_Pagination(t *testing.T) {
	cfg := testConfig()

	callCount := 0
	cursor1 := "cursor1"

	nameAlice := "Alice"
	nameBob := "Bob"
	nodesPage1 := genclient.AssignableUsersRepositoryAssignableUsersNodes{
		{Id: "U1", Login: "alice", Name: &nameAlice},
	}
	nodesPage2 := genclient.AssignableUsersRepositoryAssignableUsersNodes{
		{Id: "U2", Login: "bob", Name: &nameBob},
	}

	api := &fakeAPI{
		assignableUsers: func(ctx context.Context, repoOwner, repoName string, endCursor *string) (*genclient.AssignableUsersResponse, error) {
			callCount++
			if callCount == 1 {
				require.Nil(t, endCursor)
				return &genclient.AssignableUsersResponse{
					Repository: &genclient.AssignableUsersRepository{
						AssignableUsers: genclient.AssignableUsersRepositoryAssignableUsers{
							Nodes: &nodesPage1,
							PageInfo: genclient.AssignableUsersRepositoryAssignableUsersPageInfo{
								HasNextPage: true,
								EndCursor:   &cursor1,
							},
						},
					},
				}, nil
			}
			// Second call
			require.NotNil(t, endCursor)
			require.Equal(t, "cursor1", *endCursor)
			return &genclient.AssignableUsersResponse{
				Repository: &genclient.AssignableUsersRepository{
					AssignableUsers: genclient.AssignableUsersRepositoryAssignableUsers{
						Nodes: &nodesPage2,
						PageInfo: genclient.AssignableUsersRepositoryAssignableUsersPageInfo{
							HasNextPage: false,
							EndCursor:   nil,
						},
					},
				},
			}, nil
		},
	}
	c := newTestClient(cfg, api)

	users := c.GetAssignableUsers(context.Background())

	require.Equal(t, 2, callCount)
	require.Len(t, users, 2)
	require.Equal(t, "U1", users[0].ID)
	require.Equal(t, "U2", users[1].ID)
}

// ---------------------------------------------------------------------------
// CreatePullRequest tests
// ---------------------------------------------------------------------------

func TestCreatePullRequest(t *testing.T) {
	cfg := testConfig()
	// Use basic template type so no git calls needed
	cfg.Repo.PRTemplateType = "basic"

	commit := git.Commit{
		CommitID:   "00000001",
		CommitHash: "deadbeef",
		Subject:    "my cool feature",
		Body:       "some body",
	}

	called := false
	api := &fakeAPI{
		createPullRequest: func(ctx context.Context, input genclient.CreatePullRequestInput) (*genclient.CreatePullRequestResponse, error) {
			called = true
			require.Equal(t, "repo-id-1", input.RepositoryId)
			require.Equal(t, "master", input.BaseRefName)
			require.Equal(t, "spr/master/00000001", input.HeadRefName)
			return &genclient.CreatePullRequestResponse{
				CreatePullRequest: &genclient.CreatePullRequestCreatePullRequest{
					PullRequest: &genclient.CreatePullRequestCreatePullRequestPullRequest{
						Id:     "pr_id_new",
						Number: 99,
						Body:   "created body",
					},
				},
			}, nil
		},
	}
	c := newTestClient(cfg, api)
	gitcmd := mockgit.NewMockGit(t)
	info := &github.GitHubInfo{
		UserName:     "testuser",
		RepositoryID: "repo-id-1",
		LocalBranch:  "master",
	}

	pr := c.CreatePullRequest(context.Background(), gitcmd, info, commit, nil)

	require.True(t, called, "createPullRequest should have been called")
	require.NotNil(t, pr)
	require.Equal(t, "pr_id_new", pr.ID)
	require.Equal(t, 99, pr.Number)
	require.Equal(t, "created body", pr.Body)
	require.Equal(t, "spr/master/00000001", pr.FromBranch)
	require.Equal(t, "master", pr.ToBranch)
}

func TestCreatePullRequest_WithPrevCommit(t *testing.T) {
	cfg := testConfig()
	cfg.Repo.PRTemplateType = "basic"

	prevCommit := git.Commit{
		CommitID: "00000001",
		Subject:  "prev commit",
	}
	commit := git.Commit{
		CommitID: "00000002",
		Subject:  "second commit",
	}

	called := false
	api := &fakeAPI{
		createPullRequest: func(ctx context.Context, input genclient.CreatePullRequestInput) (*genclient.CreatePullRequestResponse, error) {
			called = true
			// Base should be the branch of the previous commit
			require.Equal(t, "spr/master/00000001", input.BaseRefName)
			require.Equal(t, "spr/master/00000002", input.HeadRefName)
			return &genclient.CreatePullRequestResponse{
				CreatePullRequest: &genclient.CreatePullRequestCreatePullRequest{
					PullRequest: &genclient.CreatePullRequestCreatePullRequestPullRequest{
						Id:     "pr2",
						Number: 2,
						Body:   "body2",
					},
				},
			}, nil
		},
	}
	c := newTestClient(cfg, api)
	gitcmd := mockgit.NewMockGit(t)
	info := &github.GitHubInfo{RepositoryID: "repo-id"}

	pr := c.CreatePullRequest(context.Background(), gitcmd, info, commit, &prevCommit)

	require.True(t, called)
	require.Equal(t, "spr/master/00000001", pr.ToBranch)
	require.Equal(t, "spr/master/00000002", pr.FromBranch)
}

// ---------------------------------------------------------------------------
// UpdatePullRequest tests
// ---------------------------------------------------------------------------

func TestUpdatePullRequest_WithChanges(t *testing.T) {
	cfg := testConfig()
	cfg.Repo.PRTemplateType = "basic"

	pr := &github.PullRequest{
		ID:       "pr_1",
		Number:   10,
		Title:    "old title",
		Body:     "old body",
		ToBranch: "master",
		InQueue:  false,
	}
	commit := git.Commit{
		CommitID: "00000001",
		Subject:  "new title", // different from pr.Title -> triggers update
		Body:     "new body",
	}

	called := false
	api := &fakeAPI{
		updatePullRequest: func(ctx context.Context, input genclient.UpdatePullRequestInput) (*genclient.UpdatePullRequestResponse, error) {
			called = true
			require.Equal(t, "pr_1", input.PullRequestId)
			return &genclient.UpdatePullRequestResponse{
				UpdatePullRequest: &genclient.UpdatePullRequestUpdatePullRequest{
					PullRequest: &genclient.UpdatePullRequestUpdatePullRequestPullRequest{Number: 10},
				},
			}, nil
		},
	}
	c := newTestClient(cfg, api)
	gitcmd := mockgit.NewMockGit(t)
	info := &github.GitHubInfo{RepositoryID: "repo1"}

	c.UpdatePullRequest(context.Background(), gitcmd, info, []*github.PullRequest{pr}, pr, commit, nil)

	require.True(t, called, "updatePullRequest API should have been called")
}

func TestUpdatePullRequest_NoChanges_Skipped(t *testing.T) {
	cfg := testConfig()
	cfg.Repo.PRTemplateType = "basic"

	// For the basic template:
	//   Title() = commit.Subject
	//   Body()  = commit.Body + "\n\n" + ManualMergeNotice()
	// So to skip the update, pr.Title and pr.Body must match EXACTLY.
	const manualMergeNotice = "⚠️ *Part of a stack created by [spr](https://github.com/ejoffe/spr). Do not merge manually using the UI - doing so may have unexpected results.*"
	commit := git.Commit{
		CommitID: "00000001",
		Subject:  "same title",
		Body:     "",
	}
	expectedBody := "\n\n" + manualMergeNotice

	pr := &github.PullRequest{
		ID:       "pr_skip",
		Number:   5,
		Title:    "same title",
		Body:     expectedBody,
		ToBranch: "master",
		InQueue:  false,
	}

	api := &fakeAPI{
		updatePullRequest: func(ctx context.Context, input genclient.UpdatePullRequestInput) (*genclient.UpdatePullRequestResponse, error) {
			t.Fatal("updatePullRequest should NOT have been called")
			return nil, nil
		},
	}
	c := newTestClient(cfg, api)
	gitcmd := mockgit.NewMockGit(t)
	info := &github.GitHubInfo{RepositoryID: "repo1"}

	// Should not panic or call API
	c.UpdatePullRequest(context.Background(), gitcmd, info, []*github.PullRequest{pr}, pr, commit, nil)
}

func TestUpdatePullRequest_PreserveTitleAndBody(t *testing.T) {
	cfg := testConfig()
	cfg.Repo.PRTemplateType = "basic"
	cfg.User.PreserveTitleAndBody = true

	// With PreserveTitleAndBody=true, titleUnchanged=true and bodyUnchanged=true always.
	// The only way the update can still be triggered is if baseRefName changes.
	// Use prevCommit so baseRefName = "spr/master/00000000", but pr.ToBranch = "master" -> different.
	prevCommit := &git.Commit{
		CommitID: "00000000",
		Subject:  "prev commit",
	}
	pr := &github.PullRequest{
		ID:       "pr_preserve",
		Number:   7,
		Title:    "old title",
		Body:     "old body",
		ToBranch: "master", // will differ from computed baseRefName = spr/master/00000000
		InQueue:  false,
	}
	commit := git.Commit{
		CommitID: "00000001",
		Subject:  "new title",
		Body:     "new body",
	}

	var capturedInput genclient.UpdatePullRequestInput
	api := &fakeAPI{
		updatePullRequest: func(ctx context.Context, input genclient.UpdatePullRequestInput) (*genclient.UpdatePullRequestResponse, error) {
			capturedInput = input
			return &genclient.UpdatePullRequestResponse{
				UpdatePullRequest: &genclient.UpdatePullRequestUpdatePullRequest{
					PullRequest: &genclient.UpdatePullRequestUpdatePullRequestPullRequest{Number: 7},
				},
			}, nil
		},
	}
	c := newTestClient(cfg, api)
	gitcmd := mockgit.NewMockGit(t)
	info := &github.GitHubInfo{RepositoryID: "repo1"}

	c.UpdatePullRequest(context.Background(), gitcmd, info, []*github.PullRequest{pr}, pr, commit, prevCommit)

	// With PreserveTitleAndBody=true, Title and Body should be nil in the input
	require.Nil(t, capturedInput.Title, "Title should be nil when PreserveTitleAndBody=true")
	require.Nil(t, capturedInput.Body, "Body should be nil when PreserveTitleAndBody=true")
	// BaseRefName should still be set (since InQueue=false and base changed)
	require.NotNil(t, capturedInput.BaseRefName, "BaseRefName should be set")
	require.Equal(t, "spr/master/00000000", *capturedInput.BaseRefName)
}

func TestUpdatePullRequest_InQueue_SkipsBaseUpdate(t *testing.T) {
	cfg := testConfig()
	cfg.Repo.PRTemplateType = "basic"

	// When InQueue=true, baseUnchanged is forced to true.
	// Title change must still trigger update.
	commit := git.Commit{
		CommitID: "00000001",
		Subject:  "changed title",
		Body:     "",
	}
	pr := &github.PullRequest{
		ID:       "pr_inqueue",
		Number:   8,
		Title:    "old title", // Different from commit.Subject -> causes update
		Body:     "",
		ToBranch: "master",
		InQueue:  true,
	}

	var capturedInput genclient.UpdatePullRequestInput
	api := &fakeAPI{
		updatePullRequest: func(ctx context.Context, input genclient.UpdatePullRequestInput) (*genclient.UpdatePullRequestResponse, error) {
			capturedInput = input
			return &genclient.UpdatePullRequestResponse{
				UpdatePullRequest: &genclient.UpdatePullRequestUpdatePullRequest{
					PullRequest: &genclient.UpdatePullRequestUpdatePullRequestPullRequest{Number: 8},
				},
			}, nil
		},
	}
	c := newTestClient(cfg, api)
	gitcmd := mockgit.NewMockGit(t)
	info := &github.GitHubInfo{RepositoryID: "repo1"}

	c.UpdatePullRequest(context.Background(), gitcmd, info, []*github.PullRequest{pr}, pr, commit, nil)

	// BaseRefName should NOT be set in input since pr.InQueue=true
	require.Nil(t, capturedInput.BaseRefName, "BaseRefName should not be set when InQueue=true")
}

// ---------------------------------------------------------------------------
// AddReviewers tests
// ---------------------------------------------------------------------------

func TestAddReviewers(t *testing.T) {
	cfg := testConfig()

	pr := &github.PullRequest{
		ID:     "pr_rev",
		Number: 11,
		Title:  "reviewable PR",
	}
	userIDs := []string{"U1", "U2"}

	called := false
	api := &fakeAPI{
		addReviewers: func(ctx context.Context, input genclient.RequestReviewsInput) (*genclient.AddReviewersResponse, error) {
			called = true
			require.Equal(t, "pr_rev", input.PullRequestId)
			require.NotNil(t, input.UserIds)
			require.Equal(t, userIDs, *input.UserIds)
			return &genclient.AddReviewersResponse{
				RequestReviews: &genclient.AddReviewersRequestReviews{
					PullRequest: &genclient.AddReviewersRequestReviewsPullRequest{Id: "pr_rev"},
				},
			}, nil
		},
	}
	c := newTestClient(cfg, api)

	c.AddReviewers(context.Background(), pr, userIDs)

	require.True(t, called)
}

// ---------------------------------------------------------------------------
// CommentPullRequest tests
// ---------------------------------------------------------------------------

func TestCommentPullRequest(t *testing.T) {
	cfg := testConfig()

	pr := &github.PullRequest{
		ID:     "pr_comment",
		Number: 12,
		Title:  "commented PR",
	}
	comment := "hello comment"

	called := false
	api := &fakeAPI{
		commentPullRequest: func(ctx context.Context, input genclient.AddCommentInput) (*genclient.CommentPullRequestResponse, error) {
			called = true
			require.Equal(t, "pr_comment", input.SubjectId)
			require.Equal(t, "hello comment", input.Body)
			return &genclient.CommentPullRequestResponse{
				AddComment: &genclient.CommentPullRequestAddComment{},
			}, nil
		},
	}
	c := newTestClient(cfg, api)

	c.CommentPullRequest(context.Background(), pr, comment)

	require.True(t, called)
}

// ---------------------------------------------------------------------------
// MergePullRequest tests
// ---------------------------------------------------------------------------

func TestMergePullRequest_Direct(t *testing.T) {
	cfg := testConfig()
	cfg.Repo.MergeQueue = false

	pr := &github.PullRequest{
		ID:     "pr_merge",
		Number: 20,
		Title:  "merging PR",
	}
	mergeMethod := genclient.PullRequestMergeMethod_REBASE

	called := false
	api := &fakeAPI{
		mergePullRequest: func(ctx context.Context, input genclient.MergePullRequestInput) (*genclient.MergePullRequestResponse, error) {
			called = true
			require.Equal(t, "pr_merge", input.PullRequestId)
			require.NotNil(t, input.MergeMethod)
			require.Equal(t, genclient.PullRequestMergeMethod_REBASE, *input.MergeMethod)
			return &genclient.MergePullRequestResponse{
				MergePullRequest: &genclient.MergePullRequestMergePullRequest{
					PullRequest: &genclient.MergePullRequestMergePullRequestPullRequest{Number: 20},
				},
			}, nil
		},
	}
	c := newTestClient(cfg, api)

	c.MergePullRequest(context.Background(), pr, mergeMethod)

	require.True(t, called)
}

func TestMergePullRequest_AutoMerge(t *testing.T) {
	cfg := testConfig()
	cfg.Repo.MergeQueue = true

	pr := &github.PullRequest{
		ID:     "pr_automerge",
		Number: 21,
		Title:  "auto-merge PR",
	}
	mergeMethod := genclient.PullRequestMergeMethod_SQUASH

	called := false
	api := &fakeAPI{
		autoMergePullRequest: func(ctx context.Context, input genclient.EnablePullRequestAutoMergeInput) (*genclient.AutoMergePullRequestResponse, error) {
			called = true
			require.Equal(t, "pr_automerge", input.PullRequestId)
			require.NotNil(t, input.MergeMethod)
			require.Equal(t, genclient.PullRequestMergeMethod_SQUASH, *input.MergeMethod)
			return &genclient.AutoMergePullRequestResponse{
				EnablePullRequestAutoMerge: &genclient.AutoMergePullRequestEnablePullRequestAutoMerge{
					PullRequest: &genclient.AutoMergePullRequestEnablePullRequestAutoMergePullRequest{Number: 21},
				},
			}, nil
		},
	}
	c := newTestClient(cfg, api)

	c.MergePullRequest(context.Background(), pr, mergeMethod)

	require.True(t, called)
}

// ---------------------------------------------------------------------------
// ClosePullRequest tests
// ---------------------------------------------------------------------------

func TestClosePullRequest(t *testing.T) {
	cfg := testConfig()

	pr := &github.PullRequest{
		ID:     "pr_close",
		Number: 30,
		Title:  "closing PR",
	}

	called := false
	api := &fakeAPI{
		closePullRequest: func(ctx context.Context, input genclient.ClosePullRequestInput) (*genclient.ClosePullRequestResponse, error) {
			called = true
			require.Equal(t, "pr_close", input.PullRequestId)
			return &genclient.ClosePullRequestResponse{
				ClosePullRequest: &genclient.ClosePullRequestClosePullRequest{
					PullRequest: &genclient.ClosePullRequestClosePullRequestPullRequest{Number: 30},
				},
			}, nil
		},
	}
	c := newTestClient(cfg, api)

	c.ClosePullRequest(context.Background(), pr)

	require.True(t, called)
}

// ---------------------------------------------------------------------------
// isStar tests
// ---------------------------------------------------------------------------

func makeStarCheckResponse(nodes []string, edges []string) *genclient.StarCheckResponse {
	n := make(genclient.StarCheckViewerStarredRepositoriesNodes, len(nodes))
	for i, name := range nodes {
		nameCopy := name
		n[i] = &struct{ NameWithOwner string }{NameWithOwner: nameCopy}
	}

	e := make(genclient.StarCheckViewerStarredRepositoriesEdges, len(edges))
	for i, cursor := range edges {
		cursorCopy := cursor
		e[i] = &struct{ Cursor string }{Cursor: cursorCopy}
	}

	return &genclient.StarCheckResponse{
		Viewer: genclient.StarCheckViewer{
			StarredRepositories: genclient.StarCheckViewerStarredRepositories{
				Nodes:      &n,
				Edges:      &e,
				TotalCount: len(nodes),
			},
		},
	}
}

func TestIsStar_Found(t *testing.T) {
	cfg := testConfig()

	api := &fakeAPI{
		starCheck: func(ctx context.Context, after *string) (*genclient.StarCheckResponse, error) {
			return makeStarCheckResponse(
				[]string{"someuser/somerepo", "ejoffe/spr"},
				[]string{"cursor1", "cursor2"},
			), nil
		},
	}
	c := newTestClient(cfg, api)

	found, err := c.isStar(context.Background())
	require.NoError(t, err)
	require.True(t, found)
}

func TestIsStar_NotFound(t *testing.T) {
	cfg := testConfig()

	api := &fakeAPI{
		starCheck: func(ctx context.Context, after *string) (*genclient.StarCheckResponse, error) {
			// Return empty edges -> loop exits immediately
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
	}
	c := newTestClient(cfg, api)

	found, err := c.isStar(context.Background())
	require.NoError(t, err)
	require.False(t, found)
}

func TestIsStar_FoundAfterPagination(t *testing.T) {
	cfg := testConfig()

	callCount := 0
	api := &fakeAPI{
		starCheck: func(ctx context.Context, after *string) (*genclient.StarCheckResponse, error) {
			callCount++
			if callCount == 1 {
				// First page: doesn't contain the repo, has edges so loop continues
				return makeStarCheckResponse(
					[]string{"other/repo1"},
					[]string{"page1cursor"},
				), nil
			}
			// Second page: contains spr
			return makeStarCheckResponse(
				[]string{"ejoffe/spr"},
				[]string{"page2cursor"},
			), nil
		},
	}
	c := newTestClient(cfg, api)

	found, err := c.isStar(context.Background())
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, 2, callCount)
}

func TestIsStar_TooManyIterations(t *testing.T) {
	cfg := testConfig()

	api := &fakeAPI{
		starCheck: func(ctx context.Context, after *string) (*genclient.StarCheckResponse, error) {
			// Always returns a non-spr repo with edges, so loop keeps going
			return makeStarCheckResponse(
				[]string{"other/repo"},
				[]string{"somecursor"},
			), nil
		},
	}
	c := newTestClient(cfg, api)

	found, err := c.isStar(context.Background())
	require.NoError(t, err)
	require.False(t, found) // bail-out after >10 iterations
}

func TestIsStar_Error(t *testing.T) {
	cfg := testConfig()

	api := &fakeAPI{
		starCheck: func(ctx context.Context, after *string) (*genclient.StarCheckResponse, error) {
			return nil, errors.New("api error")
		},
	}
	c := newTestClient(cfg, api)

	found, err := c.isStar(context.Background())
	require.Error(t, err)
	require.False(t, found)
}

// ---------------------------------------------------------------------------
// addStar tests
// ---------------------------------------------------------------------------

func TestAddStar(t *testing.T) {
	cfg := testConfig()

	starGetRepoCalled := false
	starAddCalled := false

	api := &fakeAPI{
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

	c.addStar(context.Background())

	require.True(t, starGetRepoCalled, "starGetRepo should have been called")
	require.True(t, starAddCalled, "starAdd should have been called")
}

// ---------------------------------------------------------------------------
// check() tests
// ---------------------------------------------------------------------------

func TestCheck_NilError(t *testing.T) {
	// Should not panic
	require.NotPanics(t, func() {
		check(nil)
	})
}

func TestCheck_PanicsOnError(t *testing.T) {
	require.Panics(t, func() {
		check(errors.New("unexpected error"))
	})
}

// ---------------------------------------------------------------------------
// MaybeStar tests
// ---------------------------------------------------------------------------

func TestMaybeStar_StargazerAlreadySet(t *testing.T) {
	cfg := testConfig()
	cfg.State.Stargazer = true
	cfg.State.RunCount = 0 // Would trigger if Stargazer=false

	api := &fakeAPI{
		starCheck: func(ctx context.Context, after *string) (*genclient.StarCheckResponse, error) {
			t.Fatal("starCheck should NOT have been called when Stargazer=true")
			return nil, nil
		},
	}
	c := newTestClient(cfg, api)

	// MaybeStar with Stargazer=true should return immediately without calling any API.
	c.MaybeStar(context.Background(), cfg)
}

func TestMaybeStar_NotOnCycle(t *testing.T) {
	cfg := testConfig()
	cfg.State.Stargazer = false
	cfg.State.RunCount = 1 // 1 % 25 != 0, so no API call

	api := &fakeAPI{
		starCheck: func(ctx context.Context, after *string) (*genclient.StarCheckResponse, error) {
			t.Fatal("starCheck should NOT have been called when RunCount % 25 != 0")
			return nil, nil
		},
	}
	c := newTestClient(cfg, api)

	// Should not call any API.
	c.MaybeStar(context.Background(), cfg)
}

// ---------------------------------------------------------------------------
// fetchRequiredChecksStatus direct tests (via in-memory transport)
// ---------------------------------------------------------------------------

func TestFetchRequiredChecksStatus_Success(t *testing.T) {
	cfg := testConfig()
	cfg.Repo.RequiredChecks = []string{"ci", "lint"}

	prNumber := 100
	prID := "pr_node_100"

	successConclusion := "SUCCESS"
	gqlRespData := map[string]interface{}{
		fmt.Sprintf("pr_%d", prNumber): map[string]interface{}{
			"number": prNumber,
			"commits": map[string]interface{}{
				"nodes": []interface{}{
					map[string]interface{}{
						"commit": map[string]interface{}{
							"statusCheckRollup": map[string]interface{}{
								"contexts": map[string]interface{}{
									"nodes": []interface{}{
										map[string]interface{}{
											"__typename": "CheckRun",
											"name":       "ci",
											"status":     "COMPLETED",
											"conclusion": successConclusion,
											"context":    "",
											"state":      "",
										},
										map[string]interface{}{
											"__typename": "CheckRun",
											"name":       "lint",
											"status":     "COMPLETED",
											"conclusion": successConclusion,
											"context":    "",
											"state":      "",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	gqlRespJSON, err := json.Marshal(map[string]interface{}{"data": gqlRespData})
	require.NoError(t, err)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(gqlRespJSON)
	})

	c := &client{
		config:          cfg,
		api:             &fakeAPI{},
		graphqlEndpoint: "http://fake-endpoint/graphql",
		httpClient:      newInMemoryClient(handler),
	}

	prs := []*github.PullRequest{
		{ID: prID, Number: prNumber},
	}

	result := c.fetchRequiredChecksStatus(context.Background(), prs)

	require.NotNil(t, result)
	require.Contains(t, result, prNumber)
	require.Equal(t, github.CheckStatusPass, result[prNumber])
}

func TestFetchRequiredChecksStatus_EmptyPRs(t *testing.T) {
	cfg := testConfig()
	cfg.Repo.RequiredChecks = []string{"ci"}

	c := &client{
		config:          cfg,
		api:             &fakeAPI{},
		graphqlEndpoint: "http://fake-endpoint/graphql",
		httpClient:      &http.Client{},
	}

	result := c.fetchRequiredChecksStatus(context.Background(), []*github.PullRequest{})
	require.Nil(t, result)
}

func TestFetchRequiredChecksStatus_GraphQLError(t *testing.T) {
	cfg := testConfig()
	cfg.Repo.RequiredChecks = []string{"ci"}

	gqlRespJSON, err := json.Marshal(map[string]interface{}{
		"errors": []interface{}{
			map[string]interface{}{"message": "some graphql error"},
		},
	})
	require.NoError(t, err)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(gqlRespJSON)
	})

	c := &client{
		config:          cfg,
		api:             &fakeAPI{},
		graphqlEndpoint: "http://fake-endpoint/graphql",
		httpClient:      newInMemoryClient(handler),
	}

	prs := []*github.PullRequest{{ID: "pr1", Number: 1}}
	result := c.fetchRequiredChecksStatus(context.Background(), prs)
	// Should return nil on GraphQL errors
	require.Nil(t, result)
}

func TestFetchRequiredChecksStatus_NoStatusCheckRollup(t *testing.T) {
	cfg := testConfig()
	cfg.Repo.RequiredChecks = []string{"ci"}

	prNumber := 200
	prID := "pr_node_200"

	// Return a commit with nil statusCheckRollup (no checks configured)
	gqlRespData := map[string]interface{}{
		fmt.Sprintf("pr_%d", prNumber): map[string]interface{}{
			"number": prNumber,
			"commits": map[string]interface{}{
				"nodes": []interface{}{
					map[string]interface{}{
						"commit": map[string]interface{}{
							"statusCheckRollup": nil,
						},
					},
				},
			},
		},
	}
	gqlRespJSON, err := json.Marshal(map[string]interface{}{"data": gqlRespData})
	require.NoError(t, err)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(gqlRespJSON)
	})

	c := &client{
		config:          cfg,
		api:             &fakeAPI{},
		graphqlEndpoint: "http://fake-endpoint/graphql",
		httpClient:      newInMemoryClient(handler),
	}

	prs := []*github.PullRequest{{ID: prID, Number: prNumber}}
	result := c.fetchRequiredChecksStatus(context.Background(), prs)

	require.NotNil(t, result)
	// nil statusCheckRollup means no checks configured -> pass
	require.Equal(t, github.CheckStatusPass, result[prNumber])
}

func TestFetchRequiredChecksStatus_RequiredCheckPending(t *testing.T) {
	cfg := testConfig()
	cfg.Repo.RequiredChecks = []string{"ci"}

	prNumber := 300
	prID := "pr_node_300"

	gqlRespData := map[string]interface{}{
		fmt.Sprintf("pr_%d", prNumber): map[string]interface{}{
			"number": prNumber,
			"commits": map[string]interface{}{
				"nodes": []interface{}{
					map[string]interface{}{
						"commit": map[string]interface{}{
							"statusCheckRollup": map[string]interface{}{
								"contexts": map[string]interface{}{
									"nodes": []interface{}{
										map[string]interface{}{
											"__typename": "CheckRun",
											"name":       "ci",
											"status":     "IN_PROGRESS",
											"conclusion": nil,
											"context":    "",
											"state":      "",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	gqlRespJSON, err := json.Marshal(map[string]interface{}{"data": gqlRespData})
	require.NoError(t, err)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(gqlRespJSON)
	})

	c := &client{
		config:          cfg,
		api:             &fakeAPI{},
		graphqlEndpoint: "http://fake-endpoint/graphql",
		httpClient:      newInMemoryClient(handler),
	}

	prs := []*github.PullRequest{{ID: prID, Number: prNumber}}
	result := c.fetchRequiredChecksStatus(context.Background(), prs)

	require.NotNil(t, result)
	require.Equal(t, github.CheckStatusPending, result[prNumber])
}

func TestFetchRequiredChecksStatus_RequiredCheckFailed(t *testing.T) {
	cfg := testConfig()
	cfg.Repo.RequiredChecks = []string{"ci"}

	prNumber := 400
	prID := "pr_node_400"
	failureConclusion := "FAILURE"

	gqlRespData := map[string]interface{}{
		fmt.Sprintf("pr_%d", prNumber): map[string]interface{}{
			"number": prNumber,
			"commits": map[string]interface{}{
				"nodes": []interface{}{
					map[string]interface{}{
						"commit": map[string]interface{}{
							"statusCheckRollup": map[string]interface{}{
								"contexts": map[string]interface{}{
									"nodes": []interface{}{
										map[string]interface{}{
											"__typename": "CheckRun",
											"name":       "ci",
											"status":     "COMPLETED",
											"conclusion": failureConclusion,
											"context":    "",
											"state":      "",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	gqlRespJSON, err := json.Marshal(map[string]interface{}{"data": gqlRespData})
	require.NoError(t, err)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(gqlRespJSON)
	})

	c := &client{
		config:          cfg,
		api:             &fakeAPI{},
		graphqlEndpoint: "http://fake-endpoint/graphql",
		httpClient:      newInMemoryClient(handler),
	}

	prs := []*github.PullRequest{{ID: prID, Number: prNumber}}
	result := c.fetchRequiredChecksStatus(context.Background(), prs)

	require.NotNil(t, result)
	require.Equal(t, github.CheckStatusFail, result[prNumber])
}

// TestFetchRequiredChecksStatus_StatusContext exercises the StatusContext
// (legacy commit status) union branch of the response, which drives
// computeRequiredCheckStatus's StatusContext switch and contextName's
// StatusContext branch.
func TestFetchRequiredChecksStatus_StatusContext(t *testing.T) {
	cfg := testConfig()
	cfg.Repo.RequiredChecks = []string{"ci/build"}

	prNumber := 500
	prID := "pr_node_500"

	gqlRespData := map[string]interface{}{
		fmt.Sprintf("pr_%d", prNumber): map[string]interface{}{
			"number": prNumber,
			"commits": map[string]interface{}{
				"nodes": []interface{}{
					map[string]interface{}{
						"commit": map[string]interface{}{
							"statusCheckRollup": map[string]interface{}{
								"contexts": map[string]interface{}{
									"nodes": []interface{}{
										map[string]interface{}{
											"__typename": "StatusContext",
											"context":    "ci/build",
											"state":      "SUCCESS",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	gqlRespJSON, err := json.Marshal(map[string]interface{}{"data": gqlRespData})
	require.NoError(t, err)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(gqlRespJSON)
	})

	c := &client{
		config:          cfg,
		api:             &fakeAPI{},
		graphqlEndpoint: "http://fake-endpoint/graphql",
		httpClient:      newInMemoryClient(handler),
	}

	prs := []*github.PullRequest{{ID: prID, Number: prNumber}}
	result := c.fetchRequiredChecksStatus(context.Background(), prs)

	require.NotNil(t, result)
	require.Equal(t, github.CheckStatusPass, result[prNumber])
}

// ---------------------------------------------------------------------------
// Token / CLI-config resolution tests
//
// readHubCLIConfig, readGhCLIConfig and findToken are unexported, so they are
// directly callable from this internal (package githubclient) test. They read
// CLI config files from the user home directory; t.Setenv("HOME", ...) points
// os.UserHomeDir() at a temp dir so the file paths are fully controlled with no
// network and no production-code change.
//
// NOTE: t.Setenv forbids t.Parallel in these tests (enforced by the runtime).
// ---------------------------------------------------------------------------

// writeFile writes content to path, creating parent dirs as needed.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func TestReadHubCLIConfig_Success(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeFile(t, filepath.Join(home, ".config", "hub"), `github.com:
- user: octocat
  oauth_token: hubtoken123
  protocol: https
`)

	cfg, err := readHubCLIConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	entries, ok := cfg["github.com"]
	require.True(t, ok)
	require.Len(t, entries, 1)
	require.Equal(t, "octocat", entries[0].User)
	require.Equal(t, "hubtoken123", entries[0].OauthToken)
	require.Equal(t, "https", entries[0].Protocol)
}

func TestReadHubCLIConfig_MissingFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// No ~/.config/hub written.
	cfg, err := readHubCLIConfig()
	require.Error(t, err)
	require.Nil(t, cfg)
	require.Contains(t, err.Error(), "failed to open hub config file")
}

func TestReadGhCLIConfig_Success(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeFile(t, filepath.Join(home, ".config", "gh", "hosts.yml"), `github.com:
  user: octocat
  oauth_token: ghtoken456
  git_protocol: https
`)

	cfg, err := readGhCLIConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	entry, ok := (*cfg)["github.com"]
	require.True(t, ok)
	require.Equal(t, "octocat", entry.User)
	require.Equal(t, "ghtoken456", entry.OauthToken)
	require.Equal(t, "https", entry.GitProtocol)
}

func TestReadGhCLIConfig_MissingFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg, err := readGhCLIConfig()
	require.Error(t, err)
	require.Nil(t, cfg)
	require.Contains(t, err.Error(), "failed to open gh cli config file")
}

func TestFindToken_EnvVarFastPath(t *testing.T) {
	// Point HOME at an empty dir so config-file fallbacks would yield "".
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GITHUB_TOKEN", "env-token-xyz")

	require.Equal(t, "env-token-xyz", findToken("github.com"))
}

func TestFindToken_GhConfigMatch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GITHUB_TOKEN", "")

	writeFile(t, filepath.Join(home, ".config", "gh", "hosts.yml"), `github.com:
  user: octocat
  oauth_token: gh-oauth-token
  git_protocol: https
`)

	// When the host matches, findToken calls keyring.Get. In an environment
	// without a secrets backend keyring.Get errors and findToken falls back to
	// the config's oauth_token (client.go:99). The keyring-success branch
	// (client.go:101) requires a real keyring and is a recorded deferred gap.
	require.Equal(t, "gh-oauth-token", findToken("github.com"))
}

func TestFindToken_HubConfigFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GITHUB_TOKEN", "")

	// No gh hosts.yml (so gh path fails), but a hub config exists.
	writeFile(t, filepath.Join(home, ".config", "hub"), `github.com:
- user: octocat
  oauth_token: hub-oauth-token
  protocol: https
`)

	require.Equal(t, "hub-oauth-token", findToken("github.com"))
}

func TestFindToken_HubConfigMultipleEntries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GITHUB_TOKEN", "")

	// Two entries triggers the "multiple tokens" warn branch; first is used.
	writeFile(t, filepath.Join(home, ".config", "hub"), `github.com:
- user: first
  oauth_token: first-token
  protocol: https
- user: second
  oauth_token: second-token
  protocol: https
`)

	require.Equal(t, "first-token", findToken("github.com"))
}

func TestFindToken_NoTokenAnywhere(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GITHUB_TOKEN", "")

	// No gh config and no hub config: both reads fail, findToken returns "".
	require.Equal(t, "", findToken("github.com"))
}

func TestFindToken_HubConfigNoMatchingHost(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GITHUB_TOKEN", "")

	// hub config present but keyed under a different host: the "github.com"
	// lookup misses and findToken returns "".
	writeFile(t, filepath.Join(home, ".config", "hub"), `git.example.com:
- user: octocat
  oauth_token: other-token
  protocol: https
`)

	require.Equal(t, "", findToken("github.com"))
}
