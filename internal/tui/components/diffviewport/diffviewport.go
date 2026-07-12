package diffviewport

import (
	"strings"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
	gitlabapi "gitlab.com/gitlab-org/api/client-go"

	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
)

type Model struct {
	ctx      context.ProgramContext
	viewport viewport.Model
}

func NewModel(ctx context.ProgramContext, dimensions constants.Dimensions) Model {
	return Model{
		ctx: ctx,
		viewport: viewport.New(
			viewport.WithWidth(dimensions.Width),
			viewport.WithHeight(dimensions.Height),
		),
	}
}

func (m *Model) SetDiff(diffs []*gitlabapi.MergeRequestDiff) {
	m.viewport.SetContent(renderDiffs(diffs))
}

func renderDiffs(diffs []*gitlabapi.MergeRequestDiff) string {
	if len(diffs) == 0 {
		return ""
	}

	var b strings.Builder
	for i, d := range diffs {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(diffFileHeader(d))
		b.WriteString("\n")
		b.WriteString(d.Diff)
	}
	return b.String()
}

func diffFileHeader(d *gitlabapi.MergeRequestDiff) string {
	switch {
	case d.NewFile:
		return "+++ " + d.NewPath
	case d.DeletedFile:
		return "--- " + d.OldPath
	case d.RenamedFile:
		return d.OldPath + " -> " + d.NewPath
	default:
		return d.NewPath
	}
}

func (m *Model) SetDimensions(dimensions constants.Dimensions) {
	m.viewport.SetHeight(max(0, dimensions.Height))
	m.viewport.SetWidth(max(0, dimensions.Width))
}

func (m *Model) View() string {
	return lipgloss.NewStyle().
		Width(m.viewport.Width()).
		MaxWidth(m.viewport.Width()).
		Render(m.viewport.View())
}

func (m *Model) UpdateProgramContext(ctx *context.ProgramContext) {
	m.ctx = *ctx
}
