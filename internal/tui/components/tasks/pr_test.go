package tasks

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"
	gitlabapi "gitlab.com/gitlab-org/api/client-go"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
)

func TestUpdatePR_RebasesViaRESTAndDoesNotMarkPRClosed(t *testing.T) {
	defer data.SetRESTClient(nil)

	var gotMethod, gotPath string
	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	section := SectionIdentifier{Id: 2, Type: "pr"}
	pr := mockIssue{number: 42, repoName: "owner/repo"}

	cmd := UpdatePR(ctx, section, pr)
	finished := runTaskCmd(t, cmd)

	require.NoError(t, finished.Err)
	require.Equal(t, http.MethodPut, gotMethod)
	require.Contains(t, gotPath, "/merge_requests/42/rebase")

	updateMsg, ok := finished.Msg.(UpdatePRMsg)
	require.True(t, ok, "Msg should return UpdatePRMsg")
	require.Equal(t, 42, updateMsg.PrNumber)
	require.Nil(
		t,
		updateMsg.IsClosed,
		"updating a PR branch must not change the PR open/closed state",
	)
}

func TestUpdatePR_PropagatesAPIError(t *testing.T) {
	defer data.SetRESTClient(nil)

	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	pr := mockIssue{number: 42, repoName: "owner/repo"}

	cmd := UpdatePR(ctx, SectionIdentifier{Id: 2, Type: "pr"}, pr)
	finished := runTaskCmd(t, cmd)

	require.Error(t, finished.Err)
}

func TestApproveWorkflows_TaskConfiguration(t *testing.T) {
	var capturedTask context.Task

	ctx := &context.ProgramContext{
		StartTask: func(task context.Task) tea.Cmd {
			capturedTask = task
			return nil
		},
	}
	section := SectionIdentifier{Id: 2, Type: "pr"}
	pr := mockIssue{
		number:   42,
		repoName: "owner/repo",
	}

	_ = ApproveWorkflows(ctx, section, pr)

	require.Equal(t, "pr_approve_workflows_42", capturedTask.Id)
	require.Equal(t, "Approving workflows for PR #42", capturedTask.StartText)
	require.Equal(t, "Workflows for PR #42 have been approved", capturedTask.FinishedText)
	require.Equal(t, context.TaskStart, capturedTask.State)
	require.Nil(t, capturedTask.Error)
}

func TestApproveWorkflows_ReturnsNonNilCommand(t *testing.T) {
	tests := []struct {
		name     string
		prNumber int
		repoName string
	}{
		{
			name:     "standard PR",
			prNumber: 123,
			repoName: "owner/repo",
		},
		{
			name:     "large PR number",
			prNumber: 99999,
			repoName: "my-org/my-project",
		},
		{
			name:     "hyphenated repo name",
			prNumber: 1,
			repoName: "some-owner/some-repo-name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &context.ProgramContext{
				StartTask: noopStartTask,
			}
			section := SectionIdentifier{Id: 1, Type: "pr"}
			pr := mockIssue{
				number:   tt.prNumber,
				repoName: tt.repoName,
			}

			cmd := ApproveWorkflows(ctx, section, pr)

			require.NotNil(t, cmd, "ApproveWorkflows should return a non-nil command")
		})
	}
}

func TestApproveWorkflows_UsesCorrectPRNumber(t *testing.T) {
	prNumbers := []int{1, 100, 12345, 999999}

	for _, num := range prNumbers {
		t.Run(fmt.Sprintf("pr_%d", num), func(t *testing.T) {
			var capturedTask context.Task
			ctx := &context.ProgramContext{
				StartTask: func(task context.Task) tea.Cmd {
					capturedTask = task
					return nil
				},
			}
			pr := mockIssue{number: num, repoName: "o/r"}

			ApproveWorkflows(ctx, SectionIdentifier{}, pr)

			expectedId := fmt.Sprintf("pr_approve_workflows_%d", num)
			require.Equal(t, expectedId, capturedTask.Id)
			require.Contains(t, capturedTask.StartText, fmt.Sprintf("#%d", num))
			require.Contains(t, capturedTask.FinishedText, fmt.Sprintf("#%d", num))
		})
	}
}

