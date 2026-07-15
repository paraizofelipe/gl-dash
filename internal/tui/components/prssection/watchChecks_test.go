package prssection

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"
	gitlabapi "gitlab.com/gitlab-org/api/client-go"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prrow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/section"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/tasks"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
)

func newWatchChecksTestModel(startTask func(context.Task) tea.Cmd) Model {
	ctx := &context.ProgramContext{StartTask: startTask}
	return Model{
		BaseModel: section.BaseModel{
			Id:  3,
			Ctx: ctx,
		},
		Prs: []prrow.Data{
			{Primary: &data.PullRequestData{
				Number:     42,
				Title:      "Add feature X",
				Repository: data.Repository{NameWithOwner: "group/proj"},
			}},
		},
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

func extractTaskFinishedMsg(t *testing.T, cmd tea.Cmd) constants.TaskFinishedMsg {
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

func extractWatchPipelineResultMsg(t *testing.T, cmd tea.Cmd) watchPipelineResultMsg {
	t.Helper()
	require.NotNil(t, cmd)

	msg := cmd()
	if result, ok := msg.(watchPipelineResultMsg); ok {
		return result
	}

	batch, ok := msg.(tea.BatchMsg)
	require.True(t, ok, "expected watchPipelineResultMsg or tea.BatchMsg, got %T", msg)

	for _, sub := range batch {
		if sub == nil {
			continue
		}
		if result, ok := sub().(watchPipelineResultMsg); ok {
			return result
		}
	}

	t.Fatal("command did not produce a watchPipelineResultMsg")
	return watchPipelineResultMsg{}
}

func TestDecideWatchOutcome(t *testing.T) {
	fetchErr := errors.New("network unreachable")

	tests := []struct {
		name     string
		pipeline data.MergeRequestPipeline
		err      error
		want     watchOutcome
	}{
		{
			name:     "fetch error on a zero value pipeline is reported as an error",
			pipeline: data.MergeRequestPipeline{},
			err:      fetchErr,
			want:     watchOutcomeError,
		},
		{
			name:     "fetch error takes precedence even when the pipeline looks successful",
			pipeline: data.MergeRequestPipeline{ID: 5, Status: data.StatusSuccess},
			err:      fetchErr,
			want:     watchOutcomeError,
		},
		{
			name:     "mr without a pipeline yet is rescheduled",
			pipeline: data.MergeRequestPipeline{},
			err:      nil,
			want:     watchOutcomeReschedule,
		},
		{
			name:     "running pipeline status is rescheduled",
			pipeline: data.MergeRequestPipeline{ID: 5, Status: data.StatusRunning},
			err:      nil,
			want:     watchOutcomeReschedule,
		},
		{
			name:     "pending pipeline status is rescheduled",
			pipeline: data.MergeRequestPipeline{ID: 5, Status: data.StatusPending},
			err:      nil,
			want:     watchOutcomeReschedule,
		},
		{
			name:     "created pipeline status is rescheduled",
			pipeline: data.MergeRequestPipeline{ID: 5, Status: data.StatusCreated},
			err:      nil,
			want:     watchOutcomeReschedule,
		},
		{
			name:     "manual pipeline status is terminal since it is blocked awaiting a manual action and won't progress on its own",
			pipeline: data.MergeRequestPipeline{ID: 5, Status: data.StatusManual},
			err:      nil,
			want:     watchOutcomeManual,
		},
		{
			name:     "failed pipeline status is reported as a failure",
			pipeline: data.MergeRequestPipeline{ID: 5, Status: data.StatusFailed},
			err:      nil,
			want:     watchOutcomeFailure,
		},
		{
			name:     "success pipeline status is reported as a success",
			pipeline: data.MergeRequestPipeline{ID: 5, Status: data.StatusSuccess},
			err:      nil,
			want:     watchOutcomeSuccess,
		},
		{
			name:     "canceled pipeline status is reported as neutral",
			pipeline: data.MergeRequestPipeline{ID: 5, Status: data.StatusCanceled},
			err:      nil,
			want:     watchOutcomeNeutral,
		},
		{
			name:     "skipped pipeline status is reported as neutral",
			pipeline: data.MergeRequestPipeline{ID: 5, Status: data.StatusSkipped},
			err:      nil,
			want:     watchOutcomeNeutral,
		},
		{
			name: "an unrecognized pipeline status falls back to success",
			pipeline: data.MergeRequestPipeline{
				ID:     5,
				Status: data.PipelineStatus("some_future_status"),
			},
			err:  nil,
			want: watchOutcomeSuccess,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decideWatchOutcome(tt.pipeline, tt.err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestWatchChecks_NilCurrRow(t *testing.T) {
	ctx := &context.ProgramContext{
		StartTask: func(task context.Task) tea.Cmd {
			return func() tea.Msg { return nil }
		},
	}
	m := Model{
		BaseModel: section.BaseModel{Ctx: ctx},
		Prs:       []prrow.Data{},
	}

	var cmd tea.Cmd
	require.NotPanics(t, func() {
		cmd = m.watchChecks()
	})

	require.Nil(t, cmd)
}

func TestWatchChecks_StartsTaskWithCorrectConfiguration(t *testing.T) {
	var capturedTask context.Task
	m := newWatchChecksTestModel(func(task context.Task) tea.Cmd {
		capturedTask = task
		return nil
	})

	_ = m.watchChecks()

	require.Equal(t, "pr_watch_checks_42", capturedTask.Id)
	require.Equal(t, "Watching checks for MR #42", capturedTask.StartText)
	require.Equal(t, "Watching checks for MR #42", capturedTask.FinishedText)
	require.Equal(t, context.TaskStart, capturedTask.State)
	require.Nil(t, capturedTask.Error)
}

func TestWatchChecks_DispatchesImmediateFetchWithoutWaitingForTick(t *testing.T) {
	defer data.SetRESTClient(nil)

	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	})
	data.SetRESTClient(mockClient)

	m := newWatchChecksTestModel(func(task context.Task) tea.Cmd {
		return func() tea.Msg { return nil }
	})

	cmd := m.watchChecks()

	result := extractWatchPipelineResultMsg(t, cmd)
	require.Equal(t, "pr_watch_checks_42", result.taskId)
	require.Equal(t, m.Id, result.sectionId)
	require.Equal(t, "group/proj", result.repoNameWithOwner)
	require.Equal(t, 42, result.prNumber)
	require.Equal(t, "Add feature X", result.prTitle)
	require.NoError(t, result.err)
}

func TestFetchPipelineStatusCmd(t *testing.T) {
	t.Run(
		"packages the pipeline returned by FindPipelineForMR into a watchPipelineResultMsg",
		func(t *testing.T) {
			defer data.SetRESTClient(nil)

			mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(
					`[{"id":30,"status":"running","web_url":"https://gitlab.example.com/group/proj/-/pipelines/30"}]`,
				))
			})
			data.SetRESTClient(mockClient)

			cmd := fetchPipelineStatusCmd(
				"pr_watch_checks_42",
				3,
				"group/proj",
				42,
				"Add feature X",
			)
			require.NotNil(t, cmd)

			msg := cmd()
			result, ok := msg.(watchPipelineResultMsg)
			require.True(t, ok, "expected watchPipelineResultMsg, got %T", msg)

			require.Equal(t, "pr_watch_checks_42", result.taskId)
			require.Equal(t, 3, result.sectionId)
			require.Equal(t, "group/proj", result.repoNameWithOwner)
			require.Equal(t, 42, result.prNumber)
			require.Equal(t, "Add feature X", result.prTitle)
			require.NoError(t, result.err)
			require.EqualValues(t, 30, result.pipeline.ID)
			require.Equal(t, data.StatusRunning, result.pipeline.Status)
		},
	)

	t.Run("packages a FindPipelineForMR error into a watchPipelineResultMsg", func(t *testing.T) {
		defer data.SetRESTClient(nil)

		mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})
		data.SetRESTClient(mockClient)

		cmd := fetchPipelineStatusCmd("pr_watch_checks_42", 3, "group/proj", 42, "Add feature X")
		require.NotNil(t, cmd)

		msg := cmd()
		result, ok := msg.(watchPipelineResultMsg)
		require.True(t, ok, "expected watchPipelineResultMsg, got %T", msg)
		require.Error(t, result.err)
	})
}

