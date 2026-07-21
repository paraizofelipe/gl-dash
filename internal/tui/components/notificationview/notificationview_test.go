package notificationview

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/notificationrow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prrow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

func TestSetPendingPRAction(t *testing.T) {
	tests := []struct {
		name           string
		action         string
		prNumber       int
		expectedAction string
		expectedPrompt string
	}{
		{
			name:           "close action",
			action:         "close",
			prNumber:       123,
			expectedAction: "pr_close",
			expectedPrompt: "Are you sure you want to close MR #123? (y/N)",
		},
		{
			name:           "reopen action",
			action:         "reopen",
			prNumber:       456,
			expectedAction: "pr_reopen",
			expectedPrompt: "Are you sure you want to reopen MR #456? (y/N)",
		},
		{
			name:           "ready action displays as mark as ready",
			action:         "ready",
			prNumber:       789,
			expectedAction: "pr_ready",
			expectedPrompt: "Are you sure you want to mark as ready MR #789? (y/N)",
		},
		{
			name:           "merge action",
			action:         "merge",
			prNumber:       100,
			expectedAction: "pr_merge",
			expectedPrompt: "Are you sure you want to merge MR #100? (y/N)",
		},
		{
			name:           "update action",
			action:         "update",
			prNumber:       200,
			expectedAction: "pr_update",
			expectedPrompt: "Are you sure you want to update MR #200? (y/N)",
		},
		{
			name:           "approveWorkflows action displays as approve all workflows for",
			action:         "approveWorkflows",
			prNumber:       300,
			expectedAction: "pr_approveWorkflows",
			expectedPrompt: "Are you sure you want to approve all workflows for MR #300? (y/N)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewModel(&context.ProgramContext{})
			m.SetSubjectPR(&prrow.Data{
				Primary: &data.PullRequestData{Number: tt.prNumber},
			}, "notif-id")

			prompt := m.SetPendingPRAction(tt.action)

			require.Equal(t, tt.expectedAction, m.GetPendingAction())
			require.Equal(t, tt.expectedPrompt, prompt)
			require.True(t, m.HasPendingAction())
		})
	}
}

func TestSetPendingPRAction_NilSubject(t *testing.T) {
	m := NewModel(&context.ProgramContext{})

	prompt := m.SetPendingPRAction("close")

	require.Empty(t, prompt, "should return empty prompt when no PR subject")
	require.Empty(t, m.GetPendingAction(), "should not set pending action")
	require.False(t, m.HasPendingAction())
}

func TestSetPendingIssueAction(t *testing.T) {
	tests := []struct {
		name           string
		action         string
		issueNumber    int
		expectedAction string
		expectedPrompt string
	}{
		{
			name:           "close action",
			action:         "close",
			issueNumber:    123,
			expectedAction: "issue_close",
			expectedPrompt: "Are you sure you want to close Issue #123? (y/N)",
		},
		{
			name:           "reopen action",
			action:         "reopen",
			issueNumber:    456,
			expectedAction: "issue_reopen",
			expectedPrompt: "Are you sure you want to reopen Issue #456? (y/N)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewModel(&context.ProgramContext{})
			m.SetSubjectIssue(&data.IssueData{Number: tt.issueNumber}, "notif-id")

			prompt := m.SetPendingIssueAction(tt.action)

			require.Equal(t, tt.expectedAction, m.GetPendingAction())
			require.Equal(t, tt.expectedPrompt, prompt)
			require.True(t, m.HasPendingAction())
		})
	}
}

func TestSetPendingIssueAction_NilSubject(t *testing.T) {
	m := NewModel(&context.ProgramContext{})

	prompt := m.SetPendingIssueAction("close")

	require.Empty(t, prompt, "should return empty prompt when no Issue subject")
	require.Empty(t, m.GetPendingAction(), "should not set pending action")
	require.False(t, m.HasPendingAction())
}

func TestClearPendingAction(t *testing.T) {
	m := NewModel(&context.ProgramContext{})
	m.SetSubjectPR(&prrow.Data{
		Primary: &data.PullRequestData{Number: 123},
	}, "notif-id")
	m.SetPendingPRAction("close")

	require.True(t, m.HasPendingAction(), "should have pending action before clear")

	m.ClearPendingAction()

	require.False(t, m.HasPendingAction(), "should not have pending action after clear")
	require.Empty(t, m.GetPendingAction())
}

