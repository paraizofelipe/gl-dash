package issuerow

import (
	"testing"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

func TestRenderOpenedBy(t *testing.T) {
	authorText := compat.AdaptiveColor{
		Light: lipgloss.Color("#00FF00"),
		Dark:  lipgloss.Color("#00FF00"),
	}
	primaryText := compat.AdaptiveColor{
		Light: lipgloss.Color("#000000"),
		Dark:  lipgloss.Color("#FFFFFF"),
	}

	ctx := &context.ProgramContext{
		Theme: theme.Theme{
			PrimaryText: primaryText,
			AuthorText:  authorText,
		},
	}

	issue := &Issue{
		Ctx: ctx,
		Data: data.IssueData{
			Author: struct {
				Login string
			}{Login: "octocat"},
		},
	}

	result := issue.renderOpenedBy()

	wantAuthorRun := lipgloss.NewStyle().Foreground(authorText).Render("octocat")
	wantPrimaryRun := lipgloss.NewStyle().Foreground(primaryText).Render("octocat")

	if result != wantAuthorRun {
		t.Errorf("renderOpenedBy() = %q, want %q (styled with Theme.AuthorText)", result, wantAuthorRun)
	}

	if result == wantPrimaryRun {
		t.Errorf("renderOpenedBy() = %q, should not be styled with Theme.PrimaryText", result)
	}
}