func TestWatchPipelineTickCmd_ReturnsNonNilCommand(t *testing.T) {
	cmd := watchPipelineTickCmd("pr_watch_checks_42", 3, "group/proj", 42, "Add feature X")
	require.NotNil(t, cmd)
}

func TestOnWatchPipelineTickMsg_WrongSection(t *testing.T) {
	m := newWatchChecksTestModel(func(task context.Task) tea.Cmd { return nil })

	cmd := m.onWatchPipelineTickMsg(watchPipelineTickMsg{
		sectionId: m.Id + 1,
		taskId:    "pr_watch_checks_42",
		prNumber:  42,
	})

	require.Nil(t, cmd)
}

func TestOnWatchPipelineTickMsg_MatchingSectionRefetchesPipelineStatus(t *testing.T) {
	defer data.SetRESTClient(nil)

	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	})
	data.SetRESTClient(mockClient)

	m := newWatchChecksTestModel(func(task context.Task) tea.Cmd { return nil })

	cmd := m.onWatchPipelineTickMsg(watchPipelineTickMsg{
		sectionId:         m.Id,
		taskId:            "pr_watch_checks_42",
		repoNameWithOwner: "group/proj",
		prNumber:          42,
		prTitle:           "Add feature X",
	})

	result := extractWatchPipelineResultMsg(t, cmd)
	require.Equal(t, 42, result.prNumber)
	require.Equal(t, "group/proj", result.repoNameWithOwner)
}