func TestApproveWorkflows_SectionIdentifierPropagation(t *testing.T) {
	tests := []struct {
		name        string
		sectionId   int
		sectionType string
	}{
		{
			name:        "pr section type",
			sectionId:   1,
			sectionType: "pr",
		},
		{
			name:        "notification section type",
			sectionId:   10,
			sectionType: "notification",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &context.ProgramContext{
				StartTask: noopStartTask,
			}
			section := SectionIdentifier{Id: tt.sectionId, Type: tt.sectionType}
			pr := mockIssue{number: 1, repoName: "o/r"}

			cmd := ApproveWorkflows(ctx, section, pr)

			require.NotNil(t, cmd)
		})
	}
}

func newMockRESTClient(t *testing.T, handler http.HandlerFunc) *gitlabapi.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c, err := gitlabapi.NewClient(
		"test-token",
		gitlabapi.WithBaseURL(server.URL),
		gitlabapi.WithoutRetries(),
	)
	require.NoError(t, err)
	return c
}

func runTaskCmd(t *testing.T, cmd tea.Cmd) constants.TaskFinishedMsg {
	t.Helper()
	require.NotNil(t, cmd)

	msg := cmd()
	if finished, ok := msg.(constants.TaskFinishedMsg); ok {
		return finished
	}

	batch, ok := msg.(tea.BatchMsg)
	require.True(t, ok, "expected constants.TaskFinishedMsg or tea.BatchMsg, got %T", msg)

	for _, sub := range batch {
		if sub == nil {
			continue
		}
		if finished, ok := sub().(constants.TaskFinishedMsg); ok {
			return finished
		}
	}

	t.Fatal("command did not produce a constants.TaskFinishedMsg")
	return constants.TaskFinishedMsg{}
}

func decodeJSONBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
	return body
}

func TestApproveWorkflows_PlaysAllManualJobs(t *testing.T) {
	defer data.SetRESTClient(nil)

	var mu sync.Mutex
	var playedPaths []string

	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/merge_requests/42/pipelines"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(
				`[{"id":10,"status":"running","web_url":"https://gitlab.example.com/o/r/-/pipelines/10"}]`,
			))
		case strings.Contains(r.URL.Path, "/pipelines/10/jobs"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[
				{"id":100,"name":"deploy-staging","stage":"deploy","status":"manual","web_url":"https://gitlab.example.com/o/r/-/jobs/100","allow_failure":false},
				{"id":101,"name":"deploy-prod","stage":"deploy","status":"manual","web_url":"https://gitlab.example.com/o/r/-/jobs/101","allow_failure":false}
			]`))
		case strings.HasSuffix(r.URL.Path, "/play"):
			mu.Lock()
			playedPaths = append(playedPaths, r.URL.Path)
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":100,"name":"deploy-staging","status":"pending"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	pr := mockIssue{number: 42, repoName: "o/r"}

	cmd := ApproveWorkflows(ctx, SectionIdentifier{Id: 1, Type: "pr"}, pr)
	finished := runTaskCmd(t, cmd)

	require.NoError(t, finished.Err)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, playedPaths, 2, "both manual jobs should have been played")
	joinedPlayedPaths := strings.Join(playedPaths, " ")
	require.Contains(t, joinedPlayedPaths, "/jobs/100/play")
	require.Contains(t, joinedPlayedPaths, "/jobs/101/play")
}

func TestApproveWorkflows_NoManualJobs(t *testing.T) {
	defer data.SetRESTClient(nil)

	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/merge_requests/42/pipelines"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(
				`[{"id":10,"status":"running","web_url":"https://gitlab.example.com/o/r/-/pipelines/10"}]`,
			))
		case strings.Contains(r.URL.Path, "/pipelines/10/jobs"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	pr := mockIssue{number: 42, repoName: "o/r"}

	cmd := ApproveWorkflows(ctx, SectionIdentifier{Id: 1, Type: "pr"}, pr)
	finished := runTaskCmd(t, cmd)

	require.Error(t, finished.Err)
	require.Contains(t, finished.Err.Error(), "no workflows awaiting approval")
}

func TestApproveWorkflows_NoPipeline(t *testing.T) {
	defer data.SetRESTClient(nil)

	var mu sync.Mutex
	var jobsEndpointHits int

	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/merge_requests/42/pipelines"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
		case strings.Contains(r.URL.Path, "/jobs"):
			mu.Lock()
			jobsEndpointHits++
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	pr := mockIssue{number: 42, repoName: "o/r"}

	cmd := ApproveWorkflows(ctx, SectionIdentifier{Id: 1, Type: "pr"}, pr)
	finished := runTaskCmd(t, cmd)

	require.Error(t, finished.Err)
	require.Contains(t, finished.Err.Error(), "no workflows awaiting approval")

	mu.Lock()
	defer mu.Unlock()
	require.Equal(
		t,
		0,
		jobsEndpointHits,
		"ListPipelineJobs should not be called when there is no pipeline",
	)
}

func TestApproveWorkflows_PartialFailureIsBestEffort(t *testing.T) {
	defer data.SetRESTClient(nil)

	var mu sync.Mutex
	var playedPaths []string

	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/merge_requests/42/pipelines"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(
				`[{"id":10,"status":"running","web_url":"https://gitlab.example.com/o/r/-/pipelines/10"}]`,
			))
		case strings.Contains(r.URL.Path, "/pipelines/10/jobs"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[
				{"id":100,"name":"deploy-staging","stage":"deploy","status":"manual","web_url":"https://gitlab.example.com/o/r/-/jobs/100","allow_failure":false},
				{"id":101,"name":"deploy-prod","stage":"deploy","status":"manual","web_url":"https://gitlab.example.com/o/r/-/jobs/101","allow_failure":false}
			]`))
		case strings.Contains(r.URL.Path, "/jobs/100/play"):
			mu.Lock()
			playedPaths = append(playedPaths, r.URL.Path)
			mu.Unlock()
			w.WriteHeader(http.StatusForbidden)
		case strings.Contains(r.URL.Path, "/jobs/101/play"):
			mu.Lock()
			playedPaths = append(playedPaths, r.URL.Path)
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":101,"name":"deploy-prod","status":"pending"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	pr := mockIssue{number: 42, repoName: "o/r"}

	cmd := ApproveWorkflows(ctx, SectionIdentifier{Id: 1, Type: "pr"}, pr)
	finished := runTaskCmd(t, cmd)

	require.Error(t, finished.Err, "the failed play of job 100 should surface as the task error")

	mu.Lock()
	defer mu.Unlock()
	require.Len(
		t,
		playedPaths,
		2,
		"both jobs should have been attempted even though the first one failed",
	)
}

