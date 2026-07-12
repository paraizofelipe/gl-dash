package tasks

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
)

// mockIssue implements data.RowData for testing
type mockIssue struct {
	number    int
	repoName  string
	title     string
	url       string
	updatedAt time.Time
}

func (m mockIssue) GetNumber() int               { return m.number }
func (m mockIssue) GetRepoNameWithOwner() string { return m.repoName }
func (m mockIssue) GetTitle() string             { return m.title }
func (m mockIssue) GetUrl() string               { return m.url }
func (m mockIssue) GetUpdatedAt() time.Time      { return m.updatedAt }

// noopStartTask is a stub that returns nil for testing
func noopStartTask(task context.Task) tea.Cmd {
	return nil
}

func TestUpdateIssueMsg_Fields(t *testing.T) {
	t.Run("all fields can be set", func(t *testing.T) {
		isClosed := true
		labels := &data.IssueLabels{Nodes: []data.Label{{Name: "bug", Color: "ff0000"}}}
		comment := &data.IssueComment{Body: "test comment"}
		addedAssignees := &data.Assignees{Nodes: []data.Assignee{{Login: "user1"}}}
		removedAssignees := &data.Assignees{Nodes: []data.Assignee{{Login: "user2"}}}

		msg := UpdateIssueMsg{
			IssueNumber:      123,
			Labels:           labels,
			NewComment:       comment,
			IsClosed:         &isClosed,
			AddedAssignees:   addedAssignees,
			RemovedAssignees: removedAssignees,
		}

		require.Equal(t, 123, msg.IssueNumber)
		require.Equal(t, labels, msg.Labels)
		require.Equal(t, comment, msg.NewComment)
		require.NotNil(t, msg.IsClosed)
		require.True(t, *msg.IsClosed)
		require.Equal(t, addedAssignees, msg.AddedAssignees)
		require.Equal(t, removedAssignees, msg.RemovedAssignees)
	})

	t.Run("nil pointer fields are valid", func(t *testing.T) {
		msg := UpdateIssueMsg{
			IssueNumber: 456,
		}

		require.Equal(t, 456, msg.IssueNumber)
		require.Nil(t, msg.Labels)
		require.Nil(t, msg.NewComment)
		require.Nil(t, msg.IsClosed)
		require.Nil(t, msg.AddedAssignees)
		require.Nil(t, msg.RemovedAssignees)
	})
}

func TestUpdateIssueMsg_ImplementsTeaMsg(t *testing.T) {
	var msg tea.Msg = UpdateIssueMsg{IssueNumber: 1}

	_, ok := msg.(UpdateIssueMsg)
	require.True(t, ok, "UpdateIssueMsg should be usable as tea.Msg")
}

func TestCloseIssue_TaskConfiguration(t *testing.T) {
	var capturedTask context.Task

	ctx := &context.ProgramContext{
		StartTask: func(task context.Task) tea.Cmd {
			capturedTask = task
			return nil
		},
	}
	section := SectionIdentifier{Id: 5, Type: "issue"}
	issue := mockIssue{
		number:   42,
		repoName: "test/repo",
	}

	_ = CloseIssue(ctx, section, issue)

	require.Equal(t, "issue_close_42", capturedTask.Id)
	require.Equal(t, "Closing issue #42", capturedTask.StartText)
	require.Equal(t, "Issue #42 has been closed", capturedTask.FinishedText)
	require.Equal(t, context.TaskStart, capturedTask.State)
	require.Nil(t, capturedTask.Error)
}

func TestCloseIssue_SectionIdentifierPropagation(t *testing.T) {
	tests := []struct {
		name        string
		sectionId   int
		sectionType string
	}{
		{name: "issue section type", sectionId: 1, sectionType: "issue"},
		{name: "notification section type", sectionId: 10, sectionType: "notification"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &context.ProgramContext{StartTask: noopStartTask}
			section := SectionIdentifier{Id: tt.sectionId, Type: tt.sectionType}
			issue := mockIssue{number: 1, repoName: "o/r"}

			cmd := CloseIssue(ctx, section, issue)

			require.NotNil(t, cmd)
		})
	}
}

