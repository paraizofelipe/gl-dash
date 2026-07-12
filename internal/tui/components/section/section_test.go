package section

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/git"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prompt"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/search"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/table"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

func currentRepoFilter(t *testing.T, repo git.RemoteRepo) string {
	t.Helper()
	t.Setenv("GH_REPO", fmt.Sprintf("https://github.com/%s/%s", repo.Owner, repo.Name))
	return fmt.Sprintf("project:%s/%s", repo.Owner, repo.Name)
}

func TestHasRepoNameInConfiguredFilter(t *testing.T) {
	repo := git.RemoteRepo{Owner: "dlvhdr", Name: "gh-dash"}
	repoFilter := currentRepoFilter(t, repo)

	tests := []struct {
		name        string
		searchValue string
		want        bool
	}{
		{
			name:        "no repo filter",
			searchValue: "is:open author:@me",
			want:        false,
		},
		{
			name:        "has current repo filter",
			searchValue: repoFilter + " is:open",
			want:        true,
		},
		{
			name:        "has different project filter",
			searchValue: "project:other/repo is:open",
			want:        true,
		},
		{
			name:        "empty search value",
			searchValue: "",
			want:        false,
		},
		{
			name:        "repo filter with similar prefix",
			searchValue: repoFilter + "-extra is:open",
			want:        true,
		},
		{
			name:        "old repo prefix syntax is no longer recognized",
			searchValue: "repo:other/repo is:open",
			want:        false,
		},
		{
			name:        "nested subgroup project filter is recognized",
			searchValue: "project:group/subgroup/proj is:open",
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := BaseModel{SearchValue: tt.searchValue}
			require.Equal(t, tt.want, m.HasRepoNameInConfiguredFilter())
		})
	}
}

func TestHasCurrentRepoNameInConfiguredFilter(t *testing.T) {
	repo := git.RemoteRepo{Owner: "dlvhdr", Name: "gh-dash"}
	repoFilter := currentRepoFilter(t, repo)
	oldSyntaxCurrentRepoFilter := fmt.Sprintf("repo:%s/%s", repo.Owner, repo.Name)

	tests := []struct {
		name        string
		searchValue string
		want        bool
	}{
		{
			name:        "no repo filter",
			searchValue: "is:open author:@me",
			want:        false,
		},
		{
			name:        "has current repo filter",
			searchValue: repoFilter + " is:open",
			want:        true,
		},
		{
			name:        "has different project filter",
			searchValue: "project:other/repo is:open",
			want:        false,
		},
		{
			name:        "current repo filter with extra suffix does not match",
			searchValue: repoFilter + "-extra is:open",
			want:        false,
		},
		{
			name:        "current repo filter alone",
			searchValue: repoFilter,
			want:        true,
		},
		{
			name:        "empty search value",
			searchValue: "",
			want:        false,
		},
		{
			name:        "multiple project filters including current",
			searchValue: "project:other/repo " + repoFilter + " is:open",
			want:        true,
		},
		{
			name:        "old repo prefix syntax for current repo is not recognized as a match",
			searchValue: oldSyntaxCurrentRepoFilter + " is:open",
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := BaseModel{SearchValue: tt.searchValue}
			m.Ctx = &context.ProgramContext{
				GHRepo: &repo,
			}
			require.Equal(t, tt.want, m.HasCurrentRepoNameInConfiguredFilter())
		})
	}
}

func TestHasCurrentRepoNameInConfiguredFilter_NestedSubgroup(t *testing.T) {
	repo := git.RemoteRepo{Owner: "group/subgroup", Name: "proj"}
	repoFilter := currentRepoFilter(t, repo)

	tests := []struct {
		name        string
		searchValue string
		want        bool
	}{
		{
			name:        "nested subgroup current project filter matches",
			searchValue: repoFilter + " is:open",
			want:        true,
		},
		{
			name:        "nested subgroup project filter alone matches",
			searchValue: repoFilter,
			want:        true,
		},
		{
			name:        "unrelated nested subgroup filter does not match",
			searchValue: "project:group/other-subgroup/proj is:open",
			want:        false,
		},
		{
			name:        "no filter does not match",
			searchValue: "is:open author:@me",
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := BaseModel{SearchValue: tt.searchValue}
			m.Ctx = &context.ProgramContext{
				GHRepo: &repo,
			}
			require.Equal(t, tt.want, m.HasCurrentRepoNameInConfiguredFilter())
		})
	}
}