func TestHasPendingAction(t *testing.T) {
	m := NewModel(&context.ProgramContext{})

	require.False(t, m.HasPendingAction(), "should be false initially")

	m.SetSubjectPR(&prrow.Data{
		Primary: &data.PullRequestData{Number: 123},
	}, "notif-id")
	m.SetPendingPRAction("merge")

	require.True(t, m.HasPendingAction(), "should be true after setting action")

	m.ClearPendingAction()

	require.False(t, m.HasPendingAction(), "should be false after clearing")
}

// Update method tests

func TestUpdate_NoPendingAction(t *testing.T) {
	// When there's no pending action, Update should return early with no action
	m := NewModel(&context.ProgramContext{})

	msg := tea.KeyPressMsg{Text: "y"}
	newModel, action := m.Update(msg)

	require.Empty(t, action, "should return empty action when no pending action")
	require.False(t, newModel.HasPendingAction())
}

func TestUpdate_ConfirmWithLowercaseY(t *testing.T) {
	m := NewModel(&context.ProgramContext{})
	m.SetSubjectPR(&prrow.Data{
		Primary: &data.PullRequestData{Number: 123},
	}, "notif-id")
	m.SetPendingPRAction("close")

	msg := tea.KeyPressMsg{Text: "y"}
	newModel, action := m.Update(msg)

	require.Equal(t, "pr_close", action, "should return the confirmed action")
	require.False(t, newModel.HasPendingAction(), "pending action should be cleared")
}

func TestUpdate_ConfirmWithUppercaseY(t *testing.T) {
	m := NewModel(&context.ProgramContext{})
	m.SetSubjectPR(&prrow.Data{
		Primary: &data.PullRequestData{Number: 456},
	}, "notif-id")
	m.SetPendingPRAction("merge")

	msg := tea.KeyPressMsg{Text: "Y"}
	newModel, action := m.Update(msg)

	require.Equal(t, "pr_merge", action, "should return the confirmed action")
	require.False(t, newModel.HasPendingAction(), "pending action should be cleared")
}

func TestUpdate_EnterDoesNotConfirm(t *testing.T) {
	m := NewModel(&context.ProgramContext{})
	m.SetSubjectIssue(&data.IssueData{Number: 789}, "notif-id")
	m.SetPendingIssueAction("reopen")

	msg := tea.KeyPressMsg{Code: tea.KeyEnter}
	newModel, action := m.Update(msg)

	require.Empty(t, action, "Enter should not confirm when default is No")
	require.False(t, newModel.HasPendingAction(), "pending action should be cleared")
}

func TestUpdate_CancelWithN(t *testing.T) {
	m := NewModel(&context.ProgramContext{})
	m.SetSubjectPR(&prrow.Data{
		Primary: &data.PullRequestData{Number: 123},
	}, "notif-id")
	m.SetPendingPRAction("close")

	msg := tea.KeyPressMsg{Text: "n"}
	newModel, action := m.Update(msg)

	require.Empty(t, action, "should return empty action on cancel")
	require.False(t, newModel.HasPendingAction(), "pending action should be cleared")
}

func TestUpdate_CancelWithEscape(t *testing.T) {
	m := NewModel(&context.ProgramContext{})
	m.SetSubjectPR(&prrow.Data{
		Primary: &data.PullRequestData{Number: 123},
	}, "notif-id")
	m.SetPendingPRAction("ready")

	msg := tea.KeyPressMsg{Code: tea.KeyEsc}
	newModel, action := m.Update(msg)

	require.Empty(t, action, "should return empty action on escape")
	require.False(t, newModel.HasPendingAction(), "pending action should be cleared")
}

func TestUpdate_CancelWithRandomKey(t *testing.T) {
	m := NewModel(&context.ProgramContext{})
	m.SetSubjectPR(&prrow.Data{
		Primary: &data.PullRequestData{Number: 123},
	}, "notif-id")
	m.SetPendingPRAction("update")

	// Press a random key like 'x'
	msg := tea.KeyPressMsg{Text: "x"}
	newModel, action := m.Update(msg)

	require.Empty(t, action, "should return empty action on random key")
	require.False(t, newModel.HasPendingAction(), "pending action should be cleared")
}

