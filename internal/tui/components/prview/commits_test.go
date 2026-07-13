package prview

import (
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
)

func TestRenderCommits(t *testing.T) {
	prData := &data.PullRequestData{}
	authoredAt := time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC)

	t.Run("lists each commit with count, short sha, title and author", func(t *testing.T) {
		m := newTestModel(t, prData, nil, nil)
		m.pr.Data.Enriched.Commits = []data.PullRequestCommit{
			{Sha: "abc1234def", Title: "Fix the bug", Author: "jdoe", CreatedAt: authoredAt},
			{Sha: "def5678abc", Title: "Add tests", Author: "alice", CreatedAt: authoredAt},
		}

		got := ansi.Strip(m.renderCommits())

		require.Contains(t, got, "2 commits")
		require.Contains(t, got, "abc1234d") // short sha (8 chars)
		require.Contains(t, got, "Fix the bug")
		require.Contains(t, got, "jdoe")
		require.Contains(t, got, "Add tests")
		require.Contains(t, got, "alice")
	})

	t.Run("shows zero count when there are no commits", func(t *testing.T) {
		m := newTestModel(t, prData, nil, nil)
		m.pr.Data.Enriched.Commits = nil

		got := ansi.Strip(m.renderCommits())

		require.Contains(t, got, "0 commits")
	})

	t.Run("shows loading before the PR is enriched", func(t *testing.T) {
		m := newTestModel(t, prData, nil, nil)
		m.pr.Data.IsEnriched = false

		got := ansi.Strip(m.renderCommits())

		require.Contains(t, got, "Loading")
	})
}
