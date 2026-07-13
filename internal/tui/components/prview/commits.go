package prview

import (
	"fmt"

	"charm.land/lipgloss/v2"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
)

// commitsNotYetPopulated: commit data isn't fetched from GitLab yet (tracked
// as a follow-up to T40); this count is a placeholder, not a real "zero
// commits" observation.
const commitsNotYetPopulated = 0

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

	return main.MarginBottom(1).Underline(true).Render(
		fmt.Sprintf("%s  %d commits", constants.CommitIcon, commitsNotYetPopulated),
	)
}