func TestCloseIssue_UsesCorrectIssueNumber(t *testing.T) {
	issueNumbers := []int{1, 100, 12345, 999999}

	for _, num := range issueNumbers {
		t.Run(fmt.Sprintf("issue_%d", num), func(t *testing.T) {
			var capturedTask context.Task
			ctx := &context.ProgramContext{
				StartTask: func(task context.Task) tea.Cmd {
					capturedTask = task
					return nil
				},
			}
			issue := mockIssue{number: num, repoName: "o/r"}

			CloseIssue(ctx, SectionIdentifier{}, issue)

			expectedId := fmt.Sprintf("issue_close_%d", num)
			require.Equal(t, expectedId, capturedTask.Id)
			require.Contains(t, capturedTask.StartText, fmt.Sprintf("#%d", num))
		})
	}
}

func TestCloseIssue_Success(t *testing.T) {
	defer data.SetRESTClient(nil)

	var gotMethod string
	var gotBody map[string]any
	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotBody = decodeJSONBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":1}`))
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	issue := mockIssue{number: 42, repoName: "o/r"}

	cmd := CloseIssue(ctx, SectionIdentifier{Id: 1, Type: "issue"}, issue)
	finished := runTaskCmd(t, cmd)

	require.NoError(t, finished.Err)
	require.Equal(t, http.MethodPut, gotMethod)
	require.Equal(t, "close", gotBody["state_event"])

	msg, ok := finished.Msg.(UpdateIssueMsg)
	require.True(t, ok)
	require.Equal(t, 42, msg.IssueNumber)
	require.NotNil(t, msg.IsClosed)
	require.True(t, *msg.IsClosed)
}

func TestCloseIssue_PropagatesAPIError(t *testing.T) {
	defer data.SetRESTClient(nil)

	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	issue := mockIssue{number: 42, repoName: "o/r"}

	cmd := CloseIssue(ctx, SectionIdentifier{Id: 1, Type: "issue"}, issue)
	finished := runTaskCmd(t, cmd)

	require.Error(t, finished.Err)
}

func TestReopenIssue_TaskConfiguration(t *testing.T) {
	var capturedTask context.Task

	ctx := &context.ProgramContext{
		StartTask: func(task context.Task) tea.Cmd {
			capturedTask = task
			return nil
		},
	}
	section := SectionIdentifier{Id: 3, Type: "issue"}
	issue := mockIssue{
		number:   99,
		repoName: "example/project",
	}

	_ = ReopenIssue(ctx, section, issue)

	require.Equal(t, "issue_reopen_99", capturedTask.Id)
	require.Equal(t, "Reopening issue #99", capturedTask.StartText)
	require.Equal(t, "Issue #99 has been reopened", capturedTask.FinishedText)
	require.Equal(t, context.TaskStart, capturedTask.State)
	require.Nil(t, capturedTask.Error)
}

func TestReopenIssue_SectionIdentifierPropagation(t *testing.T) {
	tests := []struct {
		name        string
		sectionId   int
		sectionType string
	}{
		{name: "issue section type", sectionId: 1, sectionType: "issue"},
		{name: "notification section type", sectionId: 10, sectionType: "notification"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &context.ProgramContext{StartTask: noopStartTask}
			section := SectionIdentifier{Id: tt.sectionId, Type: tt.sectionType}
			issue := mockIssue{number: 1, repoName: "o/r"}

			cmd := ReopenIssue(ctx, section, issue)

			require.NotNil(t, cmd)
		})
	}
}

