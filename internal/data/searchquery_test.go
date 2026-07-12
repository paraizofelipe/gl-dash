package data

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTranslateSearchQuery(t *testing.T) {
	tests := []struct {
		name            string
		query           string
		currentUsername string
		want            TranslatedQuery
	}{
		{
			name:            "is open and author me resolves username and state",
			query:           "is:open author:@me",
			currentUsername: "jdoe",
			want: TranslatedQuery{
				OrderBy:        "updated_at",
				Sort:           "desc",
				State:          "opened",
				AuthorUsername: "jdoe",
			},
		},
		{
			name:  "multiple label qualifiers accumulate in order",
			query: "label:bug label:p1",
			want: TranslatedQuery{
				OrderBy: "updated_at",
				Sort:    "desc",
				Labels:  []string{"bug", "p1"},
			},
		},
		{
			name:  "project and head qualifiers are captured",
			query: "project:group/proj head:my-branch",
			want: TranslatedQuery{
				OrderBy:      "updated_at",
				Sort:         "desc",
				ProjectPath:  "group/proj",
				SourceBranch: "my-branch",
			},
		},
		{
			name:  "repo is an alias of project",
			query: "repo:group/proj",
			want: TranslatedQuery{
				OrderBy:     "updated_at",
				Sort:        "desc",
				ProjectPath: "group/proj",
			},
		},
		{
			name:            "involves is unsupported and resolved while not author also resolves",
			query:           "involves:@me -author:@me",
			currentUsername: "jdoe",
			want: TranslatedQuery{
				OrderBy:           "updated_at",
				Sort:              "desc",
				NotAuthorUsername: "jdoe",
				Unsupported:       []string{"involves:jdoe"},
			},
		},
		{
			name:  "owner qualifier is treated as unsupported",
			query: "owner:dlvhdr",
			want: TranslatedQuery{
				OrderBy:     "updated_at",
				Sort:        "desc",
				Unsupported: []string{"owner:dlvhdr"},
			},
		},
		{
			name:  "updated qualifier is treated as unsupported",
			query: "updated:>=2026-01-01",
			want: TranslatedQuery{
				OrderBy:     "updated_at",
				Sort:        "desc",
				Unsupported: []string{"updated:>=2026-01-01"},
			},
		},
		{
			name:  "empty query returns only the default ordering",
			query: "",
			want: TranslatedQuery{
				OrderBy: "updated_at",
				Sort:    "desc",
			},
		},
		{
			name:  "archived and sort qualifiers are silently discarded",
			query: "archived:false sort:updated",
			want: TranslatedQuery{
				OrderBy: "updated_at",
				Sort:    "desc",
			},
		},
		{
			name:            "assignee me and is open are both captured",
			query:           "assignee:@me is:open",
			currentUsername: "jdoe",
			want: TranslatedQuery{
				OrderBy:          "updated_at",
				Sort:             "desc",
				AssigneeUsername: "jdoe",
				State:            "opened",
			},
		},
		{
			name:            "review requested me resolves the reviewer username",
			query:           "review-requested:@me",
			currentUsername: "jdoe",
			want: TranslatedQuery{
				OrderBy:          "updated_at",
				Sort:             "desc",
				ReviewerUsername: "jdoe",
			},
		},
		{
			name:            "is closed maps to the closed state",
			query:           "is:closed",
			currentUsername: "jdoe",
			want: TranslatedQuery{
				OrderBy: "updated_at",
				Sort:    "desc",
				State:   "closed",
			},
		},
		{
			name:            "is merged maps to the merged state",
			query:           "is:merged",
			currentUsername: "jdoe",
			want: TranslatedQuery{
				OrderBy: "updated_at",
				Sort:    "desc",
				State:   "merged",
			},
		},
		{
			name:            "author with a literal value is kept as is and not resolved",
			query:           "author:alice",
			currentUsername: "jdoe",
			want: TranslatedQuery{
				OrderBy:        "updated_at",
				Sort:           "desc",
				AuthorUsername: "alice",
			},
		},
		{
			name:  "unknown qualifiers are silently ignored",
			query: "foo:bar unknown:thing",
			want: TranslatedQuery{
				OrderBy: "updated_at",
				Sort:    "desc",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TranslateSearchQuery(tt.query, tt.currentUsername)
			require.Equal(t, tt.want, got)
		})
	}
}
