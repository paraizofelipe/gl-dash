package tasks

import (
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

func TestUpdatePR_TaskConfiguration(t *testing.T) {
	section := SectionIdentifier{Id: 2, Type: "pr"}
	pr := mockIssue{
		number:   42,
		repoName: "owner/repo",
	}

	task := updatePRTask(section, pr)

	require.Equal(t, "pr_update_42", task.Id)
	require.Equal(t, []string{"pr", "update-branch", "42", "-R", "owner/repo"}, task.Args)
	require.Equal(t, section, task.Section)
	require.Equal(t, "Updating PR #42", task.StartText)
	require.Equal(t, "PR #42 has been updated", task.FinishedText)
}

func TestUpdatePR_MsgDoesNotMarkPRClosed(t *testing.T) {
	task := updatePRTask(SectionIdentifier{Id: 2, Type: "pr"}, mockIssue{
		number:   42,
		repoName: "owner/repo",
	})

	msg := task.Msg(nil, nil)
	updateMsg, ok := msg.(UpdatePRMsg)

	require.True(t, ok, "Msg should return UpdatePRMsg")
	require.Equal(t, 42, updateMsg.PrNumber)
	require.Nil(
		t,
		updateMsg.IsClosed,
		"updating a PR branch must not change the PR open/closed state",
	)
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

func runApproveWorkflowsCmd(t *testing.T, cmd tea.Cmd) constants.TaskFinishedMsg {
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

	t.Fatal("ApproveWorkflows command did not produce a constants.TaskFinishedMsg")
	return constants.TaskFinishedMsg{}
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
	finished := runApproveWorkflowsCmd(t, cmd)

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
	finished := runApproveWorkflowsCmd(t, cmd)

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
	finished := runApproveWorkflowsCmd(t, cmd)

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
	finished := runApproveWorkflowsCmd(t, cmd)

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
	finished := runApproveWorkflowsCmd(t, cmd)

	require.Error(t, finished.Err)
	require.Contains(t, finished.Err.Error(), "failed to locate pipeline")
}
