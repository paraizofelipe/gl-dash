package context

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/git"
)

func TestHasGHRepo(t *testing.T) {
	tests := []struct {
		name   string
		ghRepo *git.RemoteRepo
		want   bool
	}{
		{
			name:   "nil GHRepo",
			ghRepo: nil,
			want:   false,
		},
		{
			name:   "zero value GHRepo",
			ghRepo: &git.RemoteRepo{},
			want:   false,
		},
		{
			name:   "populated GHRepo",
			ghRepo: &git.RemoteRepo{Host: "gitlab.com", Owner: "ns", Name: "proj"},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &ProgramContext{GHRepo: tt.ghRepo}

			require.Equal(t, tt.want, ctx.HasGHRepo())
		})
	}
}
