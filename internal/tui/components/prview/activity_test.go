package prview

import (
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/markdown"
)

func TestRenderReviewHeaderTimestamp(t *testing.T) {
	prData := &data.PullRequestData{}

	t.Run("suppresses the reviewed line when UpdatedAt is zero", func(t *testing.T) {
		m := newTestModel(t, prData, nil, nil)
		review := data.Review{
			Author: struct{ Login string }{Login: "carol"},
			State:  "APPROVED",
		}

		got := ansi.Strip(m.renderReviewHeader(review))

		require.Contains(t, got, "carol")
		require.NotContains(t, got, "reviewed")
	})

	t.Run("renders the reviewed line when UpdatedAt is set", func(t *testing.T) {
		m := newTestModel(t, prData, nil, nil)
		review := data.Review{
			Author:    struct{ Login string }{Login: "carol"},
			State:     "APPROVED",
			UpdatedAt: time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC),
		}

		got := ansi.Strip(m.renderReviewHeader(review))

		require.Contains(t, got, "carol")
		require.Contains(t, got, "reviewed")
	})
}

func TestRenderActivityDeletedAuthorFallback(t *testing.T) {
	prData := &data.PullRequestData{}
	updatedAt := time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC)

	t.Run("review header falls back to ghost for an empty author", func(t *testing.T) {
		m := newTestModel(t, prData, nil, nil)
		review := data.Review{
			Author:    struct{ Login string }{Login: ""},
			State:     "APPROVED",
			UpdatedAt: updatedAt,
		}

		got := ansi.Strip(m.renderReviewHeader(review))

		require.Contains(t, got, "ghost")
	})

	t.Run("comment falls back to ghost for an empty author", func(t *testing.T) {
		m := newTestModel(t, prData, nil, nil)
		renderer := markdown.GetMarkdownRenderer(m.getIndentedContentWidth(), m.ctx)
		c := comment{
			Author:    "",
			Body:      "hello",
			UpdatedAt: updatedAt,
		}

		got, err := m.renderComment(c, renderer)
		require.NoError(t, err)
		require.Contains(t, ansi.Strip(got), "ghost")
	})
}