func TestSyncSmartFilterWithSearchValue(t *testing.T) {
	repo := git.RemoteRepo{Owner: "dlvhdr", Name: "gh-dash"}
	repoFilter := currentRepoFilter(t, repo)

	tests := []struct {
		name        string
		searchValue string
		wantFlag    bool
	}{
		{
			name:        "search contains current repo filter",
			searchValue: repoFilter + " is:open",
			wantFlag:    true,
		},
		{
			name:        "search does not contain current repo filter",
			searchValue: "is:open author:@me",
			wantFlag:    false,
		},
		{
			name:        "search contains different project filter",
			searchValue: "project:other/repo is:open",
			wantFlag:    false,
		},
		{
			name:        "similar repo name does not set flag",
			searchValue: repoFilter + "-extra is:open",
			wantFlag:    false,
		},
		{
			name:        "old repo prefix syntax does not set flag",
			searchValue: "repo:dlvhdr/gh-dash is:open",
			wantFlag:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := BaseModel{
				SearchValue:               tt.searchValue,
				IsFilteredByCurrentRemote: !tt.wantFlag,
			}
			m.Ctx = &context.ProgramContext{
				GHRepo: &repo,
			}
			m.SyncSmartFilterWithSearchValue()
			require.Equal(t, tt.wantFlag, m.IsFilteredByCurrentRemote)
		})
	}
}

func TestGetSearchValue(t *testing.T) {
	repo := git.RemoteRepo{Owner: "dlvhdr", Name: "gh-dash"}
	repoFilter := currentRepoFilter(t, repo)

	tests := []struct {
		name                      string
		searchValue               string
		isFilteredByCurrentRemote bool
		wantContainsRepoFilter    bool
		wantContainsOtherFilters  bool
	}{
		{
			name:                      "smart filter on adds repo filter",
			searchValue:               "is:open author:@me",
			isFilteredByCurrentRemote: true,
			wantContainsRepoFilter:    true,
			wantContainsOtherFilters:  true,
		},
		{
			name:                      "smart filter off does not add repo filter",
			searchValue:               "is:open author:@me",
			isFilteredByCurrentRemote: false,
			wantContainsRepoFilter:    false,
			wantContainsOtherFilters:  true,
		},
		{
			name:                      "smart filter on with repo already present does not duplicate",
			searchValue:               repoFilter + " is:open",
			isFilteredByCurrentRemote: true,
			wantContainsRepoFilter:    true,
			wantContainsOtherFilters:  true,
		},
		{
			name:                      "similar repo name is preserved when smart filter is on",
			searchValue:               repoFilter + "-extra is:open",
			isFilteredByCurrentRemote: true,
			wantContainsRepoFilter:    true,
		},
		{
			name:                      "similar repo name is preserved when smart filter is off",
			searchValue:               repoFilter + "-extra is:open",
			isFilteredByCurrentRemote: false,
			wantContainsRepoFilter:    false,
		},
		{
			name:                      "empty search value with smart filter on",
			searchValue:               "",
			isFilteredByCurrentRemote: true,
			wantContainsRepoFilter:    true,
		},
		{
			name:                      "empty search value with smart filter off",
			searchValue:               "",
			isFilteredByCurrentRemote: false,
			wantContainsRepoFilter:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := BaseModel{
				SearchValue:               tt.searchValue,
				IsFilteredByCurrentRemote: tt.isFilteredByCurrentRemote,
			}
			m.Ctx = &context.ProgramContext{
				GHRepo: &repo,
			}

			got := m.GetSearchValue()

			hasExactRepoFilter := false
			for token := range strings.FieldsSeq(got) {
				if token == repoFilter {
					hasExactRepoFilter = true
					break
				}
			}
			require.Equal(
				t,
				tt.wantContainsRepoFilter,
				hasExactRepoFilter,
				"GetSearchValue() = %q, expected repo filter present = %v",
				got,
				tt.wantContainsRepoFilter,
			)

			if tt.wantContainsOtherFilters {
				require.Contains(t, got, "is:open")
			}
		})
	}
}

