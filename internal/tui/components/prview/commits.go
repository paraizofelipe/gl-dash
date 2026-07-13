package prview

import (
	"fmt"

	"charm.land/lipgloss/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/utils"
)

const shortShaLen = 8

func (m *Model) renderCommits() string {
	main := m.ctx.Styles.Common.MainTextStyle
	faint := m.ctx.Styles.Common.FaintTextStyle

	if !m.pr.Data.IsEnriched {
		return lipgloss.JoinHorizontal(
			lipgloss.Top,
			m.ctx.Styles.Common.WaitingGlyph,
			" ",
			faint.Render("Loading..."),
		)
	}

	commits := m.pr.Data.Enriched.Commits
	title := main.MarginBottom(1).Underline(true).Render(
		fmt.Sprintf("%s  %d commits", constants.CommitIcon, len(commits)),
	)
	if len(commits) == 0 {
		return title
	}

	rows := make([]string, 0, len(commits))
	for _, c := range commits {
		rows = append(rows, m.renderCommit(c))
	}
	return lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		lipgloss.JoinVertical(lipgloss.Left, rows...),
	)
}

func (m *Model) renderCommit(commit data.PullRequestCommit) string {
	faint := m.ctx.Styles.Common.FaintTextStyle
	main := m.ctx.Styles.Common.MainTextStyle

	sha := commit.Sha
	if len(sha) > shortShaLen {
		sha = sha[:shortShaLen]
	}

	segments := []string{faint.Render(sha), " ", main.Render(commit.Title)}
	if commit.Author != "" {
		segments = append(segments, " ", faint.Render(commit.Author))
	}
	segments = append(segments, " ", faint.Render(utils.TimeElapsed(commit.CreatedAt)))

	return lipgloss.JoinHorizontal(lipgloss.Top, segments...)
}