func TestReopenIssue_Success(t *testing.T) {
	defer data.SetRESTClient(nil)

	var gotMethod string
	var gotBody map[string]any
	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotBody = decodeJSONBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":1}`))
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	issue := mockIssue{number: 42, repoName: "o/r"}

	cmd := ReopenIssue(ctx, SectionIdentifier{Id: 1, Type: "issue"}, issue)
	finished := runTaskCmd(t, cmd)

	require.NoError(t, finished.Err)
	require.Equal(t, http.MethodPut, gotMethod)
	require.Equal(t, "reopen", gotBody["state_event"])

	msg, ok := finished.Msg.(UpdateIssueMsg)
	require.True(t, ok)
	require.Equal(t, 42, msg.IssueNumber)
	require.NotNil(t, msg.IsClosed)
	require.False(t, *msg.IsClosed)
}

func TestReopenIssue_PropagatesAPIError(t *testing.T) {
	defer data.SetRESTClient(nil)

	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	issue := mockIssue{number: 42, repoName: "o/r"}

	cmd := ReopenIssue(ctx, SectionIdentifier{Id: 1, Type: "issue"}, issue)
	finished := runTaskCmd(t, cmd)

	require.Error(t, finished.Err)
}

func TestAssignIssue_PreservesExistingAssigneesUnion(t *testing.T) {
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
			_, _ = w.Write([]byte(`{"id":1,"assignees":[{"id":10,"username":"alice"}]}`))
		case r.Method == http.MethodPut:
			putBody = decodeJSONBody(t, r)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":1}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	issue := mockIssue{number: 42, repoName: "o/r"}

	cmd := AssignIssue(ctx, SectionIdentifier{Id: 1, Type: "issue"}, issue, []string{"bob"})
	finished := runTaskCmd(t, cmd)

	require.NoError(t, finished.Err)
	require.NotNil(t, putBody)
	require.Equal(t, []int64{10, 20}, toInt64Slice(t, putBody["assignee_ids"]))

	msg, ok := finished.Msg.(UpdateIssueMsg)
	require.True(t, ok)
	require.NotNil(t, msg.AddedAssignees)
	require.Equal(t, []data.Assignee{{Login: "bob"}}, msg.AddedAssignees.Nodes)
}

func TestAssignIssue_UserNotFound_ReturnsError(t *testing.T) {
	defer data.SetRESTClient(nil)

	var issueEndpointHits int
	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/users") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
			return
		}
		issueEndpointHits++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	issue := mockIssue{number: 42, repoName: "o/r"}

	cmd := AssignIssue(ctx, SectionIdentifier{Id: 1, Type: "issue"}, issue, []string{"ghost"})
	finished := runTaskCmd(t, cmd)

	require.Error(t, finished.Err)
	require.Contains(t, finished.Err.Error(), "gitlab user not found: ghost")
	require.Equal(
		t,
		0,
		issueEndpointHits,
		"should fail resolving usernames before ever touching the issue",
	)
}

func TestAssignIssue_EmptyUsernames_ProducesEmptyAssigneeIDsPayload(t *testing.T) {
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
			_, _ = w.Write([]byte(`{"id":1,"assignees":[]}`))
		case r.Method == http.MethodPut:
			putBody = decodeJSONBody(t, r)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":1}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	issue := mockIssue{number: 42, repoName: "o/r"}

	cmd := AssignIssue(ctx, SectionIdentifier{Id: 1, Type: "issue"}, issue, []string{})
	finished := runTaskCmd(t, cmd)

	require.NoError(t, finished.Err)
	require.Equal(t, 0, usersEndpointHits)
	require.NotNil(t, putBody)
	require.Equal(t, []int64{}, toInt64Slice(t, putBody["assignee_ids"]))
}