func TestGetSearchValue_NestedSubgroup(t *testing.T) {
	repo := git.RemoteRepo{Owner: "group/subgroup", Name: "proj"}
	repoFilter := currentRepoFilter(t, repo)

	tests := []struct {
		name                      string
		searchValue               string
		isFilteredByCurrentRemote bool
		wantContainsRepoFilter    bool
	}{
		{
			name:                      "smart filter on adds nested subgroup project filter",
			searchValue:               "is:open author:@me",
			isFilteredByCurrentRemote: true,
			wantContainsRepoFilter:    true,
		},
		{
			name:                      "smart filter off does not add nested subgroup project filter",
			searchValue:               "is:open author:@me",
			isFilteredByCurrentRemote: false,
			wantContainsRepoFilter:    false,
		},
		{
			name:                      "nested subgroup project filter already present is not duplicated",
			searchValue:               repoFilter + " is:open",
			isFilteredByCurrentRemote: true,
			wantContainsRepoFilter:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := BaseModel{
				SearchValue:               tt.searchValue,
				IsFilteredByCurrentRemote: tt.isFilteredByCurrentRemote,
			}
			m.Ctx = &context.ProgramContext{
				GHRepo: &repo,
			}

			got := m.GetSearchValue()

			hasExactRepoFilter := false
			matchingTokenCount := 0
			for token := range strings.FieldsSeq(got) {
				if token == repoFilter {
					hasExactRepoFilter = true
					matchingTokenCount++
				}
			}
			require.Equal(
				t,
				tt.wantContainsRepoFilter,
				hasExactRepoFilter,
				"GetSearchValue() = %q, expected nested subgroup project filter present = %v",
				got,
				tt.wantContainsRepoFilter,
			)

			if tt.wantContainsRepoFilter {
				require.Equal(
					t,
					1,
					matchingTokenCount,
					"GetSearchValue() = %q, nested subgroup project filter should not be duplicated",
					got,
				)
			}
			require.Contains(t, got, "is:open")
		})
	}
}

func TestGetSearchValue_ProjectFilterAlreadyPresentIsNotDuplicated(t *testing.T) {
	tests := []struct {
		name string
		repo git.RemoteRepo
	}{
		{
			name: "simple owner and name",
			repo: git.RemoteRepo{Owner: "dlvhdr", Name: "gh-dash"},
		},
		{
			name: "nested subgroup owner",
			repo: git.RemoteRepo{Owner: "group/subgroup", Name: "proj"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := tt.repo
			repoFilter := currentRepoFilter(t, repo)

			m := BaseModel{
				SearchValue:               repoFilter + " is:open",
				IsFilteredByCurrentRemote: true,
			}
			m.Ctx = &context.ProgramContext{
				GHRepo: &repo,
			}

			got := m.GetSearchValue()

			require.Equal(t, repoFilter+" is:open", got)
		})
	}
}

func TestGetSearchValue_OldRepoPrefixNotStripped(t *testing.T) {
	repo := git.RemoteRepo{Owner: "dlvhdr", Name: "gh-dash"}
	repoFilter := currentRepoFilter(t, repo)
	oldSyntaxFilter := fmt.Sprintf("repo:%s/%s", repo.Owner, repo.Name)

	m := BaseModel{
		SearchValue:               oldSyntaxFilter + " is:open",
		IsFilteredByCurrentRemote: true,
	}
	m.Ctx = &context.ProgramContext{
		GHRepo: &repo,
	}

	got := m.GetSearchValue()

	require.Contains(t, got, oldSyntaxFilter,
		"old repo: syntax token is unrelated to the project: prefix and must be left untouched")
	require.Contains(t, got, repoFilter,
		"new project: filter for the current repo must still be added alongside the old token")
}

func TestGetSearchValue_SimilarRepoNameNotStripped(t *testing.T) {
	repo := git.RemoteRepo{Owner: "dlvhdr", Name: "gh-dash"}
	repoFilter := currentRepoFilter(t, repo)
	similarRepo := repoFilter + "-extra"

	m := BaseModel{
		SearchValue:               similarRepo + " is:open",
		IsFilteredByCurrentRemote: false,
	}
	m.Ctx = &context.ProgramContext{
		GHRepo: &repo,
	}

	got := m.GetSearchValue()

	require.Contains(t, got, similarRepo,
		"similar repo name should not be stripped from search value")
}