func TestApproveWorkflows_PipelineLookupError(t *testing.T) {
	defer data.SetRESTClient(nil)

	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	pr := mockIssue{number: 42, repoName: "o/r"}

	cmd := ApproveWorkflows(ctx, SectionIdentifier{Id: 1, Type: "pr"}, pr)
	finished := runTaskCmd(t, cmd)

	require.Error(t, finished.Err)
	require.Contains(t, finished.Err.Error(), "failed to locate pipeline")
}

func toInt64Slice(t *testing.T, v any) []int64 {
	t.Helper()
	if v == nil {
		return []int64{}
	}
	raw, ok := v.([]any)
	require.True(t, ok, "expected a JSON array, got %T", v)
	out := make([]int64, len(raw))
	for i, item := range raw {
		f, ok := item.(float64)
		require.True(t, ok, "expected numeric array element, got %T", item)
		out[i] = int64(f)
	}
	return out
}

func TestReopenPR_Success(t *testing.T) {
	defer data.SetRESTClient(nil)

	var gotMethod string
	var gotBody map[string]any
	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotBody = decodeJSONBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	pr := mockIssue{number: 42, repoName: "o/r"}

	cmd := ReopenPR(ctx, SectionIdentifier{Id: 1, Type: "pr"}, pr)
	finished := runTaskCmd(t, cmd)

	require.NoError(t, finished.Err)
	require.Equal(t, http.MethodPut, gotMethod)
	require.Equal(t, "reopen", gotBody["state_event"])

	msg, ok := finished.Msg.(UpdatePRMsg)
	require.True(t, ok)
	require.Equal(t, 42, msg.PrNumber)
	require.NotNil(t, msg.IsClosed)
	require.False(t, *msg.IsClosed)
}

func TestReopenPR_PropagatesAPIError(t *testing.T) {
	defer data.SetRESTClient(nil)

	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	pr := mockIssue{number: 42, repoName: "o/r"}

	cmd := ReopenPR(ctx, SectionIdentifier{Id: 1, Type: "pr"}, pr)
	finished := runTaskCmd(t, cmd)

	require.Error(t, finished.Err)
}

