package prrow

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	graphql "github.com/cli/shurcooL-graphql"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/table"
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

func newPullRequestWithCommitState(ctx *context.ProgramContext, state string) *PullRequest {
	return &PullRequest{
		Ctx: ctx,
		Data: &Data{
			Primary: &data.PullRequestData{
				Commits: newLastCommitStatus(state),
			},
		},
	}
}

func TestGetStatusChecksRollup(t *testing.T) {
	tests := []struct {
		name     string
		pr       *PullRequest
		expected data.PipelineStatus
	}{
		{
			name:     "nil Data returns empty status",
			pr:       &PullRequest{Data: nil},
			expected: "",
		},
		{
			name:     "nil Primary returns empty status",
			pr:       &PullRequest{Data: &Data{Primary: nil}},
			expected: "",
		},
		{
			name: "empty Commits returns empty status",
			pr: &PullRequest{
				Data: &Data{
					Primary: &data.PullRequestData{
						Commits: data.LastCommitStatus{
							Nodes: []struct {
								Commit struct {
									StatusCheckRollup struct {
										State graphql.String
									}
								}
							}{},
						},
					},
				},
			},
			expected: "",
		},
		{
			name:     "lowercase success state returns success",
			pr:       newPullRequestWithCommitState(nil, "success"),
			expected: data.PipelineStatus("success"),
		},
		{
			name:     "lowercase failed state returns failed",
			pr:       newPullRequestWithCommitState(nil, "failed"),
			expected: data.PipelineStatus("failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.pr.GetStatusChecksRollup()
			if result != tt.expected {
				t.Errorf("GetStatusChecksRollup() = %v, want %v", result, tt.expected)
			}
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
		name         string
		pr           *PullRequest
		wantContains string
	}{
		{
			name:         "nil Primary renders placeholder dash",
			pr:           &PullRequest{Ctx: ctx, Data: &Data{Primary: nil}},
			wantContains: "-",
		},
		{
			name:         "lowercase success state renders success icon",
			pr:           newPullRequestWithCommitState(ctx, "success"),
			wantContains: constants.SuccessIcon,
		},
		{
			name:         "lowercase pending state renders waiting glyph",
			pr:           newPullRequestWithCommitState(ctx, "pending"),
			wantContains: ctx.Styles.Common.WaitingGlyph,
		},
		{
			name:         "lowercase running state renders waiting glyph",
			pr:           newPullRequestWithCommitState(ctx, "running"),
			wantContains: ctx.Styles.Common.WaitingGlyph,
		},
		{
			name:         "lowercase failed state renders failure icon",
			pr:           newPullRequestWithCommitState(ctx, "failed"),
			wantContains: constants.FailureIcon,
		},
		{
			name:         "empty state renders empty icon",
			pr:           newPullRequestWithCommitState(ctx, ""),
			wantContains: constants.EmptyIcon,
		},
		{
			name:         "skipped state renders empty icon",
			pr:           newPullRequestWithCommitState(ctx, "skipped"),
			wantContains: constants.EmptyIcon,
		},
		{
			name:         "manual state renders empty icon",
			pr:           newPullRequestWithCommitState(ctx, "manual"),
			wantContains: constants.EmptyIcon,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.pr.renderCiStatus()
			if !strings.Contains(result, tt.wantContains) {
				t.Errorf("renderCiStatus() = %q, want substring %q", result, tt.wantContains)
			}
		})
	}
}

func TestRenderLabels(t *testing.T) {
	tests := []struct {
		name         string
		pr           *PullRequest
		isSelected   bool
		wantContains []string
		wantNewlines int
	}{
		{
			name: "nil Data returns empty string",
			pr: &PullRequest{
				Data: nil,
				Ctx:  &context.ProgramContext{},
			},
		},
		{
			name: "nil Primary returns empty string",
			pr: &PullRequest{
				Data: &Data{Primary: nil},
				Ctx:  &context.ProgramContext{},
			},
		},
		{
			name: "empty labels returns empty string",
			pr: &PullRequest{
				Data: &Data{
					Primary: &data.PullRequestData{
						Labels: data.PRLabels{
							Nodes: []data.Label{},
						},
					},
				},
				Ctx: &context.ProgramContext{},
			},
		},
		{
			name: "single label returns non-empty string",
			pr: &PullRequest{
				Data: &Data{
					Primary: &data.PullRequestData{
						Labels: data.PRLabels{
							Nodes: []data.Label{
								{Name: "bug", Color: "FF0000"},
							},
						},
					},
				},
				Ctx: &context.ProgramContext{
					Config: &config.Config{
						Theme: &config.ThemeConfig{},
					},
				},
				Columns: []table.Column{
					{Title: constants.LabelsIcon, ComputedWidth: 20},
				},
			},
			wantContains: []string{"bug"},
		},
		{
			name: "compact labels keep overflow summary on one line",
			pr: &PullRequest{
				Data: &Data{
					Primary: &data.PullRequestData{
						Labels: data.PRLabels{
							Nodes: []data.Label{
								{Name: "bug", Color: "FF0000"},
								{Name: "fix", Color: "00FF00"},
								{Name: "chore", Color: "0000FF"},
							},
						},
					},
				},
				Ctx: &context.ProgramContext{
					Config: &config.Config{
						Theme: &config.ThemeConfig{
							Ui: config.UIThemeConfig{
								Table: config.TableUIThemeConfig{Compact: true},
							},
						},
					},
				},
				Columns: []table.Column{
					{Title: constants.LabelsIcon, ComputedWidth: 12},
				},
			},
			wantContains: []string{"bug", "fix", "+1"},
			wantNewlines: 0,
		},
		{
			name: "selected labels keep content on selected rows",
			pr: &PullRequest{
				Data: &Data{
					Primary: &data.PullRequestData{
						Labels: data.PRLabels{
							Nodes: []data.Label{
								{Name: "bug", Color: "FF0000"},
								{Name: "fix", Color: "00FF00"},
							},
						},
					},
				},
				Ctx: &context.ProgramContext{
					Config: &config.Config{
						Theme: &config.ThemeConfig{},
					},
				},
				Columns: []table.Column{
					{Title: constants.LabelsIcon, ComputedWidth: 20},
				},
			},
			isSelected:   true,
			wantContains: []string{"bug", "fix"},
			wantNewlines: 0,
		},
		{
			name: "full labels keep overflow summary across two lines",
			pr: &PullRequest{
				Data: &Data{
					Primary: &data.PullRequestData{
						Labels: data.PRLabels{
							Nodes: []data.Label{
								{Name: "bug", Color: "FF0000"},
								{Name: "fix", Color: "00FF00"},
								{Name: "chore", Color: "0000FF"},
								{Name: "feature", Color: "AAAAAA"},
							},
						},
					},
				},
				Ctx: &context.ProgramContext{
					Config: &config.Config{
						Theme: &config.ThemeConfig{},
					},
				},
				Columns: []table.Column{
					{Title: constants.LabelsIcon, ComputedWidth: 14},
				},
			},
			wantContains: []string{"bug", "fix", "chore", "+1"},
			wantNewlines: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.pr.Ctx.Theme.SelectedBackground = compat.AdaptiveColor{
				Light: lipgloss.Color("7"),
				Dark:  lipgloss.Color("7"),
			}
			result := tt.pr.renderLabels(tt.isSelected)

			// For nil/empty cases, expect empty string
			if tt.pr.Data == nil ||
				tt.pr.Data.Primary == nil ||
				len(tt.pr.Data.Primary.Labels.Nodes) == 0 {
				if result != "" {
					t.Errorf("renderLabels() = %q, want empty string", result)
				}
				return
			}

			if result == "" {
				t.Errorf(
					"renderLabels() returned empty string for %d labels",
					len(tt.pr.Data.Primary.Labels.Nodes),
				)
			}

			if strings.Count(result, "\n") != tt.wantNewlines {
				t.Errorf(
					"renderLabels() newline count = %d, want %d",
					strings.Count(result, "\n"),
					tt.wantNewlines,
				)
			}

			for _, want := range tt.wantContains {
				if !strings.Contains(result, want) {
					t.Errorf("renderLabels() = %q, want substring %q", result, want)
				}
			}
		})
	}
}