func TestOnWatchPipelineResultMsg_WrongSection(t *testing.T) {
	m := newWatchChecksTestModel(func(task context.Task) tea.Cmd { return nil })

	cmd := m.onWatchPipelineResultMsg(watchPipelineResultMsg{
		sectionId: m.Id + 1,
		taskId:    "pr_watch_checks_42",
		prNumber:  42,
		pipeline:  data.MergeRequestPipeline{ID: 5, Status: data.StatusRunning},
	})

	require.Nil(t, cmd)
}

func TestOnWatchPipelineResultMsg_Reschedule(t *testing.T) {
	m := newWatchChecksTestModel(func(task context.Task) tea.Cmd { return nil })

	cmd := m.onWatchPipelineResultMsg(watchPipelineResultMsg{
		sectionId:         m.Id,
		taskId:            "pr_watch_checks_42",
		repoNameWithOwner: "group/proj",
		prNumber:          42,
		prTitle:           "Add feature X",
		pipeline:          data.MergeRequestPipeline{ID: 5, Status: data.StatusRunning},
	})

	require.NotNil(
		t,
		cmd,
		"a pending pipeline should delegate to watchPipelineTickCmd; not invoked here since it blocks for a real 5s tea.Tick",
	)
}

func TestOnWatchPipelineResultMsg_TerminalSuccess(t *testing.T) {
	m := newWatchChecksTestModel(func(task context.Task) tea.Cmd { return nil })

	cmd := m.onWatchPipelineResultMsg(watchPipelineResultMsg{
		sectionId:         m.Id,
		taskId:            "pr_watch_checks_42",
		repoNameWithOwner: "group/proj",
		prNumber:          42,
		prTitle:           "Add feature X",
		pipeline:          data.MergeRequestPipeline{ID: 5, Status: data.StatusSuccess},
	})

	finished := extractTaskFinishedMsg(t, cmd)
	require.Equal(t, "pr_watch_checks_42", finished.TaskId)
	require.Equal(t, m.Id, finished.SectionId)
	require.Equal(t, SectionType, finished.SectionType)
	require.NoError(t, finished.Err)

	updateMsg, ok := finished.Msg.(tasks.UpdatePRMsg)
	require.True(t, ok, "expected tasks.UpdatePRMsg, got %T", finished.Msg)
	require.Equal(t, 42, updateMsg.PrNumber)
}