func TestClosePR_Success(t *testing.T) {
	defer data.SetRESTClient(nil)

	var gotBody map[string]any
	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotBody = decodeJSONBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	pr := mockIssue{number: 42, repoName: "o/r"}

	cmd := ClosePR(ctx, SectionIdentifier{Id: 1, Type: "pr"}, pr)
	finished := runTaskCmd(t, cmd)

	require.NoError(t, finished.Err)
	require.Equal(t, "close", gotBody["state_event"])

	msg, ok := finished.Msg.(UpdatePRMsg)
	require.True(t, ok)
	require.NotNil(t, msg.IsClosed)
	require.True(t, *msg.IsClosed)
}

func TestClosePR_PropagatesAPIError(t *testing.T) {
	defer data.SetRESTClient(nil)

	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	pr := mockIssue{number: 42, repoName: "o/r"}

	cmd := ClosePR(ctx, SectionIdentifier{Id: 1, Type: "pr"}, pr)
	finished := runTaskCmd(t, cmd)

	require.Error(t, finished.Err)
}

func TestPRReady_StripsDraftPrefix(t *testing.T) {
	defer data.SetRESTClient(nil)

	var putBody map[string]any
	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"title":"Draft: My great PR"}`))
		case http.MethodPut:
			putBody = decodeJSONBody(t, r)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	pr := mockIssue{number: 42, repoName: "o/r"}

	cmd := PRReady(ctx, SectionIdentifier{Id: 1, Type: "pr"}, pr)
	finished := runTaskCmd(t, cmd)

	require.NoError(t, finished.Err)
	require.Equal(t, "My great PR", putBody["title"])

	msg, ok := finished.Msg.(UpdatePRMsg)
	require.True(t, ok)
	require.NotNil(t, msg.ReadyForReview)
	require.True(t, *msg.ReadyForReview)
}

func TestPRReady_TitleWithoutPrefixStaysUnchanged(t *testing.T) {
	defer data.SetRESTClient(nil)

	var putBody map[string]any
	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"title":"My great PR"}`))
		case http.MethodPut:
			putBody = decodeJSONBody(t, r)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	pr := mockIssue{number: 42, repoName: "o/r"}

	cmd := PRReady(ctx, SectionIdentifier{Id: 1, Type: "pr"}, pr)
	finished := runTaskCmd(t, cmd)

	require.NoError(t, finished.Err)
	require.Equal(t, "My great PR", putBody["title"])
}

func TestPRReady_PropagatesAPIError(t *testing.T) {
	defer data.SetRESTClient(nil)

	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	pr := mockIssue{number: 42, repoName: "o/r"}

	cmd := PRReady(ctx, SectionIdentifier{Id: 1, Type: "pr"}, pr)
	finished := runTaskCmd(t, cmd)

	require.Error(t, finished.Err)
}

func TestAssignPR_PreservesExistingAssigneesUnion(t *testing.T) {
	defer data.SetRESTClient(nil)

	var putBody map[string]any
	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/users"):
			require.Equal(t, "bob", r.URL.Query().Get("username"))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"id":20,"username":"bob"}]`))
		case r.Method == http.MethodGet:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"assignees":[{"id":10,"username":"alice"}]}`))
		case r.Method == http.MethodPut:
			putBody = decodeJSONBody(t, r)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	pr := mockIssue{number: 42, repoName: "o/r"}

	cmd := AssignPR(ctx, SectionIdentifier{Id: 1, Type: "pr"}, pr, []string{"bob"})
	finished := runTaskCmd(t, cmd)

	require.NoError(t, finished.Err)
	require.NotNil(t, putBody)
	require.Equal(t, []int64{10, 20}, toInt64Slice(t, putBody["assignee_ids"]))

	msg, ok := finished.Msg.(UpdatePRMsg)
	require.True(t, ok)
	require.NotNil(t, msg.AddedAssignees)
	require.Equal(t, []data.Assignee{{Login: "bob"}}, msg.AddedAssignees.Nodes)
}

func TestAssignPR_UserNotFound_ReturnsError(t *testing.T) {
	defer data.SetRESTClient(nil)

	var mergeRequestEndpointHits int
	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/users") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
			return
		}
		mergeRequestEndpointHits++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	pr := mockIssue{number: 42, repoName: "o/r"}

	cmd := AssignPR(ctx, SectionIdentifier{Id: 1, Type: "pr"}, pr, []string{"ghost"})
	finished := runTaskCmd(t, cmd)

	require.Error(t, finished.Err)
	require.Contains(t, finished.Err.Error(), "gitlab user not found: ghost")
	require.Equal(
		t,
		0,
		mergeRequestEndpointHits,
		"should fail resolving usernames before ever touching the merge request",
	)
}

