package tui

import (
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prrow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prssection"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/section"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

func newOpenBrowserTestContext(t *testing.T, view config.ViewType) *context.ProgramContext {
	t.Helper()
	cfg, err := config.ParseConfig(config.Location{
		ConfigFlag:       "../config/testdata/test-config.yml",
		SkipGlobalConfig: true,
	})
	require.NoError(t, err)

	ctx := &context.ProgramContext{
		Config: &cfg,
		View:   view,
		StartTask: func(task context.Task) tea.Cmd {
			return nil
		},
	}
	ctx.Theme = theme.ParseTheme(ctx.Config)
	ctx.Styles = context.InitStyles(ctx.Theme)
	return ctx
}

func newModelWithCurrentPRURL(t *testing.T, url string) Model {
	t.Helper()
	ctx := newOpenBrowserTestContext(t, config.PRsView)

	prSection := prssection.NewModel(
		0,
		ctx,
		config.PrsSectionConfig{Title: "Test", Filters: "is:open"},
		time.Now(),
		time.Now(),
	)
	prSection.Prs = []prrow.Data{
		{Primary: &data.PullRequestData{Url: url}},
	}

	return Model{
		ctx: ctx,
		prs: []section.Section{&prSection},
	}
}

func runOpenBrowserCmd(t *testing.T, m Model) constants.TaskFinishedMsg {
	t.Helper()
	cmd := m.openBrowser()
	require.NotNil(t, cmd, "openBrowser() should return a non-nil command")

	msg := cmd()
	if finished, ok := msg.(constants.TaskFinishedMsg); ok {
		return finished
	}

	batch, ok := msg.(tea.BatchMsg)
	require.True(t, ok, "expected constants.TaskFinishedMsg or tea.BatchMsg, got %T", msg)

	for _, sub := range batch {
		if sub == nil {
			continue
		}
		if finished, ok := sub().(constants.TaskFinishedMsg); ok {
			return finished
		}
	}

	t.Fatal("openBrowser() command did not produce a constants.TaskFinishedMsg")
	return constants.TaskFinishedMsg{}
}

func TestOpenBrowser_InvokesOpenURLWithCurrentRowURL(t *testing.T) {
	const wantURL = "https://github.com/owner/repo/pull/42"

	originalOpenURL := openURL
	defer func() { openURL = originalOpenURL }()

	var gotURL string
	var callCount int
	openURL = func(url string) error {
		callCount++
		gotURL = url
		return nil
	}

	m := newModelWithCurrentPRURL(t, wantURL)

	finished := runOpenBrowserCmd(t, m)

	require.Equal(t, 1, callCount, "openURL should be called exactly once")
	require.Equal(t, wantURL, gotURL, "openURL should receive the current row's URL")
	require.NoError(t, finished.Err)
}

func TestOpenBrowser_PropagatesOpenURLError(t *testing.T) {
	wantErr := errors.New("xdg-open: command not found")

	originalOpenURL := openURL
	defer func() { openURL = originalOpenURL }()

	var callCount int
	openURL = func(url string) error {
		callCount++
		return wantErr
	}

	m := newModelWithCurrentPRURL(t, "https://github.com/owner/repo/pull/7")

	finished := runOpenBrowserCmd(t, m)

	require.Equal(t, 1, callCount, "openURL should be called exactly once")
	require.ErrorIs(t, finished.Err, wantErr)
}

func TestOpenBrowser_NoCurrentSelection_ReturnsErrorWithoutCallingOpenURL(t *testing.T) {
	originalOpenURL := openURL
	defer func() { openURL = originalOpenURL }()

	var called bool
	openURL = func(url string) error {
		called = true
		return nil
	}

	ctx := &context.ProgramContext{
		View: config.IssuesView,
		StartTask: func(task context.Task) tea.Cmd {
			return nil
		},
	}
	m := Model{ctx: ctx}

	finished := runOpenBrowserCmd(t, m)

	require.False(t, called, "openURL should not be called when there is no current selection")
	require.EqualError(t, finished.Err, "current selection doesn't have a URL")
}
