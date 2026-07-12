package prssection

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

func (m *Model) checkout() (tea.Cmd, error) {
	pr := m.GetCurrRow()
	if pr == nil {
		return nil, errors.New("no pr selected")
	}

	repoName := pr.GetRepoNameWithOwner()
	repoPath, ok := common.GetRepoLocalPath(repoName, m.Ctx.Config.RepoPaths)

	if !ok {
		return nil, errors.New(
			"local path to repo not specified, set one in your config.yml under repoPaths",
		)
	}

	prNumber := pr.GetNumber()
	taskId := fmt.Sprintf("checkout_%d", prNumber)
	task := context.Task{
		Id:           taskId,
		StartText:    fmt.Sprintf("Checking out PR #%d", prNumber),
		FinishedText: fmt.Sprintf("PR #%d has been checked out at %s", prNumber, repoPath),
		State:        context.TaskStart,
		Error:        nil,
	}
	startCmd := m.Ctx.StartTask(task)
	return tea.Batch(startCmd, func() tea.Msg {
		userHomeDir, _ := os.UserHomeDir()
		if strings.HasPrefix(repoPath, "~") {
			repoPath = strings.Replace(repoPath, "~", userHomeDir, 1)
		}

		err := git.CheckoutMergeRequest(repoPath, prNumber)
		return constants.TaskFinishedMsg{TaskId: taskId, Err: err}
	}), nil
}
