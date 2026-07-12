package diffviewport

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
	gitlabapi "gitlab.com/gitlab-org/api/client-go"

	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
)

func newTestModel() Model {
	return NewModel(context.ProgramContext{}, constants.Dimensions{Width: 80, Height: 20})
}

func TestSetDiff_PopulatesViewWithFileContent(t *testing.T) {
	m := newTestModel()

	m.SetDiff([]*gitlabapi.MergeRequestDiff{
		{
			OldPath: "main.go",
			NewPath: "main.go",
			Diff:    "@@ -1 +1 @@\n-old line\n+new line",
		},
	})

	view := ansi.Strip(m.View())

	require.Contains(t, view, "main.go")
	require.Contains(t, view, "new line")
}

func TestSetDiff_MultipleFilesAreAllRendered(t *testing.T) {
	m := newTestModel()

	m.SetDiff([]*gitlabapi.MergeRequestDiff{
		{OldPath: "a.go", NewPath: "a.go", Diff: "diff-a-content"},
		{OldPath: "b.go", NewPath: "b.go", Diff: "diff-b-content"},
	})

	view := ansi.Strip(m.View())

	require.Contains(t, view, "a.go")
	require.Contains(t, view, "diff-a-content")
	require.Contains(t, view, "b.go")
	require.Contains(t, view, "diff-b-content")
}

func TestSetDiff_NewFileHeaderUsesNewPath(t *testing.T) {
	m := newTestModel()

	m.SetDiff([]*gitlabapi.MergeRequestDiff{
		{NewFile: true, NewPath: "created.go", Diff: "+entire file"},
	})

	view := ansi.Strip(m.View())

	require.Contains(t, view, "+++ created.go")
}

func TestSetDiff_DeletedFileHeaderUsesOldPath(t *testing.T) {
	m := newTestModel()

	m.SetDiff([]*gitlabapi.MergeRequestDiff{
		{DeletedFile: true, OldPath: "removed.go", Diff: "-entire file"},
	})

	view := ansi.Strip(m.View())

	require.Contains(t, view, "--- removed.go")
}

func TestSetDiff_RenamedFileHeaderShowsOldAndNewPath(t *testing.T) {
	m := newTestModel()

	m.SetDiff([]*gitlabapi.MergeRequestDiff{
		{RenamedFile: true, OldPath: "old_name.go", NewPath: "new_name.go", Diff: ""},
	})

	view := ansi.Strip(m.View())

	require.Contains(t, view, "old_name.go -> new_name.go")
}

func TestSetDiff_EmptyDiffsDoesNotPanicAndViewStaysUsable(t *testing.T) {
	m := newTestModel()

	require.NotPanics(t, func() {
		m.SetDiff([]*gitlabapi.MergeRequestDiff{})
	})
	require.NotPanics(t, func() {
		_ = m.View()
	})
}

func TestSetDiff_NilDiffsDoesNotPanic(t *testing.T) {
	m := newTestModel()

	require.NotPanics(t, func() {
		m.SetDiff(nil)
	})
	require.NotPanics(t, func() {
		_ = m.View()
	})
}

func TestSetDimensions_ResizesViewportWithoutPanickingOnNegativeValues(t *testing.T) {
	m := newTestModel()

	require.NotPanics(t, func() {
		m.SetDimensions(constants.Dimensions{Width: -5, Height: -5})
	})
	require.NotPanics(t, func() {
		_ = m.View()
	})
}

func TestUpdateProgramContext_UpdatesInternalContext(t *testing.T) {
	m := newTestModel()

	m.UpdateProgramContext(&context.ProgramContext{User: "someone"})

	require.Equal(t, "someone", m.ctx.User)
}

func TestUpdateProgramContext_OverwritesPreviousContext(t *testing.T) {
	m := NewModel(
		context.ProgramContext{User: "first"},
		constants.Dimensions{Width: 80, Height: 20},
	)

	m.UpdateProgramContext(&context.ProgramContext{User: "second"})

	require.Equal(t, "second", m.ctx.User)
}
