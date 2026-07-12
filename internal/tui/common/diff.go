package common

import (
	tea "charm.land/bubbletea/v2"
	gitlabapi "gitlab.com/gitlab-org/api/client-go"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
)

type DiffFetchedMsg struct {
	PrNumber int
	Diffs    []*gitlabapi.MergeRequestDiff
	Err      error
}

func DiffPR(prNumber int, repoName string) tea.Cmd {
	return func() tea.Msg {
		diffs, err := data.FetchMergeRequestDiffs(repoName, prNumber)
		return DiffFetchedMsg{PrNumber: prNumber, Diffs: diffs, Err: err}
	}
}