func TestUpdate_ConfirmReturnsAction(t *testing.T) {
	// Confirming should return the action string
	m := NewModel(&context.ProgramContext{})
	m.SetSubjectPR(&prrow.Data{
		Primary: &data.PullRequestData{Number: 123},
	}, "notif-id")
	m.SetPendingPRAction("close")

	msg := tea.KeyPressMsg{Text: "y"}
	newModel, action := m.Update(msg)

	require.Equal(t, "pr_close", action, "should return the confirmed action")
	require.False(t, newModel.HasPendingAction(), "pending action should be cleared")
}

func TestUpdate_NonKeyMsg(t *testing.T) {
	// Non-KeyMsg messages should be ignored
	m := NewModel(&context.ProgramContext{})
	m.SetSubjectPR(&prrow.Data{
		Primary: &data.PullRequestData{Number: 123},
	}, "notif-id")
	m.SetPendingPRAction("close")

	// Send a non-key message (e.g., WindowSizeMsg)
	msg := tea.WindowSizeMsg{Width: 100, Height: 50}
	newModel, action := m.Update(msg)

	require.Empty(t, action, "should return empty action for non-key messages")
	require.True(
		t,
		newModel.HasPendingAction(),
		"pending action should remain for non-key messages",
	)
}

func TestUpdate_AllPRActions(t *testing.T) {
	// Test that all PR action types work correctly
	actions := []string{"close", "reopen", "ready", "merge", "update", "approveWorkflows"}

	for _, action := range actions {
		t.Run(action, func(t *testing.T) {
			m := NewModel(&context.ProgramContext{})
			m.SetSubjectPR(&prrow.Data{
				Primary: &data.PullRequestData{Number: 123},
			}, "notif-id")
			m.SetPendingPRAction(action)

			msg := tea.KeyPressMsg{Text: "y"}
			newModel, confirmedAction := m.Update(msg)

			require.Equal(t, "pr_"+action, confirmedAction)
			require.False(t, newModel.HasPendingAction())
		})
	}
}

func TestUpdate_AllIssueActions(t *testing.T) {
	// Test that all Issue action types work correctly
	actions := []string{"close", "reopen"}

	for _, action := range actions {
		t.Run(action, func(t *testing.T) {
			m := NewModel(&context.ProgramContext{})
			m.SetSubjectIssue(&data.IssueData{Number: 456}, "notif-id")
			m.SetPendingIssueAction(action)

			msg := tea.KeyPressMsg{Text: "y"}
			newModel, confirmedAction := m.Update(msg)

			require.Equal(t, "issue_"+action, confirmedAction)
			require.False(t, newModel.HasPendingAction())
		})
	}
}

func TestUpdate_ReturnsActionOnConfirm(t *testing.T) {
	m := NewModel(&context.ProgramContext{})
	m.SetSubjectPR(&prrow.Data{
		Primary: &data.PullRequestData{Number: 123},
	}, "notif-id")
	m.SetPendingPRAction("close")

	msg := tea.KeyPressMsg{Text: "y"}
	_, action := m.Update(msg)

	require.Equal(t, "pr_close", action, "should return the action on confirm")
}

func TestGetTypeIconMergeRequestMatchesPullRequest(t *testing.T) {
	pullRequestIcon := getTypeIcon("PullRequest")
	mergeRequestIcon := getTypeIcon("MergeRequest")

	require.Equal(
		t,
		pullRequestIcon,
		mergeRequestIcon,
		"getTypeIcon(\"MergeRequest\") should render the same icon as getTypeIcon(\"PullRequest\")",
	)
}