func TestUnassignIssue_RemovesFromExistingAssignees(t *testing.T) {
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
				[]byte(
					`{"id":1,"assignees":[{"id":10,"username":"alice"},{"id":20,"username":"bob"}]}`,
				),
			)
		case r.Method == http.MethodPut:
			putBody = decodeJSONBody(t, r)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":1}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	issue := mockIssue{number: 42, repoName: "o/r"}

	cmd := UnassignIssue(ctx, SectionIdentifier{Id: 1, Type: "issue"}, issue, []string{"bob"})
	finished := runTaskCmd(t, cmd)

	require.NoError(t, finished.Err)
	require.NotNil(t, putBody)
	require.Equal(t, []int64{10}, toInt64Slice(t, putBody["assignee_ids"]))

	msg, ok := finished.Msg.(UpdateIssueMsg)
	require.True(t, ok)
	require.NotNil(t, msg.RemovedAssignees)
	require.Equal(t, []data.Assignee{{Login: "bob"}}, msg.RemovedAssignees.Nodes)
}

func TestUnassignIssue_PropagatesAPIError(t *testing.T) {
	defer data.SetRESTClient(nil)

	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	issue := mockIssue{number: 42, repoName: "o/r"}

	cmd := UnassignIssue(ctx, SectionIdentifier{Id: 1, Type: "issue"}, issue, []string{"bob"})
	finished := runTaskCmd(t, cmd)

	require.Error(t, finished.Err)
}

func TestCommentOnIssue_Success(t *testing.T) {
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
	issue := mockIssue{number: 42, repoName: "o/r"}

	cmd := CommentOnIssue(ctx, SectionIdentifier{Id: 1, Type: "issue"}, issue, "nice catch")
	finished := runTaskCmd(t, cmd)

	require.NoError(t, finished.Err)
	require.Equal(t, "nice catch", postBody["body"])

	msg, ok := finished.Msg.(UpdateIssueMsg)
	require.True(t, ok)
	require.NotNil(t, msg.NewComment)
	require.Equal(t, "nice catch", msg.NewComment.Body)
	require.Equal(t, "tester", msg.NewComment.Author.Login)
}

func TestCommentOnIssue_PropagatesAPIError(t *testing.T) {
	defer data.SetRESTClient(nil)

	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask, User: "tester"}
	issue := mockIssue{number: 42, repoName: "o/r"}

	cmd := CommentOnIssue(ctx, SectionIdentifier{Id: 1, Type: "issue"}, issue, "nice catch")
	finished := runTaskCmd(t, cmd)

	require.Error(t, finished.Err)
}

func TestLabelIssue_RequestPayloadAndMsg(t *testing.T) {
	defer data.SetRESTClient(nil)

	var gotMethod string
	var putBody map[string]any
	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		putBody = decodeJSONBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":1}`))
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	issue := mockIssue{number: 42, repoName: "o/r"}
	existingLabels := []data.Label{
		{Name: "bug", Color: "ff0000"},
		{Name: "stale", Color: "cccccc"},
	}

	cmd := LabelIssue(
		ctx,
		SectionIdentifier{Id: 1, Type: "issue"},
		issue,
		[]string{"bug", "urgent"},
		existingLabels,
	)
	finished := runTaskCmd(t, cmd)

	require.NoError(t, finished.Err)
	require.Equal(t, http.MethodPut, gotMethod)
	require.Equal(t, "bug,urgent", putBody["add_labels"])
	require.Equal(t, "stale", putBody["remove_labels"])

	msg, ok := finished.Msg.(UpdateIssueMsg)
	require.True(t, ok)
	require.NotNil(t, msg.Labels)
	require.Equal(t, []data.Label{
		{Name: "bug", Color: "ff0000"},
		{Name: "urgent", Color: ""},
	}, msg.Labels.Nodes)
}

func TestLabelIssue_PropagatesAPIError(t *testing.T) {
	defer data.SetRESTClient(nil)

	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	data.SetRESTClient(mockClient)

	ctx := &context.ProgramContext{StartTask: noopStartTask}
	issue := mockIssue{number: 42, repoName: "o/r"}

	cmd := LabelIssue(
		ctx,
		SectionIdentifier{Id: 1, Type: "issue"},
		issue,
		[]string{"bug"},
		nil,
	)
	finished := runTaskCmd(t, cmd)

	require.Error(t, finished.Err)
}