func TestAssignPR_EmptyUsernames_ProducesEmptyAssigneeIDsPayload(t *testing.T) {
	defer data.SetRESTClient(nil)

	var putBody map[string]any
	var usersEndpointHits int
	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/users"):
			usersEndpointHits++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodGet:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"assignees":[]}`))
		case r.Method == http.MethodPut:
			putBody = decodeJSONBody(t, r)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	pr := mockIssue{number: 42, repoName: "o/r"}

	cmd := AssignPR(ctx, SectionIdentifier{Id: 1, Type: "pr"}, pr, []string{})
	finished := runTaskCmd(t, cmd)

	require.NoError(t, finished.Err)
	require.Equal(t, 0, usersEndpointHits)
	require.NotNil(t, putBody)
	require.Equal(t, []int64{}, toInt64Slice(t, putBody["assignee_ids"]))
}

func TestUnassignPR_RemovesFromExistingAssignees(t *testing.T) {
	defer data.SetRESTClient(nil)

	var putBody map[string]any
	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/users"):
			require.Equal(t, "bob", r.URL.Query().Get("username"))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"id":20,"username":"bob"}]`))
		case r.Method == http.MethodGet:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(
				[]byte(`{"assignees":[{"id":10,"username":"alice"},{"id":20,"username":"bob"}]}`),
			)
		case r.Method == http.MethodPut:
			putBody = decodeJSONBody(t, r)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	pr := mockIssue{number: 42, repoName: "o/r"}

	cmd := UnassignPR(ctx, SectionIdentifier{Id: 1, Type: "pr"}, pr, []string{"bob"})
	finished := runTaskCmd(t, cmd)

	require.NoError(t, finished.Err)
	require.NotNil(t, putBody)
	require.Equal(t, []int64{10}, toInt64Slice(t, putBody["assignee_ids"]))

	msg, ok := finished.Msg.(UpdatePRMsg)
	require.True(t, ok)
	require.NotNil(t, msg.RemovedAssignees)
	require.Equal(t, []data.Assignee{{Login: "bob"}}, msg.RemovedAssignees.Nodes)
}

func TestCommentOnPR_Success(t *testing.T) {
	defer data.SetRESTClient(nil)

	var postBody map[string]any
	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Contains(t, r.URL.Path, "/notes")
		postBody = decodeJSONBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask, User: "tester"}
	pr := mockIssue{number: 42, repoName: "o/r"}

	cmd := CommentOnPR(ctx, SectionIdentifier{Id: 1, Type: "pr"}, pr, "nice work")
	finished := runTaskCmd(t, cmd)

	require.NoError(t, finished.Err)
	require.Equal(t, "nice work", postBody["body"])

	msg, ok := finished.Msg.(UpdatePRMsg)
	require.True(t, ok)
	require.NotNil(t, msg.NewComment)
	require.Equal(t, "nice work", msg.NewComment.Body)
	require.Equal(t, "tester", msg.NewComment.Author.Login)
}

func TestCommentOnPR_PropagatesAPIError(t *testing.T) {
	defer data.SetRESTClient(nil)

	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask, User: "tester"}
	pr := mockIssue{number: 42, repoName: "o/r"}

	cmd := CommentOnPR(ctx, SectionIdentifier{Id: 1, Type: "pr"}, pr, "nice work")
	finished := runTaskCmd(t, cmd)

	require.Error(t, finished.Err)
}

func TestApprovePR_WithCommentAlsoPostsNote(t *testing.T) {
	defer data.SetRESTClient(nil)

	var approveHits, notesHits int
	var noteBody map[string]any
	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/approve"):
			approveHits++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		case strings.Contains(r.URL.Path, "/notes"):
			notesHits++
			noteBody = decodeJSONBody(t, r)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	pr := mockIssue{number: 42, repoName: "o/r"}

	cmd := ApprovePR(ctx, SectionIdentifier{Id: 1, Type: "pr"}, pr, "lgtm")
	finished := runTaskCmd(t, cmd)

	require.NoError(t, finished.Err)
	require.Equal(t, 1, approveHits)
	require.Equal(t, 1, notesHits)
	require.Equal(t, "lgtm", noteBody["body"])
}