func TestGetSearchValue_ManualRepoFilterRemoval(t *testing.T) {
	repo := git.RemoteRepo{Owner: "dlvhdr", Name: "gh-dash"}
	repoFilter := currentRepoFilter(t, repo)

	tests := []struct {
		name                      string
		configFilters             string
		searchValue               string
		isFilteredByCurrentRemote bool
		wantContainsRepoFilter    bool
	}{
		{
			name:                      "smart filtering on, repo filter in search value",
			configFilters:             "is:open author:@me",
			searchValue:               repoFilter + " is:open author:@me",
			isFilteredByCurrentRemote: true,
			wantContainsRepoFilter:    true,
		},
		{
			name:                      "smart filtering off via toggle, repo filter not in search value",
			configFilters:             "is:open author:@me",
			searchValue:               "is:open author:@me",
			isFilteredByCurrentRemote: false,
			wantContainsRepoFilter:    false,
		},
		{
			name:                      "user manually removed repo filter from search bar",
			configFilters:             "is:open author:@me",
			searchValue:               "is:open author:@me",
			isFilteredByCurrentRemote: true,
			wantContainsRepoFilter:    false,
		},
		{
			name:                      "user replaced repo filter with a different project",
			configFilters:             "is:open author:@me",
			searchValue:               "project:other/repo is:open author:@me",
			isFilteredByCurrentRemote: true,
			wantContainsRepoFilter:    false,
		},
		{
			name:                      "config already has repo filter, search value unchanged",
			configFilters:             repoFilter + " is:open",
			searchValue:               repoFilter + " is:open",
			isFilteredByCurrentRemote: false,
			wantContainsRepoFilter:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := BaseModel{
				Config:                    config.SectionConfig{Filters: tt.configFilters},
				SearchValue:               tt.searchValue,
				IsFilteredByCurrentRemote: tt.isFilteredByCurrentRemote,
			}
			m.Ctx = &context.ProgramContext{
				GHRepo: &repo,
			}

			m.SyncSmartFilterWithSearchValue()
			got := m.GetSearchValue()

			containsRepoFilter := false
			for token := range strings.FieldsSeq(got) {
				if token == repoFilter {
					containsRepoFilter = true
					break
				}
			}

			require.Equal(t, tt.wantContainsRepoFilter, containsRepoFilter,
				"GetSearchValue() = %q, contains %q = %v, want %v",
				got, repoFilter, containsRepoFilter, tt.wantContainsRepoFilter)
		})
	}
}

func TestGetConfigFiltersWithCurrentRemoteAdded(t *testing.T) {
	repo := git.RemoteRepo{Owner: "dlvhdr", Name: "gh-dash"}
	repoFilter := currentRepoFilter(t, repo)

	tests := []struct {
		name                   string
		filters                string
		smartFilteringAtLaunch bool
		wantContainsRepoFilter bool
	}{
		{
			name:                   "smart filtering enabled, no repo in config",
			filters:                "is:open author:@me",
			smartFilteringAtLaunch: true,
			wantContainsRepoFilter: true,
		},
		{
			name:                   "smart filtering disabled, no repo in config",
			filters:                "is:open author:@me",
			smartFilteringAtLaunch: false,
			wantContainsRepoFilter: false,
		},
		{
			name:                   "smart filtering enabled, repo already in config",
			filters:                repoFilter + " is:open",
			smartFilteringAtLaunch: true,
			wantContainsRepoFilter: true,
		},
		{
			name:                   "smart filtering enabled, different project in config",
			filters:                "project:other/repo is:open",
			smartFilteringAtLaunch: true,
			wantContainsRepoFilter: false,
		},
		{
			name:                   "smart filtering enabled, old repo prefix syntax in config is not recognized so project filter is injected anyway",
			filters:                "repo:other/repo is:open",
			smartFilteringAtLaunch: true,
			wantContainsRepoFilter: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := NewSectionOptions{
				Config: config.SectionConfig{Filters: tt.filters},
			}
			ctx := &context.ProgramContext{
				Config: &config.Config{
					SmartFilteringAtLaunch: tt.smartFilteringAtLaunch,
				},
				GHRepo: &repo,
			}

			got := options.GetConfigFiltersWithCurrentRemoteAdded(ctx)

			hasRepoFilter := false
			for token := range strings.FieldsSeq(got) {
				if token == repoFilter {
					hasRepoFilter = true
					break
				}
			}

			require.Equal(t, tt.wantContainsRepoFilter, hasRepoFilter,
				"GetConfigFiltersWithCurrentRemoteAdded() = %q, expected repo filter = %v",
				got, tt.wantContainsRepoFilter)

			require.Contains(t, got, "is:open",
				"original filters should be preserved")
		})
	}
}

