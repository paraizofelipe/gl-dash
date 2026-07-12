package branch

import (
	"strings"
	"testing"

	graphql "github.com/cli/shurcooL-graphql"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

func newLastCommitStatus(state string) data.LastCommitStatus {
	return data.LastCommitStatus{
		Nodes: []struct {
			Commit struct {
				StatusCheckRollup struct {
					State graphql.String
				}
			}
		}{
			{
				Commit: struct {
					StatusCheckRollup struct {
						State graphql.String
					}
				}{
					StatusCheckRollup: struct {
						State graphql.String
					}{
						State: graphql.String(state),
					},
				},
			},
		},
	}
}

func newBranchWithCommitState(ctx *context.ProgramContext, state string) *Branch {
	return &Branch{
		Ctx: ctx,
		PR: &data.PullRequestData{
			Commits: newLastCommitStatus(state),
		},
	}
}

func TestGetStatusChecksRollup(t *testing.T) {
	tests := []struct {
		name     string
		b        *Branch
		expected data.PipelineStatus
	}{
		{
			name:     "nil PR returns empty status",
			b:        &Branch{PR: nil},
			expected: "",
		},
		{
			name:     "empty Commits returns empty status",
			b:        &Branch{PR: &data.PullRequestData{}},
			expected: "",
		},
		{
			name:     "lowercase success state returns success",
			b:        newBranchWithCommitState(nil, "success"),
			expected: data.PipelineStatus("success"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result any
			require.NotPanics(t, func() {
				result = tt.b.GetStatusChecksRollup()
			})
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestRenderCiStatus(t *testing.T) {
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

	tests := []struct {
		name            string
		b               *Branch
		wantContains    string
		wantNotContains string
	}{
		{
			name:         "nil PR renders placeholder dash",
			b:            &Branch{Ctx: ctx, PR: nil},
			wantContains: "-",
		},
		{
			name:         "lowercase success state renders success icon",
			b:            newBranchWithCommitState(ctx, "success"),
			wantContains: constants.SuccessIcon,
		},
		{
			name:         "lowercase running state renders waiting glyph",
			b:            newBranchWithCommitState(ctx, "running"),
			wantContains: ctx.Styles.Common.WaitingGlyph,
		},
		{
			name:         "lowercase failed state renders failure icon",
			b:            newBranchWithCommitState(ctx, "failed"),
			wantContains: constants.FailureIcon,
		},
		{
			name:            "lowercase pending state renders waiting glyph not failure icon",
			b:               newBranchWithCommitState(ctx, "pending"),
			wantContains:    ctx.Styles.Common.WaitingGlyph,
			wantNotContains: constants.FailureIcon,
		},
		{
			name:            "PR with empty commits renders empty icon not failure icon",
			b:               &Branch{Ctx: ctx, PR: &data.PullRequestData{}},
			wantContains:    constants.EmptyIcon,
			wantNotContains: constants.FailureIcon,
		},
		{
			name:            "skipped state renders empty icon not failure icon",
			b:               newBranchWithCommitState(ctx, "skipped"),
			wantContains:    constants.EmptyIcon,
			wantNotContains: constants.FailureIcon,
		},
		{
			name:            "canceled state renders empty icon not failure icon",
			b:               newBranchWithCommitState(ctx, "canceled"),
			wantContains:    constants.EmptyIcon,
			wantNotContains: constants.FailureIcon,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result string
			require.NotPanics(t, func() {
				result = tt.b.renderCiStatus()
			})

			require.True(
				t,
				strings.Contains(result, tt.wantContains),
				"renderCiStatus() = %q, want substring %q",
				result,
				tt.wantContains,
			)

			if tt.wantNotContains != "" {
				require.False(
					t,
					strings.Contains(result, tt.wantNotContains),
					"renderCiStatus() = %q must not contain %q",
					result,
					tt.wantNotContains,
				)
			}
		})
	}
}