func TestApprovePR_WithoutCommentDoesNotPostNote(t *testing.T) {
	defer data.SetRESTClient(nil)

	var approveHits, notesHits int
	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/approve"):
			approveHits++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		case strings.Contains(r.URL.Path, "/notes"):
			notesHits++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	pr := mockIssue{number: 42, repoName: "o/r"}

	cmd := ApprovePR(ctx, SectionIdentifier{Id: 1, Type: "pr"}, pr, "")
	finished := runTaskCmd(t, cmd)

	require.NoError(t, finished.Err)
	require.Equal(t, 1, approveHits)
	require.Equal(t, 0, notesHits)
}

func TestApprovePR_PropagatesAPIError(t *testing.T) {
	defer data.SetRESTClient(nil)

	var notesHits int
	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/notes") {
			notesHits++
		}
		w.WriteHeader(http.StatusForbidden)
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	pr := mockIssue{number: 42, repoName: "o/r"}

	cmd := ApprovePR(ctx, SectionIdentifier{Id: 1, Type: "pr"}, pr, "lgtm")
	finished := runTaskCmd(t, cmd)

	require.Error(t, finished.Err)
	require.Equal(t, 0, notesHits, "a failed approval must not attempt to post the comment")
}

func TestOpenBranchPR_OpensWebURLForFoundMR(t *testing.T) {
	defer data.SetRESTClient(nil)

	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "my-branch", r.URL.Query().Get("source_branch"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(
			[]byte(`[{"iid":7,"web_url":"https://gitlab.example.com/o/r/-/merge_requests/7"}]`),
		)
	})
	data.SetRESTClient(mockClient)

	origOpenURL := openURL
	defer func() { openURL = origOpenURL }()
	var openedURL string
	openURL = func(url string) error {
		openedURL = url
		return nil
	}

	ctx := &context.ProgramContext{
		StartTask: noopStartTask,
		RepoUrl:   "https://gitlab.example.com/o/r.git",
	}

	cmd := OpenBranchPR(ctx, SectionIdentifier{Id: 1, Type: "pr"}, "my-branch")
	finished := runTaskCmd(t, cmd)

	require.NoError(t, finished.Err)
	require.Equal(t, "https://gitlab.example.com/o/r/-/merge_requests/7", openedURL)
}

func TestOpenBranchPR_NoMergeRequestFound_ReturnsError(t *testing.T) {
	defer data.SetRESTClient(nil)

	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	})
	data.SetRESTClient(mockClient)

	origOpenURL := openURL
	defer func() { openURL = origOpenURL }()
	var openCalled bool
	openURL = func(url string) error {
		openCalled = true
		return nil
	}

	ctx := &context.ProgramContext{
		StartTask: noopStartTask,
		RepoUrl:   "https://gitlab.example.com/o/r.git",
	}

	cmd := OpenBranchPR(ctx, SectionIdentifier{Id: 1, Type: "pr"}, "my-branch")
	finished := runTaskCmd(t, cmd)

	require.Error(t, finished.Err)
	require.Contains(t, finished.Err.Error(), "no merge request found")
	require.False(t, openCalled, "must not attempt to open a browser when no MR was found")
}

func TestMergePR_Success(t *testing.T) {
	defer data.SetRESTClient(nil)

	var gotMethod, gotPath string
	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	pr := mockIssue{number: 42, repoName: "o/r"}

	cmd := MergePR(ctx, SectionIdentifier{Id: 1, Type: "pr"}, pr)
	finished := runTaskCmd(t, cmd)

	require.NoError(t, finished.Err)
	require.Equal(t, http.MethodPut, gotMethod)
	require.Contains(t, gotPath, "/merge_requests/42/merge")

	msg, ok := finished.Msg.(UpdatePRMsg)
	require.True(t, ok)
	require.NotNil(t, msg.IsMerged)
	require.True(t, *msg.IsMerged)
}

func TestMergePR_PropagatesAPIErrorAndReportsNotMerged(t *testing.T) {
	defer data.SetRESTClient(nil)

	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	pr := mockIssue{number: 42, repoName: "o/r"}

	cmd := MergePR(ctx, SectionIdentifier{Id: 1, Type: "pr"}, pr)
	finished := runTaskCmd(t, cmd)

	require.Error(t, finished.Err)

	msg, ok := finished.Msg.(UpdatePRMsg)
	require.True(t, ok)
	require.NotNil(t, msg.IsMerged)
	require.False(t, *msg.IsMerged)
}

