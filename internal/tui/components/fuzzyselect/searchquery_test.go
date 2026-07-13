package fuzzyselect

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
)

func TestProjectPrefix(t *testing.T) {
	tests := []struct {
		name       string
		word       string
		wantPrefix string
		wantOk     bool
	}{
		{
			name:       "project prefix matches",
			word:       "project:foo",
			wantPrefix: "project:",
			wantOk:     true,
		},
		{
			name:       "repo prefix matches as synonym",
			word:       "repo:foo",
			wantPrefix: "repo:",
			wantOk:     true,
		},
		{
			name:       "author prefix does not match",
			word:       "author:foo",
			wantPrefix: "",
			wantOk:     false,
		},
		{
			name:       "label prefix does not match",
			word:       "label:foo",
			wantPrefix: "",
			wantOk:     false,
		},
		{
			name:       "empty word does not match",
			word:       "",
			wantPrefix: "",
			wantOk:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPrefix, gotOk := projectPrefix(WordInfo{Word: tt.word})
			require.Equal(t, tt.wantPrefix, gotPrefix)
			require.Equal(t, tt.wantOk, gotOk)
		})
	}
}

func TestSearchQuerySourceExtractContext_ProjectPrefix(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		cursorPos tea.Position
		want      Context
	}{
		{
			name:      "project alias prefix at end of input",
			input:     "project:@cat",
			cursorPos: tea.Position{X: len("project:@cat")},
			want: Context{
				Start:   tea.Position{X: len("project:")},
				End:     tea.Position{X: len("project:@cat")},
				Content: "@cat",
			},
		},
		{
			name:      "repo alias prefix at end of input synonym",
			input:     "repo:@cat",
			cursorPos: tea.Position{X: len("repo:@cat")},
			want: Context{
				Start:   tea.Position{X: len("repo:")},
				End:     tea.Position{X: len("repo:@cat")},
				Content: "@cat",
			},
		},
		{
			name:      "project alias prefix with preceding token",
			input:     "is:open project:@cat",
			cursorPos: tea.Position{X: len("is:open project:@cat")},
			want: Context{
				Start:   tea.Position{X: len("is:open project:")},
				End:     tea.Position{X: len("is:open project:@cat")},
				Content: "@cat",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := &SearchQuerySource{}
			got := src.ExtractContext(tt.input, tt.cursorPos)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestSearchQuerySourceExtractContext_AuthorPrefixUnchanged(t *testing.T) {
	src := &SearchQuerySource{}
	got := src.ExtractContext("author:oct", tea.Position{X: len("author:oct")})
	want := Context{
		Start:   tea.Position{X: len("author:")},
		End:     tea.Position{X: len("author:oct")},
		Content: "oct",
	}
	require.Equal(t, want, got)
}

func TestSearchQuerySourceExtractContext_LabelPrefixUnchanged(t *testing.T) {
	src := &SearchQuerySource{}
	got := src.ExtractContext("label:bu", tea.Position{X: len("label:bu")})
	want := Context{
		Start:   tea.Position{X: len("label:")},
		End:     tea.Position{X: len("label:bu")},
		Content: "bu",
	}
	require.Equal(t, want, got)
}

func TestSearchQuerySourceSuggestions_ProjectPrefix(t *testing.T) {
	src := &SearchQuerySource{
		ProjectAliases: map[string]string{
			"hector":   "luizalabs/canais-digitais/hector",
			"catalogo": "luizalabs/canais-digitais/navegacao/catalogo",
		},
	}
	want := []Suggestion{
		{Value: "@catalogo", Detail: "luizalabs/canais-digitais/navegacao/catalogo"},
		{Value: "@hector", Detail: "luizalabs/canais-digitais/hector"},
	}

	gotProject := src.Suggestions("project:@", tea.Position{X: len("project:@")})
	require.Equal(t, want, gotProject)

	gotRepo := src.Suggestions("repo:@", tea.Position{X: len("repo:@")})
	require.Equal(t, want, gotRepo)
}

func TestSearchQuerySourceSuggestions_ProjectPrefixEmptyAliases(t *testing.T) {
	tests := []struct {
		name    string
		aliases map[string]string
	}{
		{name: "nil aliases", aliases: nil},
		{name: "empty aliases", aliases: map[string]string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := &SearchQuerySource{ProjectAliases: tt.aliases}
			got := src.Suggestions("project:@", tea.Position{X: len("project:@")})
			require.Equal(t, []Suggestion{}, got)
		})
	}
}

func TestSearchQuerySourceSuggestions_AuthorPrefixUnchanged(t *testing.T) {
	src := &SearchQuerySource{
		Users: []data.User{{Login: "octocat", Name: "Octo Cat"}},
	}
	got := src.Suggestions("author:oc", tea.Position{X: len("author:oc")})
	want := []Suggestion{
		{Value: "@me", Detail: "Signed-in user"},
		{Value: "octocat", Detail: "Octo Cat"},
	}
	require.Equal(t, want, got)
}

func TestSearchQuerySourceSuggestions_LabelPrefixUnchanged(t *testing.T) {
	src := &SearchQuerySource{
		Labels: []data.Label{{Name: "bug", Description: "Something is broken"}},
	}
	got := src.Suggestions("label:bu", tea.Position{X: len("label:bu")})
	want := []Suggestion{
		{Value: "bug", Detail: "Something is broken"},
	}
	require.Equal(t, want, got)
}

func TestSearchQuerySourceInsertSuggestion_ProjectPrefix(t *testing.T) {
	src := &SearchQuerySource{}
	gotInput, gotCursor := src.InsertSuggestion(
		"project:@ta",
		"@catalogo",
		tea.Position{X: len("project:")},
		tea.Position{X: len("project:@ta")},
	)
	require.Equal(t, "project:@catalogo ", gotInput)
	require.Equal(t, tea.Position{X: len("project:@catalogo ")}, gotCursor)
}

func TestSearchQuerySourceInsertSuggestion_RepoPrefix(t *testing.T) {
	src := &SearchQuerySource{}
	gotInput, gotCursor := src.InsertSuggestion(
		"repo:@ta",
		"@catalogo",
		tea.Position{X: len("repo:")},
		tea.Position{X: len("repo:@ta")},
	)
	require.Equal(t, "repo:@catalogo ", gotInput)
	require.Equal(t, tea.Position{X: len("repo:@catalogo ")}, gotCursor)
}
