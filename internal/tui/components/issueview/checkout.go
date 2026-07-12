package issueview

import (
	"errors"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/git"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/common"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
)

func (m *Model) Checkout() (tea.Cmd, error) {
	if m.issue == nil {
		return nil, errors.New("no issue selected")
	}

	issue := m.issue.Data
	repoName := issue.GetRepoNameWithOwner()
	repoPath, ok := common.GetRepoLocalPath(repoName, m.ctx.Config.RepoPaths)
	if !ok {
		return nil, errors.New(
			"local path to repo not specified, set one in your config.yml under repoPaths",
		)
	}

	issueNumber := issue.GetNumber()
	taskId := fmt.Sprintf("issue_checkout_%d", issueNumber)
	task := context.Task{
		Id:        taskId,
		StartText: fmt.Sprintf("Checking out branch for issue #%d", issueNumber),
		FinishedText: fmt.Sprintf(
			"Branch for issue #%d has been checked out at %s",
			issueNumber,
			repoPath,
		),
		State: context.TaskStart,
		Error: nil,
	}
	startCmd := m.ctx.StartTask(task)
	return tea.Batch(startCmd, func() tea.Msg {
		userHomeDir, _ := os.UserHomeDir()
		if strings.HasPrefix(repoPath, "~") {
			repoPath = strings.Replace(repoPath, "~", userHomeDir, 1)
		}

		branchName := issueBranchName(issueNumber)
		err := git.CheckoutBranch(repoPath, branchName)
		return constants.TaskFinishedMsg{TaskId: taskId, Err: err}
	}), nil
}

func issueBranchName(issueNumber int) string {
	return fmt.Sprintf("%d-issue", issueNumber)
}
