package prview

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

func newTestModelForCommits(t *testing.T) Model {
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
	return m
}

func TestCommitStateSign(t *testing.T) {
	m := newTestModelForCommits(t)

	tests := []struct {
		name  string
		state data.PipelineStatus
		want  string
	}{
		{
			"failed status returns the failure glyph",
			data.StatusFailed,
			m.ctx.Styles.Common.FailureGlyph,
		},
		{
			"pending status returns the waiting glyph",
			data.StatusPending,
			m.ctx.Styles.Common.WaitingGlyph,
		},
		{
			"running status returns the waiting glyph",
			data.StatusRunning,
			m.ctx.Styles.Common.WaitingGlyph,
		},
		{
			"success status returns the success glyph",
			data.StatusSuccess,
			m.ctx.Styles.Common.SuccessGlyph,
		},
		{"empty status returns an empty string", data.PipelineStatus(""), ""},
		{"skipped status returns an empty string", data.StatusSkipped, ""},
		{"canceled status returns an empty string", data.StatusCanceled, ""},
		{"manual status returns an empty string", data.StatusManual, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.commitStateSign(tt.state)
			require.Equal(t, tt.want, got)
		})
	}
}