func TestMergePR_TaskConfigurationForLargePRNumber(t *testing.T) {
	var capturedTask context.Task
	ctx := &context.ProgramContext{
		StartTask: func(task context.Task) tea.Cmd {
			capturedTask = task
			return nil
		},
	}
	pr := mockIssue{number: 999999, repoName: "o/r"}

	_ = MergePR(ctx, SectionIdentifier{Id: 1, Type: "pr"}, pr)

	require.Equal(t, "merge_999999", capturedTask.Id)
	require.Contains(t, capturedTask.StartText, "#999999")
	require.Contains(t, capturedTask.FinishedText, "#999999")
}

func TestCreatePR_UsesSourceAndResolvedTargetBranch(t *testing.T) {
	defer data.SetRESTClient(nil)

	var postBody map[string]any
	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && !strings.Contains(r.URL.Path, "/merge_requests"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"default_branch":"main"}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/merge_requests"):
			postBody = decodeJSONBody(t, r)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{
		StartTask: noopStartTask,
		RepoUrl:   "https://gitlab.example.com/o/r.git",
	}

	cmd := CreatePR(ctx, SectionIdentifier{Id: 1, Type: "pr"}, "feature-x", "My title")
	finished := runTaskCmd(t, cmd)

	require.NoError(t, finished.Err)
	require.Equal(t, "feature-x", postBody["source_branch"])
	require.Equal(t, "main", postBody["target_branch"])
	require.Equal(t, "My title", postBody["title"])

	msg, ok := finished.Msg.(UpdateBranchMsg)
	require.True(t, ok)
	require.Equal(t, "feature-x", msg.Name)
	require.NotNil(t, msg.IsCreated)
	require.True(t, *msg.IsCreated)
}

func TestCreatePR_APIFailure_ErrStaysNilButIsCreatedFalse(t *testing.T) {
	defer data.SetRESTClient(nil)

	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && !strings.Contains(r.URL.Path, "/merge_requests"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"default_branch":"main"}`))
		default:
			w.WriteHeader(http.StatusConflict)
		}
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{
		StartTask: noopStartTask,
		RepoUrl:   "https://gitlab.example.com/o/r.git",
	}

	cmd := CreatePR(ctx, SectionIdentifier{Id: 1, Type: "pr"}, "feature-x", "My title")
	finished := runTaskCmd(t, cmd)

	require.NoError(
		t,
		finished.Err,
		"CreatePR must always report Err=nil in TaskFinishedMsg, preserving pre-existing behavior",
	)

	msg, ok := finished.Msg.(UpdateBranchMsg)
	require.True(t, ok)
	require.NotNil(t, msg.IsCreated)
	require.False(t, *msg.IsCreated)
}

func TestCreatePR_DefaultBranchLookupFailure_IsCreatedFalse(t *testing.T) {
	defer data.SetRESTClient(nil)

	var createHits int
	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			createHits++
		}
		w.WriteHeader(http.StatusInternalServerError)
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{
		StartTask: noopStartTask,
		RepoUrl:   "https://gitlab.example.com/o/r.git",
	}

	cmd := CreatePR(ctx, SectionIdentifier{Id: 1, Type: "pr"}, "feature-x", "My title")
	finished := runTaskCmd(t, cmd)

	require.NoError(t, finished.Err)
	require.Equal(
		t,
		0,
		createHits,
		"must not attempt to create the MR when the default branch lookup fails",
	)

	msg, ok := finished.Msg.(UpdateBranchMsg)
	require.True(t, ok)
	require.NotNil(t, msg.IsCreated)
	require.False(t, *msg.IsCreated)
}

func TestCreatePR_TitleWithSpecialCharactersRoundTrips(t *testing.T) {
	defer data.SetRESTClient(nil)

	title := `Fix "quoted" bug & <edge> cases`
	var postBody map[string]any
	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && !strings.Contains(r.URL.Path, "/merge_requests"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"default_branch":"main"}`))
		case r.Method == http.MethodPost:
			postBody = decodeJSONBody(t, r)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{
		StartTask: noopStartTask,
		RepoUrl:   "https://gitlab.example.com/o/r.git",
	}

	cmd := CreatePR(ctx, SectionIdentifier{Id: 1, Type: "pr"}, "feature-x", title)
	finished := runTaskCmd(t, cmd)

	require.NoError(t, finished.Err)
	require.Equal(t, title, postBody["title"])
}