func TestOnWatchPipelineResultMsg_TerminalFailure(t *testing.T) {
	m := newWatchChecksTestModel(func(task context.Task) tea.Cmd { return nil })

	cmd := m.onWatchPipelineResultMsg(watchPipelineResultMsg{
		sectionId:         m.Id,
		taskId:            "pr_watch_checks_42",
		repoNameWithOwner: "group/proj",
		prNumber:          42,
		prTitle:           "Add feature X",
		pipeline:          data.MergeRequestPipeline{ID: 5, Status: data.StatusFailed},
	})

	finished := extractTaskFinishedMsg(t, cmd)
	require.NoError(
		t,
		finished.Err,
		"a failed pipeline must not surface as an error on the watch task itself",
	)
}

func TestOnWatchPipelineResultMsg_TerminalNeutral(t *testing.T) {
	m := newWatchChecksTestModel(func(task context.Task) tea.Cmd { return nil })

	cmd := m.onWatchPipelineResultMsg(watchPipelineResultMsg{
		sectionId:         m.Id,
		taskId:            "pr_watch_checks_42",
		repoNameWithOwner: "group/proj",
		prNumber:          42,
		prTitle:           "Add feature X",
		pipeline:          data.MergeRequestPipeline{ID: 5, Status: data.StatusCanceled},
	})

	finished := extractTaskFinishedMsg(t, cmd)
	require.NoError(
		t,
		finished.Err,
		"a canceled pipeline must not surface as an error on the watch task itself",
	)

	updateMsg, ok := finished.Msg.(tasks.UpdatePRMsg)
	require.True(t, ok, "expected tasks.UpdatePRMsg, got %T", finished.Msg)
	require.Equal(t, 42, updateMsg.PrNumber)
}

func TestOnWatchPipelineResultMsg_TerminalManual(t *testing.T) {
	m := newWatchChecksTestModel(func(task context.Task) tea.Cmd { return nil })

	cmd := m.onWatchPipelineResultMsg(watchPipelineResultMsg{
		sectionId:         m.Id,
		taskId:            "pr_watch_checks_42",
		repoNameWithOwner: "group/proj",
		prNumber:          42,
		prTitle:           "Add feature X",
		pipeline:          data.MergeRequestPipeline{ID: 5, Status: data.StatusManual},
	})

	finished := extractTaskFinishedMsg(t, cmd)
	require.NoError(
		t,
		finished.Err,
		"a manual pipeline must terminate the watch (not reschedule) and must not surface an error",
	)

	updateMsg, ok := finished.Msg.(tasks.UpdatePRMsg)
	require.True(t, ok, "expected tasks.UpdatePRMsg, got %T", finished.Msg)
	require.Equal(t, 42, updateMsg.PrNumber)
}

func TestOnWatchPipelineResultMsg_FetchError(t *testing.T) {
	m := newWatchChecksTestModel(func(task context.Task) tea.Cmd { return nil })
	fetchErr := errors.New("network unreachable")

	cmd := m.onWatchPipelineResultMsg(watchPipelineResultMsg{
		sectionId: m.Id,
		taskId:    "pr_watch_checks_42",
		prNumber:  42,
		err:       fetchErr,
	})

	finished := extractTaskFinishedMsg(t, cmd)
	require.Error(t, finished.Err)
	require.ErrorIs(t, finished.Err, fetchErr)
}

func TestFinishWatchChecks(t *testing.T) {
	t.Run("wraps a nil error into a TaskFinishedMsg carrying the pr number", func(t *testing.T) {
		cmd := finishWatchChecks("pr_watch_checks_42", 3, 42, nil)

		finished := extractTaskFinishedMsg(t, cmd)
		require.Equal(t, "pr_watch_checks_42", finished.TaskId)
		require.Equal(t, 3, finished.SectionId)
		require.Equal(t, SectionType, finished.SectionType)
		require.NoError(t, finished.Err)

		updateMsg, ok := finished.Msg.(tasks.UpdatePRMsg)
		require.True(t, ok, "expected tasks.UpdatePRMsg, got %T", finished.Msg)
		require.Equal(t, 42, updateMsg.PrNumber)
	})

	t.Run("propagates a non-nil error into the TaskFinishedMsg", func(t *testing.T) {
		wantErr := errors.New("boom")
		cmd := finishWatchChecks("pr_watch_checks_7", 1, 7, wantErr)

		finished := extractTaskFinishedMsg(t, cmd)
		require.ErrorIs(t, finished.Err, wantErr)
	})
}