func TestGetConfigFiltersWithCurrentRemoteAdded_UsesProjectPrefixNotRepoPrefix(t *testing.T) {
	repo := git.RemoteRepo{Owner: "dlvhdr", Name: "gh-dash"}

	options := NewSectionOptions{
		Config: config.SectionConfig{Filters: ""},
	}
	ctx := &context.ProgramContext{
		Config: &config.Config{
			SmartFilteringAtLaunch: true,
		},
		GHRepo: &repo,
	}

	got := options.GetConfigFiltersWithCurrentRemoteAdded(ctx)

	require.Contains(t, got, "project:dlvhdr/gh-dash")
	require.NotContains(t, got, "repo:dlvhdr/gh-dash")
}

func TestGetConfigFiltersWithCurrentRemoteAdded_NestedSubgroup(t *testing.T) {
	repo := git.RemoteRepo{Owner: "group/subgroup", Name: "proj"}
	repoFilter := currentRepoFilter(t, repo)

	tests := []struct {
		name                   string
		filters                string
		smartFilteringAtLaunch bool
		wantContainsRepoFilter bool
	}{
		{
			name:                   "smart filtering enabled, no project in config",
			filters:                "is:open author:@me",
			smartFilteringAtLaunch: true,
			wantContainsRepoFilter: true,
		},
		{
			name:                   "smart filtering disabled, no project in config",
			filters:                "is:open author:@me",
			smartFilteringAtLaunch: false,
			wantContainsRepoFilter: false,
		},
		{
			name:                   "smart filtering enabled, nested subgroup project already in config",
			filters:                repoFilter + " is:open",
			smartFilteringAtLaunch: true,
			wantContainsRepoFilter: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := NewSectionOptions{
				Config: config.SectionConfig{Filters: tt.filters},
			}
			ctx := &context.ProgramContext{
				Config: &config.Config{
					SmartFilteringAtLaunch: tt.smartFilteringAtLaunch,
				},
				GHRepo: &repo,
			}

			got := options.GetConfigFiltersWithCurrentRemoteAdded(ctx)

			hasRepoFilter := false
			for token := range strings.FieldsSeq(got) {
				if token == repoFilter {
					hasRepoFilter = true
					break
				}
			}

			require.Equal(
				t,
				tt.wantContainsRepoFilter,
				hasRepoFilter,
				"GetConfigFiltersWithCurrentRemoteAdded() = %q, expected nested subgroup project filter = %v",
				got,
				tt.wantContainsRepoFilter,
			)

			require.Contains(t, got, "is:open",
				"original filters should be preserved")
		})
	}
}

func TestGetConfigFiltersWithCurrentRemoteAdded_IdempotentWithProjectPrefix(t *testing.T) {
	tests := []struct {
		name string
		repo git.RemoteRepo
	}{
		{
			name: "simple owner and name",
			repo: git.RemoteRepo{Owner: "dlvhdr", Name: "gh-dash"},
		},
		{
			name: "nested subgroup owner",
			repo: git.RemoteRepo{Owner: "group/subgroup", Name: "proj"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := tt.repo
			repoFilter := currentRepoFilter(t, repo)

			options := NewSectionOptions{
				Config: config.SectionConfig{Filters: repoFilter + " is:open"},
			}
			ctx := &context.ProgramContext{
				Config: &config.Config{
					SmartFilteringAtLaunch: true,
				},
				GHRepo: &repo,
			}

			got := options.GetConfigFiltersWithCurrentRemoteAdded(ctx)

			require.Equal(t, repoFilter+" is:open", got)
		})
	}
}

