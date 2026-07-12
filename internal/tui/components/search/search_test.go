package search

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/cmpcontroller"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

func testContext(t *testing.T) *context.ProgramContext {
	t.Helper()

	cfg, err := config.ParseConfig(config.Location{
		ConfigFlag:       "../../../config/testdata/test-config.yml",
		SkipGlobalConfig: true,
	})
	require.NoError(t, err)

	thm := theme.ParseTheme(&cfg)

	return &context.ProgramContext{
		Config:            &cfg,
		Theme:             thm,
		Styles:            context.InitStyles(thm),
		MainContentWidth:  120,
		MainContentHeight: 40,
	}
}

func TestModel_Repo(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		wantRepo  cmpcontroller.RepoRef
		wantFound bool
	}{
		{
			name:  "project prefix with simple owner and name",
			value: "project:group/proj",
			wantRepo: cmpcontroller.RepoRef{
				NameWithOwner: "group/proj",
				Owner:         "group",
				Name:          "proj",
			},
			wantFound: true,
		},
		{
			name:  "project prefix with nested subgroup owner",
			value: "project:group/subgroup/proj",
			wantRepo: cmpcontroller.RepoRef{
				NameWithOwner: "group/subgroup/proj",
				Owner:         "group/subgroup",
				Name:          "proj",
			},
			wantFound: true,
		},
		{
			name:  "project prefix mixed with other qualifiers is still found",
			value: "is:open project:group/proj author:@me",
			wantRepo: cmpcontroller.RepoRef{
				NameWithOwner: "group/proj",
				Owner:         "group",
				Name:          "proj",
			},
			wantFound: true,
		},
		{
			name:      "old repo prefix syntax is no longer recognized",
			value:     "repo:group/proj",
			wantRepo:  cmpcontroller.RepoRef{},
			wantFound: false,
		},
		{
			name:      "no project token present",
			value:     "is:open author:@me",
			wantRepo:  cmpcontroller.RepoRef{},
			wantFound: false,
		},
		{
			name:      "empty search value",
			value:     "",
			wantRepo:  cmpcontroller.RepoRef{},
			wantFound: false,
		},
		{
			name:      "project prefix without a slash separator is not a valid repo",
			value:     "project:noslash",
			wantRepo:  cmpcontroller.RepoRef{},
			wantFound: false,
		},
	}

	ctx := testContext(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewModel(ctx, SearchOptions{})
			m.SetValue(tt.value)

			repo, found := m.Repo()

			require.Equal(t, tt.wantFound, found)
			require.Equal(t, tt.wantRepo, repo)
		})
	}
}

func TestModel_Repo_InitialValueIsHonoredWithoutSetValue(t *testing.T) {
	ctx := testContext(t)

	m := NewModel(ctx, SearchOptions{InitialValue: "project:group/proj is:open"})

	repo, found := m.Repo()

	require.True(t, found)
	require.Equal(t, cmpcontroller.RepoRef{
		NameWithOwner: "group/proj",
		Owner:         "group",
		Name:          "proj",
	}, repo)
}
