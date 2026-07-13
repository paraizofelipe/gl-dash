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
		{
			name:      "unexpanded alias with slash and remainder is not a valid repo",
			value:     "project:@catalogo/taz",
			wantRepo:  cmpcontroller.RepoRef{},
			wantFound: false,
		},
		{
			name:      "unexpanded alias without remainder is not a valid repo",
			value:     "project:@catalogo",
			wantRepo:  cmpcontroller.RepoRef{},
			wantFound: false,
		},
		{
			name:      "unexpanded alias mixed with other qualifiers is not a valid repo",
			value:     "is:open project:@catalogo/taz author:@me",
			wantRepo:  cmpcontroller.RepoRef{},
			wantFound: false,
		},
		{
			name:  "unexpanded alias token is skipped in favor of a later literal project token",
			value: "project:@catalogo/taz project:dlvhdr/gh-dash",
			wantRepo: cmpcontroller.RepoRef{
				NameWithOwner: "dlvhdr/gh-dash",
				Owner:         "dlvhdr",
				Name:          "gh-dash",
			},
			wantFound: true,
		},
		{
			name:  "already expanded literal path with nested subgroups is still found",
			value: "project:luizalabs/canais-digitais/navegacao/catalogo/taz",
			wantRepo: cmpcontroller.RepoRef{
				NameWithOwner: "luizalabs/canais-digitais/navegacao/catalogo/taz",
				Owner:         "luizalabs/canais-digitais/navegacao/catalogo",
				Name:          "taz",
			},
			wantFound: true,
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

func TestModel_Focus_PopulatesProjectAliasSuggestions(t *testing.T) {
	ctx := testContext(t)
	ctx.Config.ProjectAliases = map[string]string{
		"catalogo": "luizalabs/canais-digitais/navegacao/catalogo",
	}

	m := NewModel(ctx, SearchOptions{})
	m.Focus()
	m.SetValue("project:@")
	m.CursorEnd()
	m.cmpctl.Filter()
	m.cmpctl.ShowCompletions()

	view := m.ViewCompletions()

	require.Contains(t, view, "@catalogo")
}

func TestModel_Focus_NoProjectAliasesConfiguredShowsNoAliasSuggestions(t *testing.T) {
	ctx := testContext(t)
	ctx.Config.ProjectAliases = map[string]string{}

	m := NewModel(ctx, SearchOptions{})
	m.Focus()
	m.SetValue("project:@")
	m.CursorEnd()
	m.cmpctl.Filter()
	m.cmpctl.ShowCompletions()

	view := m.ViewCompletions()

	require.NotContains(t, view, "@catalogo")
}

func TestModel_Focus_NilConfigDoesNotPanic(t *testing.T) {
	ctx := testContext(t)
	ctx.Config = nil

	m := NewModel(ctx, SearchOptions{})

	require.NotPanics(t, func() {
		m.Focus()
	})
}
