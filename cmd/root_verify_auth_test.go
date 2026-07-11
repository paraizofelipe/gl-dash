package cmd

import (
	"net/http"
	"net/http/httptest"
	"testing"

	glab "github.com/dlvhdr/gh-dash/v4/internal/gitlab"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gitlab "gitlab.com/gitlab-org/api/client-go"
)

func TestVerifyGitLabAuth(t *testing.T) {
	t.Run("returns nil when the GitLab API authenticates the current user", func(t *testing.T) {
		defer glab.SetClients(nil, nil)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/api/v4/user", r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":1,"username":"octocat"}`))
		}))
		defer server.Close()

		mockRest, err := gitlab.NewClient("test-token", gitlab.WithBaseURL(server.URL))
		require.NoError(t, err)
		glab.SetClients(mockRest, nil)

		err = verifyGitLabAuth()

		require.NoError(t, err)
	})

	t.Run("returns an error when the GitLab API rejects authentication", func(t *testing.T) {
		defer glab.SetClients(nil, nil)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer server.Close()

		mockRest, err := gitlab.NewClient("invalid-token", gitlab.WithBaseURL(server.URL))
		require.NoError(t, err)
		glab.SetClients(mockRest, nil)

		err = verifyGitLabAuth()

		require.Error(t, err)
	})
}