func TestFormatReason(t *testing.T) {
	tests := []struct {
		name     string
		reason   string
		expected string
	}{
		{name: "subscribed", reason: "subscribed", expected: "Subscribed"},
		{name: "review_requested", reason: "review_requested", expected: "Review requested"},
		{name: "author", reason: "author", expected: "Author"},
		{name: "comment", reason: "comment", expected: "Comment"},
		{name: "mention", reason: "mention", expected: "Mentioned"},
		{name: "team_mention", reason: "team_mention", expected: "Team mentioned"},
		{name: "state_change", reason: "state_change", expected: "State changed"},
		{name: "assign", reason: "assign", expected: "Assigned"},
		{name: "ci_activity", reason: "ci_activity", expected: "CI activity"},
		{
			name:     "approval_requested is not a real GitLab action and falls back to the raw value",
			reason:   "approval_requested",
			expected: "approval_requested",
		},
		{name: "assigned", reason: "assigned", expected: "Assigned"},
		{name: "mentioned", reason: "mentioned", expected: "Mentioned"},
		{name: "build_failed", reason: "build_failed", expected: "Build failed"},
		{name: "marked", reason: "marked", expected: "Marked"},
		{
			name:     "approval_required distinct from approval_requested",
			reason:   "approval_required",
			expected: "Approval required",
		},
		{name: "directly_addressed", reason: "directly_addressed", expected: "Directly addressed"},
		{name: "unmergeable", reason: "unmergeable", expected: "Cannot be merged"},
		{
			name:     "unknown reason echoes the raw value",
			reason:   "some_unknown_reason",
			expected: "some_unknown_reason",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatReason(tt.reason)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestNoPanicOnUnknownGitLabValues(t *testing.T) {
	unknownSubjectTypeCases := []struct {
		name        string
		subjectType string
	}{
		{name: "unknown GitLab target type", subjectType: "AlertManagement::Alert"},
		{name: "unknown short value", subjectType: "unmergeable"},
		{name: "empty subject type", subjectType: ""},
	}

	for _, tt := range unknownSubjectTypeCases {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("getTypeIcon() panicked for subject type %q: %v", tt.subjectType, r)
				}
			}()
			_ = getTypeIcon(tt.subjectType)
		})
	}

	unknownReasonCases := []struct {
		name   string
		reason string
	}{
		{name: "unknown GitLab reason", reason: "some_future_action"},
		{name: "unknown namespaced value", reason: "AlertManagement::Alert"},
		{name: "empty reason", reason: ""},
	}

	for _, tt := range unknownReasonCases {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("formatReason() panicked for reason %q: %v", tt.reason, r)
				}
			}()
			result := formatReason(tt.reason)
			require.Equal(
				t,
				tt.reason,
				result,
				"formatReason(%q) should echo the raw reason back",
				tt.reason,
			)
		})
	}
}

func newViewTestContext() *context.ProgramContext {
	thm := *theme.DefaultTheme
	return &context.ProgramContext{
		Theme:  thm,
		Styles: context.InitStyles(thm),
	}
}

func TestViewHasCommentSectionVisibility(t *testing.T) {
	tests := []struct {
		name             string
		latestCommentUrl string
		expectSection    bool
	}{
		{
			name:             "empty latest comment url hides the has comment section entirely",
			latestCommentUrl: "",
			expectSection:    false,
		},
		{
			name:             "non empty latest comment url shows the has comment section as yes",
			latestCommentUrl: "https://gitlab.com/group/proj/-/merge_requests/9#note_1",
			expectSection:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewModel(newViewTestContext())
			m.SetWidth(80)
			m.SetRow(&notificationrow.Data{
				Notification: data.NotificationData{
					Subject: data.NotificationSubject{
						Title:            "Some title",
						Type:             "MergeRequest",
						LatestCommentUrl: tt.latestCommentUrl,
					},
					Repository: data.NotificationRepository{FullName: "group/proj"},
				},
			})

			view := m.View()

			if tt.expectSection {
				require.Contains(t, view, "Has Comment")
				require.Contains(t, view, "Yes")
			} else {
				require.NotContains(t, view, "Has Comment")
			}
		})
	}
}

func TestViewUrlLabelReplacesApiUrl(t *testing.T) {
	m := NewModel(newViewTestContext())
	m.SetWidth(80)
	subjectUrl := "https://gitlab.com/group/proj/-/merge_requests/9"
	m.SetRow(&notificationrow.Data{
		Notification: data.NotificationData{
			Subject: data.NotificationSubject{
				Title: "Some title",
				Type:  "MergeRequest",
				Url:   subjectUrl,
			},
			Repository: data.NotificationRepository{FullName: "group/proj"},
		},
	})

	view := m.View()

	require.NotContains(t, view, "API URL")
	require.Contains(t, view, subjectUrl)
}
