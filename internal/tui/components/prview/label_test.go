package prview

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"
	gitlabapi "gitlab.com/gitlab-org/api/client-go"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prrow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/tasks"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

func newTestModelForLabel(t *testing.T, existingLabels []data.Label) Model {
	t.Helper()
	cfg, err := config.ParseConfig(config.Location{
		ConfigFlag:       "../../../config/testdata/test-config.yml",
		SkipGlobalConfig: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	thm := theme.ParseTheme(&cfg)
	ctx := &context.ProgramContext{
		Config:    &cfg,
		Theme:     thm,
		Styles:    context.InitStyles(thm),
		StartTask: func(task context.Task) tea.Cmd { return nil },
	}

	m := NewModel(ctx)
	m.ctx = ctx
	m.sectionId = 7
	m.pr = &prrow.PullRequest{
		Ctx: ctx,
		Data: &prrow.Data{
			Primary: &data.PullRequestData{
				Number:     42,
				Repository: data.Repository{NameWithOwner: "o/r"},
				Labels:     data.PRLabels{Nodes: existingLabels},
			},
			IsEnriched: true,
		},
	}
	return m
}

func newLabelTestRESTClient(t *testing.T, handler http.HandlerFunc) *gitlabapi.Client {
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

func decodeJSONBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
	return body
}

func runLabelCmd(t *testing.T, cmd tea.Cmd) constants.TaskFinishedMsg {
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

	t.Fatal("label command did not produce a constants.TaskFinishedMsg")
	return constants.TaskFinishedMsg{}
}

func TestLabel_AddsAndRemovesBasedOnDiffAgainstExistingLabels(t *testing.T) {
	defer data.SetRESTClient(nil)

	m := newTestModelForLabel(t, []data.Label{
		{Name: "bug", Color: "d73a4a"},
		{Name: "keep-me", Color: "00ff00"},
	})

	var gotBody map[string]any
	mockClient := newLabelTestRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPut, r.Method)
		require.Contains(t, r.URL.Path, "/merge_requests/42")
		gotBody = decodeJSONBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})
	data.SetRESTClient(mockClient)

	cmd := m.label([]string{"keep-me", "feature"})
	finished := runLabelCmd(t, cmd)

	require.NoError(t, finished.Err)
	require.Equal(t, "keep-me,feature", gotBody["add_labels"])
	require.Equal(t, "bug", gotBody["remove_labels"])
}

func TestLabel_UnchangedLabelsStillCallsAPIWithNoRemovals(t *testing.T) {
	defer data.SetRESTClient(nil)

	m := newTestModelForLabel(t, []data.Label{
		{Name: "bug", Color: "d73a4a"},
		{Name: "feature", Color: "00ff00"},
	})

	var gotBody map[string]any
	var apiCalls int
	mockClient := newLabelTestRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		apiCalls++
		gotBody = decodeJSONBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})
	data.SetRESTClient(mockClient)

	cmd := m.label([]string{"bug", "feature"})
	finished := runLabelCmd(t, cmd)

	require.NoError(t, finished.Err)
	require.Equal(t, 1, apiCalls, "the API call must still happen even when nothing changed")
	require.Equal(t, "bug,feature", gotBody["add_labels"])
	require.Nil(
		t,
		gotBody["remove_labels"],
		"nothing needs removing when the desired labels match the existing ones",
	)
}

func TestLabel_APIErrorPropagatesButLabelsMsgStillPopulated(t *testing.T) {
	defer data.SetRESTClient(nil)

	m := newTestModelForLabel(t, []data.Label{{Name: "bug", Color: "d73a4a"}})

	mockClient := newLabelTestRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	data.SetRESTClient(mockClient)

	cmd := m.label([]string{"feature"})
	finished := runLabelCmd(t, cmd)

	require.Error(t, finished.Err)

	msg, ok := finished.Msg.(tasks.UpdatePRMsg)
	require.True(t, ok)
	require.NotNil(t, msg.Labels)
	require.Equal(t, []data.Label{{Name: "feature", Color: ""}}, msg.Labels.Nodes)
}

func TestLabel_NewLabelGetsEmptyColorWhenNotPreviouslyOnPR(t *testing.T) {
	defer data.SetRESTClient(nil)

	m := newTestModelForLabel(t, []data.Label{{Name: "bug", Color: "d73a4a"}})

	mockClient := newLabelTestRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})
	data.SetRESTClient(mockClient)

	cmd := m.label([]string{"bug", "brand-new"})
	finished := runLabelCmd(t, cmd)

	require.NoError(t, finished.Err)

	msg, ok := finished.Msg.(tasks.UpdatePRMsg)
	require.True(t, ok)
	require.Equal(t, 42, msg.PrNumber)
	require.Equal(t, []data.Label{
		{Name: "bug", Color: "d73a4a"},
		{Name: "brand-new", Color: ""},
	}, msg.Labels.Nodes)
}

func TestLabel_TaskIdentifiersIncludePRNumber(t *testing.T) {
	var capturedTask context.Task
	m := newTestModelForLabel(t, nil)
	m.ctx.StartTask = func(task context.Task) tea.Cmd {
		capturedTask = task
		return nil
	}

	_ = m.label([]string{"feature"})

	require.Equal(t, "pr_label_42", capturedTask.Id)
	require.Contains(t, capturedTask.StartText, "#42")
	require.Contains(t, capturedTask.FinishedText, "#42")
}
