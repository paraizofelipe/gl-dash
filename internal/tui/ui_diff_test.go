package tui

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gitlabapi "gitlab.com/gitlab-org/api/client-go"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/common"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/branchsidebar"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/footer"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/issueview"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/notificationview"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prrow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prssection"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prview"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/section"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/sidebar"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/tabs"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/keys"
)

func TestIsDiffStillRelevant_NotificationSubjectPRMatches(t *testing.T) {
	m := Model{ctx: &context.ProgramContext{View: config.PRsView}}
	m.notificationView.SetSubjectPR(
		&prrow.Data{Primary: &data.PullRequestData{Number: 42}},
		"notif-1",
	)

	require.True(t, m.isDiffStillRelevant(42))
}

func TestIsDiffStillRelevant_NotificationSubjectPRDoesNotMatch(t *testing.T) {
	m := Model{ctx: &context.ProgramContext{View: config.PRsView}}
	m.notificationView.SetSubjectPR(
		&prrow.Data{Primary: &data.PullRequestData{Number: 42}},
		"notif-1",
	)

	require.False(t, m.isDiffStillRelevant(43))
}

func TestIsDiffStillRelevant_CurrRowMatchesWhenNoNotificationSubject(t *testing.T) {
	ctx := newOpenBrowserTestContext(t, config.PRsView)
	prSection := prssection.NewModel(0, ctx, config.PrsSectionConfig{}, time.Now(), time.Now())
	prSection.Prs = []prrow.Data{{Primary: &data.PullRequestData{Number: 7}}}
	m := Model{ctx: ctx, prs: []section.Section{&prSection}}

	require.True(t, m.isDiffStillRelevant(7))
}

func TestIsDiffStillRelevant_CurrRowDoesNotMatchWhenNoNotificationSubject(t *testing.T) {
	ctx := newOpenBrowserTestContext(t, config.PRsView)
	prSection := prssection.NewModel(0, ctx, config.PrsSectionConfig{}, time.Now(), time.Now())
	prSection.Prs = []prrow.Data{{Primary: &data.PullRequestData{Number: 7}}}
	m := Model{ctx: ctx, prs: []section.Section{&prSection}}

	require.False(t, m.isDiffStillRelevant(8))
}

func TestIsDiffStillRelevant_NotificationSubjectTakesPrecedenceOverCurrRow(t *testing.T) {
	ctx := newOpenBrowserTestContext(t, config.PRsView)
	prSection := prssection.NewModel(0, ctx, config.PrsSectionConfig{}, time.Now(), time.Now())
	prSection.Prs = []prrow.Data{{Primary: &data.PullRequestData{Number: 7}}}
	m := Model{ctx: ctx, prs: []section.Section{&prSection}}
	m.notificationView.SetSubjectPR(
		&prrow.Data{Primary: &data.PullRequestData{Number: 42}},
		"notif-1",
	)

	require.True(t, m.isDiffStillRelevant(42))
	require.False(t, m.isDiffStillRelevant(7))
}

func TestIsDiffStillRelevant_NoSubjectAndNoCurrSection_DefaultsToTrue(t *testing.T) {
	m := Model{ctx: &context.ProgramContext{View: config.PRsView}}

	require.True(t, m.isDiffStillRelevant(999))
}

func TestIsDiffStillRelevant_NoSubjectAndEmptySection_DefaultsToTrue(t *testing.T) {
	ctx := newOpenBrowserTestContext(t, config.PRsView)
	prSection := prssection.NewModel(0, ctx, config.PrsSectionConfig{}, time.Now(), time.Now())
	m := Model{ctx: ctx, prs: []section.Section{&prSection}}

	require.True(t, m.isDiffStillRelevant(999))
}

func newModelForDiffFetchedMsg(t *testing.T, currentPRNumber int) Model {
	t.Helper()
	ctx := newOpenBrowserTestContext(t, config.PRsView)
	ctx.MainContentHeight = 20
	ctx.DynamicPreviewWidth = 80

	sidebarModel := sidebar.NewModel()
	sidebarModel.UpdateProgramContext(ctx)

	prSection := prssection.NewModel(0, ctx, config.PrsSectionConfig{}, time.Now(), time.Now())
	prSection.Prs = []prrow.Data{
		{Primary: &data.PullRequestData{Number: currentPRNumber}},
	}

	return Model{
		ctx:              ctx,
		keys:             keys.Keys,
		prView:           prview.NewModel(ctx),
		issueSidebar:     issueview.NewModel(ctx),
		notificationView: notificationview.NewModel(ctx),
		branchSidebar:    branchsidebar.NewModel(ctx),
		footer:           footer.NewModel(ctx),
		tabs:             tabs.NewModel(ctx),
		sidebar:          sidebarModel,
		prs:              []section.Section{&prSection},
	}
}

func TestUpdate_DiffFetchedMsg_MatchingCurrentPR_PopulatesSidebar(t *testing.T) {
	m := newModelForDiffFetchedMsg(t, 42)
	beforeView := m.diffViewport.View()

	msg := common.DiffFetchedMsg{
		PrNumber: 42,
		Diffs: []*gitlabapi.MergeRequestDiff{
			{NewPath: "main.go", Diff: "+new line"},
		},
	}

	newModel, _ := m.Update(msg)
	updated := newModel.(Model)

	require.True(
		t,
		updated.sidebar.IsOpen,
		"sidebar should open for a diff matching the current PR",
	)
	require.NotEqual(t, beforeView, updated.diffViewport.View(),
		"diff viewport content should be populated for a matching PR")
}

func TestUpdate_DiffFetchedMsg_StalePRNumber_DoesNotPopulateSidebar(t *testing.T) {
	m := newModelForDiffFetchedMsg(t, 42)
	beforeView := m.diffViewport.View()

	msg := common.DiffFetchedMsg{
		PrNumber: 99,
		Diffs: []*gitlabapi.MergeRequestDiff{
			{NewPath: "stale.go", Diff: "+stale line"},
		},
	}

	newModel, _ := m.Update(msg)
	updated := newModel.(Model)

	require.False(t, updated.sidebar.IsOpen, "sidebar should stay closed for a stale diff response")
	require.Equal(t, beforeView, updated.diffViewport.View(),
		"diff viewport content should not change for a stale PR response")
}

func TestUpdate_DiffFetchedMsg_StalePRNumberWithError_DoesNotOverwriteCtxError(t *testing.T) {
	m := newModelForDiffFetchedMsg(t, 42)
	sentinelErr := errors.New("previous unrelated error")
	m.ctx.Error = sentinelErr

	msg := common.DiffFetchedMsg{
		PrNumber: 99,
		Err:      errors.New("stale fetch error"),
	}

	newModel, _ := m.Update(msg)
	updated := newModel.(Model)

	require.Equal(t, sentinelErr, updated.ctx.Error,
		"a stale error response should not overwrite the current ctx error")
}

func TestUpdate_DiffFetchedMsg_MatchingPRNumberWithError_SetsCtxError(t *testing.T) {
	m := newModelForDiffFetchedMsg(t, 42)

	fetchErr := errors.New("fetch failed")
	msg := common.DiffFetchedMsg{
		PrNumber: 42,
		Err:      fetchErr,
	}

	newModel, _ := m.Update(msg)
	updated := newModel.(Model)

	require.Equal(t, fetchErr, updated.ctx.Error)
	require.False(t, updated.sidebar.IsOpen)
}
