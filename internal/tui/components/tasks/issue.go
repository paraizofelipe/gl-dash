package tasks

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/utils"
)

type UpdateIssueMsg struct {
	IssueNumber      int
	Labels           *data.IssueLabels
	NewComment       *data.IssueComment
	IsClosed         *bool
	AddedAssignees   *data.Assignees
	RemovedAssignees *data.Assignees
}

func CloseIssue(
	ctx *context.ProgramContext,
	section SectionIdentifier,
	issue data.RowData,
) tea.Cmd {
	issueNumber := issue.GetNumber()
	return runMRAction(
		ctx, section,
		fmt.Sprintf("issue_close_%d", issueNumber),
		fmt.Sprintf("Closing issue #%d", issueNumber),
		fmt.Sprintf("Issue #%d has been closed", issueNumber),
		func() (tea.Msg, error) {
			err := data.CloseIssue(issue.GetRepoNameWithOwner(), issueNumber)
			return UpdateIssueMsg{
				IssueNumber: issueNumber,
				IsClosed:    utils.BoolPtr(true),
			}, err
		},
	)
}

func ReopenIssue(
	ctx *context.ProgramContext,
	section SectionIdentifier,
	issue data.RowData,
) tea.Cmd {
	issueNumber := issue.GetNumber()
	return runMRAction(
		ctx, section,
		fmt.Sprintf("issue_reopen_%d", issueNumber),
		fmt.Sprintf("Reopening issue #%d", issueNumber),
		fmt.Sprintf("Issue #%d has been reopened", issueNumber),
		func() (tea.Msg, error) {
			err := data.ReopenIssue(issue.GetRepoNameWithOwner(), issueNumber)
			return UpdateIssueMsg{
				IssueNumber: issueNumber,
				IsClosed:    utils.BoolPtr(false),
			}, err
		},
	)
}

func AssignIssue(
	ctx *context.ProgramContext,
	section SectionIdentifier,
	issue data.RowData,
	usernames []string,
) tea.Cmd {
	issueNumber := issue.GetNumber()
	return runMRAction(
		ctx, section,
		fmt.Sprintf("issue_assign_%d", issueNumber),
		fmt.Sprintf("Assigning issue #%d to %s", issueNumber, usernames),
		fmt.Sprintf("Issue #%d has been assigned to %s", issueNumber, usernames),
		func() (tea.Msg, error) {
			err := data.AddIssueAssignees(issue.GetRepoNameWithOwner(), issueNumber, usernames)
			returnedAssignees := toAssignees(usernames)
			return UpdateIssueMsg{
				IssueNumber:    issueNumber,
				AddedAssignees: &returnedAssignees,
			}, err
		},
	)
}

func UnassignIssue(
	ctx *context.ProgramContext,
	section SectionIdentifier,
	issue data.RowData,
	usernames []string,
) tea.Cmd {
	issueNumber := issue.GetNumber()
	return runMRAction(
		ctx, section,
		fmt.Sprintf("issue_unassign_%d", issueNumber),
		fmt.Sprintf("Unassigning %s from issue #%d", usernames, issueNumber),
		fmt.Sprintf("%s unassigned from issue #%d", usernames, issueNumber),
		func() (tea.Msg, error) {
			err := data.RemoveIssueAssignees(issue.GetRepoNameWithOwner(), issueNumber, usernames)
			returnedAssignees := toAssignees(usernames)
			return UpdateIssueMsg{
				IssueNumber:      issueNumber,
				RemovedAssignees: &returnedAssignees,
			}, err
		},
	)
}

func CommentOnIssue(
	ctx *context.ProgramContext,
	section SectionIdentifier,
	issue data.RowData,
	body string,
) tea.Cmd {
	issueNumber := issue.GetNumber()
	return runMRAction(
		ctx, section,
		fmt.Sprintf("issue_comment_%d", issueNumber),
		fmt.Sprintf("Commenting on issue #%d", issueNumber),
		fmt.Sprintf("Commented on issue #%d", issueNumber),
		func() (tea.Msg, error) {
			err := data.CommentOnIssue(issue.GetRepoNameWithOwner(), issueNumber, body)
			return UpdateIssueMsg{
				IssueNumber: issueNumber,
				NewComment: &data.IssueComment{
					Author:    struct{ Login string }{Login: ctx.User},
					Body:      body,
					UpdatedAt: time.Now(),
				},
			}, err
		},
	)
}

func LabelIssue(
	ctx *context.ProgramContext,
	section SectionIdentifier,
	issue data.RowData,
	labels []string,
	existingLabels []data.Label,
) tea.Cmd {
	issueNumber := issue.GetNumber()

	labelsMap := make(map[string]bool)
	for _, label := range labels {
		labelsMap[label] = true
	}

	existingLabelsColorMap := make(map[string]string)
	var toRemove []string
	for _, label := range existingLabels {
		existingLabelsColorMap[label.Name] = label.Color
		if _, ok := labelsMap[label.Name]; !ok {
			toRemove = append(toRemove, label.Name)
		}
	}

	return runMRAction(
		ctx, section,
		fmt.Sprintf("issue_label_%d", issueNumber),
		fmt.Sprintf("Labeling issue #%d to %s", issueNumber, labels),
		fmt.Sprintf("Issue #%d has been labeled with %s", issueNumber, labels),
		func() (tea.Msg, error) {
			err := data.UpdateIssueLabels(
				issue.GetRepoNameWithOwner(),
				issueNumber,
				labels,
				toRemove,
			)
			returnedLabels := data.IssueLabels{Nodes: []data.Label{}}
			for _, label := range labels {
				returnedLabels.Nodes = append(returnedLabels.Nodes, data.Label{
					Name:  label,
					Color: existingLabelsColorMap[label],
				})
			}
			return UpdateIssueMsg{
				IssueNumber: issueNumber,
				Labels:      &returnedLabels,
			}, err
		},
	)
}
