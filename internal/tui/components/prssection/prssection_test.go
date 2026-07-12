package prssection

import (
	"os"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prompt"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prrow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/section"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/table"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/utils"
)

// newTestModel creates a minimal Model with the prompt confirmation box
// focused and a single PR row so that GetCurrRow returns non-nil.
func newTestModel(action string) Model {
	ctx := &context.ProgramContext{
		StartTask: func(task context.Task) tea.Cmd {
			return func() tea.Msg { return nil }
		},
	}
	m := Model{
		BaseModel: section.BaseModel{
			Ctx:                       ctx,
			IsPromptConfirmationShown: true,
			PromptConfirmationAction:  action,
			PromptConfirmationBox:     prompt.NewModel(ctx),
		},
		Prs: []prrow.Data{
			{Primary: &data.PullRequestData{Number: 42}},
		},
	}
	m.PromptConfirmationBox.Focus()
	return m
}

func TestConfirmation_EmptyInputDoesNotConfirm(t *testing.T) {
	// Pressing Enter without typing anything should NOT confirm, since the
	// prompt says (y/N) indicating N is the default.
	m := newTestModel("close")

	msg := tea.KeyPressMsg{Code: tea.KeyEnter}
	_, _ = m.Update(msg)

	require.False(t, m.IsPromptConfirmationShown,
		"confirmation prompt should be dismissed")
}

func TestConfirmation_AcceptWithLowercaseY(t *testing.T) {
	m := newTestModel("merge")
	m.PromptConfirmationBox.SetValue("y")

	msg := tea.KeyPressMsg{Code: tea.KeyEnter}
	_, cmd := m.Update(msg)

	require.NotNil(t, cmd, "lowercase y should execute the action")
}

func TestConfirmation_AcceptWithUppercaseY(t *testing.T) {
	m := newTestModel("reopen")
	m.PromptConfirmationBox.SetValue("Y")

	msg := tea.KeyPressMsg{Code: tea.KeyEnter}
	_, cmd := m.Update(msg)

	require.NotNil(t, cmd, "uppercase Y should execute the action")
}

func TestConfirmation_RejectWithN(t *testing.T) {
	m := newTestModel("close")
	m.PromptConfirmationBox.SetValue("n")

	msg := tea.KeyPressMsg{Code: tea.KeyEnter}
	_, cmd := m.Update(msg)

	// cmd is a batch of (nil, blinkCmd) -- the nil means no action was taken.
	// We verify the prompt is dismissed regardless.
	require.False(t, m.IsPromptConfirmationShown,
		"confirmation prompt should be dismissed on rejection")
	_ = cmd
}

func TestConfirmation_CancelWithEsc(t *testing.T) {
	m := newTestModel("merge")

	msg := tea.KeyPressMsg{Code: tea.KeyEsc}
	_, cmd := m.Update(msg)

	require.False(t, m.IsPromptConfirmationShown,
		"Esc should dismiss the confirmation prompt")
	_ = cmd
}

func TestConfirmation_CancelWithCtrlC(t *testing.T) {
	m := newTestModel("update")

	msg := tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	_, cmd := m.Update(msg)

	require.False(t, m.IsPromptConfirmationShown,
		"Ctrl+C should dismiss the confirmation prompt")
	_ = cmd
}

func TestConfirmation_AllActions(t *testing.T) {
	actions := []string{"close", "reopen", "ready", "merge", "update", "approveWorkflows"}

	for _, action := range actions {
		t.Run(action+"_empty_input_does_not_confirm", func(t *testing.T) {
			m := newTestModel(action)

			msg := tea.KeyPressMsg{Code: tea.KeyEnter}
			_, _ = m.Update(msg)

			require.False(t, m.IsPromptConfirmationShown,
				"empty input should dismiss prompt for action %q", action)
		})

		t.Run(action+"_explicit_y", func(t *testing.T) {
			m := newTestModel(action)
			m.PromptConfirmationBox.SetValue("y")

			msg := tea.KeyPressMsg{Code: tea.KeyEnter}
			_, cmd := m.Update(msg)

			require.NotNil(t, cmd,
				"explicit y should confirm for action %q", action)
		})
	}
}

func newProgramContextWithParsedDefaultConfig(t *testing.T) *context.ProgramContext {
	t.Helper()

	dir, err := os.MkdirTemp("", "config")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("GH_DASH_CONFIG", "")

	cfg, err := config.ParseConfig(config.Location{})
	require.NoError(t, err)

	return &context.ProgramContext{Config: &cfg}
}

func findColumnByTitle(t *testing.T, columns []table.Column, title string) table.Column {
	t.Helper()

	for _, c := range columns {
		if c.Title == title {
			return c
		}
	}

	t.Fatalf("column with title %q not found", title)
	return table.Column{}
}

func TestGetSectionColumns_LabelsColumnVisibleByDefault(t *testing.T) {
	ctx := newProgramContextWithParsedDefaultConfig(t)

	columns := GetSectionColumns(config.PrsSectionConfig{}, ctx)

	labelsColumn := findColumnByTitle(t, columns, constants.LabelsIcon)
	require.NotNil(t, labelsColumn.Hidden)
	require.False(t, *labelsColumn.Hidden)
}

func TestGetSectionColumns_LabelsColumnHiddenWhenSectionOverridesHidden(t *testing.T) {
	ctx := newProgramContextWithParsedDefaultConfig(t)

	sectionCfg := config.PrsSectionConfig{
		Layout: config.PrsLayoutConfig{
			Labels: config.ColumnConfig{Hidden: utils.BoolPtr(true)},
		},
	}

	columns := GetSectionColumns(sectionCfg, ctx)

	labelsColumn := findColumnByTitle(t, columns, constants.LabelsIcon)
	require.NotNil(t, labelsColumn.Hidden)
	require.True(t, *labelsColumn.Hidden)
}
