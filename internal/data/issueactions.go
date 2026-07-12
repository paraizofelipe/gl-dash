package data

import (
	"sort"

	gitlabapi "gitlab.com/gitlab-org/api/client-go"
)

func ReopenIssue(projectPath string, issueIID int) error {
	return updateIssueState(projectPath, issueIID, "reopen")
}

func CloseIssue(projectPath string, issueIID int) error {
	return updateIssueState(projectPath, issueIID, "close")
}

func updateIssueState(projectPath string, issueIID int, stateEvent string) error {
	c, err := resolveRESTClient()
	if err != nil {
		return err
	}
	_, _, err = c.Issues.UpdateIssue(
		projectPath,
		int64(issueIID),
		&gitlabapi.UpdateIssueOptions{
			StateEvent: gitlabapi.Ptr(stateEvent),
		},
	)
	return err
}

func AddIssueAssignees(projectPath string, issueIID int, usernames []string) error {
	return updateIssueAssignees(projectPath, issueIID, usernames, true)
}

func RemoveIssueAssignees(projectPath string, issueIID int, usernames []string) error {
	return updateIssueAssignees(projectPath, issueIID, usernames, false)
}

func updateIssueAssignees(
	projectPath string,
	issueIID int,
	usernames []string,
	add bool,
) error {
	c, err := resolveRESTClient()
	if err != nil {
		return err
	}

	ids, err := resolveUserIDs(usernames)
	if err != nil {
		return err
	}

	issue, _, err := c.Issues.GetIssue(projectPath, int64(issueIID))
	if err != nil {
		return err
	}

	assignees := make(map[int64]bool, len(issue.Assignees))
	for _, u := range issue.Assignees {
		assignees[u.ID] = true
	}
	for _, id := range ids {
		if add {
			assignees[id] = true
		} else {
			delete(assignees, id)
		}
	}

	merged := make([]int64, 0, len(assignees))
	for id := range assignees {
		merged = append(merged, id)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i] < merged[j] })

	_, _, err = c.Issues.UpdateIssue(
		projectPath,
		int64(issueIID),
		&gitlabapi.UpdateIssueOptions{
			AssigneeIDs: &merged,
		},
	)
	return err
}

func CommentOnIssue(projectPath string, issueIID int, body string) error {
	c, err := resolveRESTClient()
	if err != nil {
		return err
	}
	_, _, err = c.Notes.CreateIssueNote(
		projectPath,
		int64(issueIID),
		&gitlabapi.CreateIssueNoteOptions{
			Body: gitlabapi.Ptr(body),
		},
	)
	return err
}

func UpdateIssueLabels(
	projectPath string,
	issueIID int,
	addLabels, removeLabels []string,
) error {
	c, err := resolveRESTClient()
	if err != nil {
		return err
	}
	add := gitlabapi.LabelOptions(addLabels)
	remove := gitlabapi.LabelOptions(removeLabels)
	_, _, err = c.Issues.UpdateIssue(
		projectPath,
		int64(issueIID),
		&gitlabapi.UpdateIssueOptions{
			AddLabels:    &add,
			RemoveLabels: &remove,
		},
	)
	return err
}