func TestNewModel_IsFilteredByCurrentRemote(t *testing.T) {
	cfg, err := config.ParseConfig(config.Location{
		ConfigFlag:       "../../../config/testdata/test-config.yml",
		SkipGlobalConfig: true,
	})
	require.NoError(t, err)
	cfg.SmartFilteringAtLaunch = false

	thm := theme.ParseTheme(&cfg)
	styles := context.InitStyles(thm)

	tests := []struct {
		name    string
		repo    git.RemoteRepo
		filters string
		want    bool
	}{
		{
			name:    "filters contain current project filter",
			repo:    git.RemoteRepo{Owner: "dlvhdr", Name: "gh-dash"},
			filters: "project:dlvhdr/gh-dash is:open",
			want:    true,
		},
		{
			name:    "filters do not contain current project filter",
			repo:    git.RemoteRepo{Owner: "dlvhdr", Name: "gh-dash"},
			filters: "is:open author:@me",
			want:    false,
		},
		{
			name:    "filters contain old repo prefix syntax which is not recognized",
			repo:    git.RemoteRepo{Owner: "dlvhdr", Name: "gh-dash"},
			filters: "repo:dlvhdr/gh-dash is:open",
			want:    false,
		},
		{
			name:    "filters contain nested subgroup current project filter",
			repo:    git.RemoteRepo{Owner: "group/subgroup", Name: "proj"},
			filters: "project:group/subgroup/proj is:open",
			want:    true,
		},
		{
			name:    "filters contain unrelated nested subgroup project filter",
			repo:    git.RemoteRepo{Owner: "group/subgroup", Name: "proj"},
			filters: "project:group/other-subgroup/proj is:open",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := tt.repo
			ctx := &context.ProgramContext{
				Config:            &cfg,
				GHRepo:            &repo,
				Theme:             thm,
				Styles:            styles,
				MainContentWidth:  120,
				MainContentHeight: 40,
			}

			m := NewModel(ctx, NewSectionOptions{
				Config: config.SectionConfig{Filters: tt.filters},
				Type:   "pr",
			})

			require.Equal(t, tt.want, m.IsFilteredByCurrentRemote)
		})
	}
}

func TestGetPromptConfirmation(t *testing.T) {
	tests := []struct {
		name         string
		action       string
		view         config.ViewType
		wantNonEmpty bool
	}{
		{
			name:         "done_all in notifications view shows confirmation",
			action:       "done_all",
			view:         config.NotificationsView,
			wantNonEmpty: true,
		},
		{
			name:         "close in PRs view shows confirmation",
			action:       "close",
			view:         config.PRsView,
			wantNonEmpty: true,
		},
		{
			name:         "merge in PRs view shows confirmation",
			action:       "merge",
			view:         config.PRsView,
			wantNonEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &context.ProgramContext{
				View: tt.view,
			}
			m := BaseModel{
				IsPromptConfirmationShown: true,
				PromptConfirmationAction:  tt.action,
				PromptConfirmationBox:     prompt.NewModel(ctx),
			}
			m.Ctx = ctx

			result := m.GetPromptConfirmation()
			if tt.wantNonEmpty {
				require.NotEmpty(
					t,
					result,
					"GetPromptConfirmation() should return non-empty for action %q in view %v",
					tt.action,
					tt.view,
				)
			}
		})
	}
}

func TestViewRendersAtMainContentWidth(t *testing.T) {
	cfg, err := config.ParseConfig(config.Location{
		ConfigFlag:       "../../../config/testdata/test-config.yml",
		SkipGlobalConfig: true,
	})
	require.NoError(t, err)

	thm := theme.ParseTheme(&cfg)
	styles := context.InitStyles(thm)

	widths := []int{80, 120, 200}
	for _, targetWidth := range widths {
		t.Run(fmt.Sprintf("width_%d", targetWidth), func(t *testing.T) {
			ctx := &context.ProgramContext{
				Config:            &cfg,
				MainContentWidth:  targetWidth,
				MainContentHeight: 20,
				Theme:             thm,
				Styles:            styles,
			}
			m := BaseModel{
				Ctx:       ctx,
				SearchBar: search.NewModel(ctx, search.SearchOptions{}),
				Table: table.NewModel(
					*ctx,
					constants.Dimensions{Width: targetWidth, Height: 10},
					time.Now(),
					time.Now(),
					nil,
					nil,
					"pr",
					nil,
					"Loading...",
					false,
				),
			}

			view := m.View()
			lines := strings.Split(view, "\n")
			for i, line := range lines {
				w := lipgloss.Width(line)
				require.Equal(t, targetWidth, w,
					"line %d rendered at width %d, expected %d", i, w, targetWidth)
			}
		})
	}
}
