package git

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseRemoteURL(t *testing.T) {
	tests := []struct {
		name   string
		url    string
		want   RemoteRepo
		wantOk bool
	}{
		{
			name:   "https gitlab.com",
			url:    "https://gitlab.com/ns/proj.git",
			want:   RemoteRepo{Host: "gitlab.com", Owner: "ns", Name: "proj"},
			wantOk: true,
		},
		{
			name:   "https self-managed gitlab",
			url:    "https://gitlab.empresa.com/ns/proj.git",
			want:   RemoteRepo{Host: "gitlab.empresa.com", Owner: "ns", Name: "proj"},
			wantOk: true,
		},
		{
			name:   "ssh gitlab.com",
			url:    "git@gitlab.com:ns/proj.git",
			want:   RemoteRepo{Host: "gitlab.com", Owner: "ns", Name: "proj"},
			wantOk: true,
		},
		{
			name:   "ssh self-managed nested subgroup",
			url:    "git@gitlab.empresa.com:group/subgroup/proj.git",
			want:   RemoteRepo{Host: "gitlab.empresa.com", Owner: "group/subgroup", Name: "proj"},
			wantOk: true,
		},
		{
			name:   "invalid url",
			url:    "not a url",
			wantOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseRemoteURL(tt.url)

			require.Equal(t, tt.wantOk, ok)
			if tt.wantOk {
				require.Equal(t, tt.want, got)
			}
		})
	}
}

func TestGetRepoShortName(t *testing.T) {
	require.Equal(t, "ns/proj", GetRepoShortName("https://gitlab.com/ns/proj.git"))
}
