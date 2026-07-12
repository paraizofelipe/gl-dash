package data

import (
	"fmt"
	"regexp"
	"sort"

	gitlabapi "gitlab.com/gitlab-org/api/client-go"
)

func ReopenMergeRequest(projectPath string, mrIID int) error {
	return updateMergeRequestState(projectPath, mrIID, "reopen")
}

func CloseMergeRequest(projectPath string, mrIID int) error {
	return updateMergeRequestState(projectPath, mrIID, "close")
}

func updateMergeRequestState(projectPath string, mrIID int, stateEvent string) error {
	c, err := resolveRESTClient()
	if err != nil {
		return err
	}
	_, _, err = c.MergeRequests.UpdateMergeRequest(
		projectPath,
		int64(mrIID),
		&gitlabapi.UpdateMergeRequestOptions{
			StateEvent: gitlabapi.Ptr(stateEvent),
		},
	)
	return err
}

var draftTitlePrefixRe = regexp.MustCompile(
	`(?i)^(draft:\s*|\[draft\]\s*|\(draft\)\s*|wip:\s*|\[wip\]\s*|\(wip\)\s*)`,
)

func MarkMergeRequestReady(projectPath string, mrIID int) error {
	c, err := resolveRESTClient()
	if err != nil {
		return err
	}
	mr, _, err := c.MergeRequests.GetMergeRequest(projectPath, int64(mrIID), nil)
	if err != nil {
		return err
	}
	newTitle := draftTitlePrefixRe.ReplaceAllString(mr.Title, "")
	_, _, err = c.MergeRequests.UpdateMergeRequest(
		projectPath,
		int64(mrIID),
		&gitlabapi.UpdateMergeRequestOptions{
			Title: gitlabapi.Ptr(newTitle),
		},
	)
	return err
}

func RebaseMergeRequest(projectPath string, mrIID int) error {
	c, err := resolveRESTClient()
	if err != nil {
		return err
	}
	_, err = c.MergeRequests.RebaseMergeRequest(projectPath, int64(mrIID), nil)
	return err
}

func AddMergeRequestAssignees(projectPath string, mrIID int, usernames []string) error {
	return updateMergeRequestAssignees(projectPath, mrIID, usernames, true)
}

func RemoveMergeRequestAssignees(projectPath string, mrIID int, usernames []string) error {
	return updateMergeRequestAssignees(projectPath, mrIID, usernames, false)
}

func updateMergeRequestAssignees(
	projectPath string,
	mrIID int,
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

	mr, _, err := c.MergeRequests.GetMergeRequest(projectPath, int64(mrIID), nil)
	if err != nil {
		return err
	}

	assignees := make(map[int64]bool, len(mr.Assignees))
	for _, u := range mr.Assignees {
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

	_, _, err = c.MergeRequests.UpdateMergeRequest(
		projectPath,
		int64(mrIID),
		&gitlabapi.UpdateMergeRequestOptions{
			AssigneeIDs: &merged,
		},
	)
	return err
}

func resolveUserIDs(usernames []string) ([]int64, error) {
	c, err := resolveRESTClient()
	if err != nil {
		return nil, err
	}

	ids := make([]int64, 0, len(usernames))
	for _, username := range usernames {
		users, _, err := c.Users.ListUsers(&gitlabapi.ListUsersOptions{
			Username: gitlabapi.Ptr(username),
		})
		if err != nil {
			return nil, err
		}
		var found *gitlabapi.User
		for _, u := range users {
			if u.Username == username {
				found = u
				break
			}
		}
		if found == nil {
			return nil, fmt.Errorf("gitlab user not found: %s", username)
		}
		ids = append(ids, found.ID)
	}
	return ids, nil
}

func CommentOnMergeRequest(projectPath string, mrIID int, body string) error {
	c, err := resolveRESTClient()
	if err != nil {
		return err
	}
	_, _, err = c.Notes.CreateMergeRequestNote(
		projectPath,
		int64(mrIID),
		&gitlabapi.CreateMergeRequestNoteOptions{
			Body: gitlabapi.Ptr(body),
		},
	)
	return err
}

func ApproveMergeRequest(projectPath string, mrIID int, comment string) error {
	c, err := resolveRESTClient()
	if err != nil {
		return err
	}
	if _, _, err := c.MergeRequestApprovals.ApproveMergeRequest(
		projectPath,
		int64(mrIID),
		nil,
	); err != nil {
		return err
	}
	if comment == "" {
		return nil
	}
	_, _, err = c.Notes.CreateMergeRequestNote(
		projectPath,
		int64(mrIID),
		&gitlabapi.CreateMergeRequestNoteOptions{
			Body: gitlabapi.Ptr(comment),
		},
	)
	return err
}

func AcceptMergeRequest(projectPath string, mrIID int) error {
	c, err := resolveRESTClient()
	if err != nil {
		return err
	}
	_, _, err = c.MergeRequests.AcceptMergeRequest(projectPath, int64(mrIID), nil)
	return err
}

func ProjectDefaultBranch(projectPath string) (string, error) {
	c, err := resolveRESTClient()
	if err != nil {
		return "", err
	}
	proj, _, err := c.Projects.GetProject(projectPath, nil)
	if err != nil {
		return "", err
	}
	return proj.DefaultBranch, nil
}

func CreateMergeRequest(projectPath, sourceBranch, targetBranch, title string) error {
	c, err := resolveRESTClient()
	if err != nil {
		return err
	}
	_, _, err = c.MergeRequests.CreateMergeRequest(
		projectPath,
		&gitlabapi.CreateMergeRequestOptions{
			Title:        gitlabapi.Ptr(title),
			SourceBranch: gitlabapi.Ptr(sourceBranch),
			TargetBranch: gitlabapi.Ptr(targetBranch),
		},
	)
	return err
}

func FindMergeRequestWebURLByBranch(projectPath, branch string) (string, error) {
	c, err := resolveRESTClient()
	if err != nil {
		return "", err
	}
	mrs, _, err := c.MergeRequests.ListProjectMergeRequests(
		projectPath,
		&gitlabapi.ListProjectMergeRequestsOptions{
			SourceBranch: gitlabapi.Ptr(branch),
		},
	)
	if err != nil {
		return "", err
	}
	if len(mrs) == 0 {
		return "", fmt.Errorf("no merge request found for branch %q", branch)
	}
	return mrs[0].WebURL, nil
}

func UpdateMergeRequestLabels(
	projectPath string,
	mrIID int,
	addLabels, removeLabels []string,
) error {
	c, err := resolveRESTClient()
	if err != nil {
		return err
	}
	add := gitlabapi.LabelOptions(addLabels)
	remove := gitlabapi.LabelOptions(removeLabels)
	_, _, err = c.MergeRequests.UpdateMergeRequest(
		projectPath,
		int64(mrIID),
		&gitlabapi.UpdateMergeRequestOptions{
			AddLabels:    &add,
			RemoveLabels: &remove,
		},
	)
	return err
}

func FetchMergeRequestDiffs(projectPath string, mrIID int) ([]*gitlabapi.MergeRequestDiff, error) {
	c, err := resolveRESTClient()
	if err != nil {
		return nil, err
	}

	opts := &gitlabapi.ListMergeRequestDiffsOptions{
		ListOptions: gitlabapi.ListOptions{PerPage: 100, Page: 1},
	}

	var all []*gitlabapi.MergeRequestDiff
	for {
		diffs, resp, err := c.MergeRequests.ListMergeRequestDiffs(projectPath, int64(mrIID), opts)
		if err != nil {
			return nil, err
		}
		all = append(all, diffs...)
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return all, nil
}
