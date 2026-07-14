package prview

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prrow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

type checksTestOptions struct {
	isEnriched bool
	jobs       []data.PipelineJob
}

func newTestModelForChecks(t *testing.T, opts checksTestOptions) Model {
	t.Helper()
	cfg, err := config.ParseConfig(config.Location{
		ConfigFlag:       "../../../config/testdata/test-config.yml",
		SkipGlobalConfig: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	thm := theme.ParseTheme(&cfg)
	ctx := &context.ProgramContext{
		Config: &cfg,
		Theme:  thm,
		Styles: context.InitStyles(thm),
	}

	m := NewModel(ctx)
	m.ctx = ctx
	m.width = 80
	m.pr = &prrow.PullRequest{
		Ctx: ctx,
		Data: &prrow.Data{
			Primary:    &data.PullRequestData{},
			IsEnriched: opts.isEnriched,
			Enriched: data.EnrichedPullRequestData{
				Pipeline: data.MergeRequestPipeline{
					Jobs: opts.jobs,
				},
			},
		},
	}
	return m
}

func newEnrichedTestModelForChecks(t *testing.T, jobs []data.PipelineJob) Model {
	t.Helper()
	return newTestModelForChecks(t, checksTestOptions{isEnriched: true, jobs: jobs})
}

func makeJob(stage, name string, status data.PipelineStatus) data.PipelineJob {
	return data.PipelineJob{
		Stage:  stage,
		Name:   name,
		Status: status,
	}
}

func TestRenderJobName(t *testing.T) {
	tests := []struct {
		name string
		job  data.PipelineJob
		want string
	}{
		{"stage and name both present", data.PipelineJob{Stage: "test", Name: "unit"}, "test/unit"},
		{"only name present", data.PipelineJob{Stage: "", Name: "build"}, "build"},
		{"only stage present", data.PipelineJob{Stage: "deploy", Name: ""}, "deploy"},
		{"both empty", data.PipelineJob{Stage: "", Name: ""}, ""},
		{
			"surrounding whitespace is trimmed",
			data.PipelineJob{Stage: "  test  ", Name: "  unit  "},
			"test/unit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderJobName(tt.job)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestRenderJobConclusion(t *testing.T) {
	m := newEnrichedTestModelForChecks(t, nil)

	neutralGlyph := lipgloss.NewStyle().
		Foreground(m.ctx.Theme.FaintText).
		Render(constants.SmallDotIcon)

	tests := []struct {
		name         string
		status       data.PipelineStatus
		wantCategory CheckCategory
		wantIcon     string
	}{
		{
			"manual status is waiting",
			data.StatusManual,
			CheckWaiting,
			m.ctx.Styles.Common.WaitingGlyph,
		},
		{
			"pending status is waiting",
			data.StatusPending,
			CheckWaiting,
			m.ctx.Styles.Common.WaitingGlyph,
		},
		{
			"running status is waiting",
			data.StatusRunning,
			CheckWaiting,
			m.ctx.Styles.Common.WaitingGlyph,
		},
		{
			"failed status is failure",
			data.StatusFailed,
			CheckFailure,
			m.ctx.Styles.Common.FailureGlyph,
		},
		{
			"success status is success",
			data.StatusSuccess,
			CheckSuccess,
			m.ctx.Styles.Common.SuccessGlyph,
		},
		{
			"skipped status is neutral, not success",
			data.StatusSkipped,
			CheckNeutral,
			neutralGlyph,
		},
		{
			"canceled status is neutral, not success",
			data.StatusCanceled,
			CheckNeutral,
			neutralGlyph,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCategory, gotIcon := m.renderJobConclusion(data.PipelineJob{Status: tt.status})
			require.Equal(t, tt.wantCategory, gotCategory)
			require.Equal(t, tt.wantIcon, gotIcon)
		})
	}
}

func TestRenderChecks_Loading(t *testing.T) {
	t.Run("not enriched and no jobs shows loading", func(t *testing.T) {
		m := newTestModelForChecks(t, checksTestOptions{isEnriched: false})
		got := m.renderChecks()

		require.True(t, strings.Contains(got, "Loading..."),
			"expected 'Loading...' message, got: %q", got)
		require.False(t, strings.Contains(got, "No checks to display"),
			"did not expect 'No checks to display...' while not enriched, got: %q", got)
	})

	t.Run("not enriched takes priority over having jobs", func(t *testing.T) {
		m := newTestModelForChecks(t, checksTestOptions{
			isEnriched: false,
			jobs:       []data.PipelineJob{makeJob("test", "unit", data.StatusSuccess)},
		})
		got := m.renderChecks()

		require.True(t, strings.Contains(got, "Loading..."),
			"expected 'Loading...' message even with jobs present, got: %q", got)
	})
}

func TestRenderChecks_NoChecks(t *testing.T) {
	t.Run("nil jobs", func(t *testing.T) {
		m := newEnrichedTestModelForChecks(t, nil)
		got := m.renderChecks()

		require.True(t, strings.Contains(got, "No checks to display"),
			"expected 'No checks to display...' message, got: %q", got)
	})

	t.Run("empty jobs slice", func(t *testing.T) {
		m := newEnrichedTestModelForChecks(t, []data.PipelineJob{})
		got := m.renderChecks()

		require.True(t, strings.Contains(got, "No checks to display"),
			"expected 'No checks to display...' message, got: %q", got)
	})
}

func TestRenderChecks_AwaitingApproval(t *testing.T) {
	jobs := []data.PipelineJob{
		makeJob("deploy", "prod", data.StatusManual),
		makeJob("release", "tag", data.StatusManual),
	}
	m := newEnrichedTestModelForChecks(t, jobs)

	got := m.renderChecks()

	require.True(t, strings.Contains(got, "Awaiting Approval (2)"),
		"expected 'Awaiting Approval (2)' section, got: %q", got)
	require.True(t, strings.Contains(got, "deploy/prod"),
		"expected 'deploy/prod' job name, got: %q", got)
	require.True(t, strings.Contains(got, "release/tag"),
		"expected 'release/tag' job name, got: %q", got)
	require.True(t, strings.Contains(got, constants.ActionRequiredIcon),
		"expected ActionRequiredIcon, got: %q", got)
}

func TestRenderChecks_FailedJobs(t *testing.T) {
	jobs := []data.PipelineJob{
		makeJob("test", "unit", data.StatusFailed),
		makeJob("test", "lint", data.StatusSuccess),
	}
	m := newEnrichedTestModelForChecks(t, jobs)

	got := m.renderChecks()

	require.True(t, strings.Contains(got, "test/unit"),
		"expected 'test/unit' job name, got: %q", got)
	require.True(t, strings.Contains(got, "test/lint"),
		"expected 'test/lint' job name, got: %q", got)
	require.True(t, strings.Contains(got, constants.FailureIcon),
		"expected FailureIcon for failed job, got: %q", got)
	require.True(t, strings.Contains(got, constants.SuccessIcon),
		"expected SuccessIcon for successful job, got: %q", got)
}

func TestRenderChecks_WaitingJobs(t *testing.T) {
	jobs := []data.PipelineJob{
		makeJob("build", "compile", data.StatusRunning),
		makeJob("build", "package", data.StatusPending),
	}
	m := newEnrichedTestModelForChecks(t, jobs)

	got := m.renderChecks()

	require.True(t, strings.Contains(got, "build/compile"),
		"expected 'build/compile' job name, got: %q", got)
	require.True(t, strings.Contains(got, "build/package"),
		"expected 'build/package' job name, got: %q", got)
	require.True(t, strings.Contains(got, constants.WaitingIcon),
		"expected WaitingIcon for in-progress jobs, got: %q", got)
}

func TestRenderChecks_MixedStates(t *testing.T) {
	jobs := []data.PipelineJob{
		makeJob("deploy", "approval", data.StatusManual),
		makeJob("test", "unit", data.StatusFailed),
		makeJob("build", "compile", data.StatusRunning),
		makeJob("test", "lint", data.StatusSuccess),
	}
	m := newEnrichedTestModelForChecks(t, jobs)

	got := m.renderChecks()

	require.True(t, strings.Contains(got, "Awaiting Approval (1)"),
		"expected 'Awaiting Approval (1)' section, got: %q", got)
	require.True(t, strings.Contains(got, "deploy/approval"),
		"expected 'deploy/approval' job name, got: %q", got)
	require.True(t, strings.Contains(got, "test/unit"),
		"expected 'test/unit' job name, got: %q", got)
	require.True(t, strings.Contains(got, "build/compile"),
		"expected 'build/compile' job name, got: %q", got)
	require.True(t, strings.Contains(got, "test/lint"),
		"expected 'test/lint' job name, got: %q", got)
	require.True(t, strings.Contains(got, constants.ActionRequiredIcon),
		"expected ActionRequiredIcon, got: %q", got)
	require.True(t, strings.Contains(got, constants.FailureIcon),
		"expected FailureIcon, got: %q", got)
	require.True(t, strings.Contains(got, constants.WaitingIcon),
		"expected WaitingIcon, got: %q", got)
	require.True(t, strings.Contains(got, constants.SuccessIcon),
		"expected SuccessIcon, got: %q", got)
	require.False(t, strings.Contains(got, "Pending ("),
		"the old header-based 'Pending (N)' section must no longer exist, got: %q", got)

	awaitingIdx := strings.Index(got, "Awaiting Approval")
	failedIdx := strings.Index(got, "test/unit")
	require.True(t, awaitingIdx >= 0 && failedIdx >= 0 && awaitingIdx < failedIdx,
		"expected 'Awaiting Approval' section to render before failures, got: %q", got)
}

func TestGetChecksStats(t *testing.T) {
	tests := []struct {
		name string
		jobs []data.PipelineJob
		want checksStats
	}{
		{
			name: "mixed states increment each independent bucket",
			jobs: []data.PipelineJob{
				makeJob("s", "a", data.StatusManual),
				makeJob("s", "b", data.StatusManual),
				makeJob("s", "c", data.StatusSuccess),
				makeJob("s", "d", data.StatusFailed),
				makeJob("s", "e", data.StatusRunning),
				makeJob("s", "f", data.StatusPending),
				makeJob("s", "g", data.StatusSkipped),
				makeJob("s", "h", data.StatusCanceled),
			},
			want: checksStats{
				succeeded:        1,
				neutral:          1,
				failed:           1,
				skipped:          1,
				inProgress:       2,
				awaitingApproval: 2,
			},
		},
		{
			name: "no jobs yields zero stats",
			jobs: nil,
			want: checksStats{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newEnrichedTestModelForChecks(t, tt.jobs)
			got := m.getChecksStats()
			require.Equal(t, tt.want, got)
		})
	}
}

func TestViewChecksBar_NarrowWidth_NoPanic(t *testing.T) {
	jobs := []data.PipelineJob{
		makeJob("build", "compile", data.StatusFailed),
		makeJob("test", "lint", data.StatusSuccess),
		makeJob("test", "unit", data.StatusRunning),
	}
	m := newEnrichedTestModelForChecks(t, jobs)

	for _, width := range []int{0, 1, 5, 10} {
		m.width = width
		require.NotPanics(t, func() {
			m.viewChecksBar()
		}, "viewChecksBar panicked with width=%d", width)
	}
}
