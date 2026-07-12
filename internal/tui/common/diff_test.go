package common

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	gitlabapi "gitlab.com/gitlab-org/api/client-go"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
)

func newDiffTestRESTClient(t *testing.T, handler http.HandlerFunc) *gitlabapi.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c, err := gitlabapi.NewClient(
		"test-token",
		gitlabapi.WithBaseURL(server.URL),
		gitlabapi.WithoutRetries(),
	)
	require.NoError(t, err)
	return c
}

func TestDiffPR_Success(t *testing.T) {
	defer data.SetRESTClient(nil)

	mockClient := newDiffTestRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Contains(t, r.URL.Path, "/merge_requests/123/diffs")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[
			{"old_path":"README.md","new_path":"README.md","diff":"@@ -1 +1 @@\n-old\n+new"},
			{"old_path":"main.go","new_path":"main.go","diff":"@@ -2 +2 @@\n-foo\n+bar"}
		]`))
	})
	data.SetRESTClient(mockClient)

	cmd := DiffPR(123, "owner/repo")
	require.NotNil(t, cmd)

	msg, ok := cmd().(DiffFetchedMsg)
	require.True(t, ok)
	require.NoError(t, msg.Err)
	require.Equal(t, 123, msg.PrNumber)
	require.Len(t, msg.Diffs, 2)
	require.Equal(t, "README.md", msg.Diffs[0].NewPath)
	require.Equal(t, "main.go", msg.Diffs[1].NewPath)
}

func TestDiffPR_APIErrorIsPropagated(t *testing.T) {
	defer data.SetRESTClient(nil)

	mockClient := newDiffTestRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	data.SetRESTClient(mockClient)

	cmd := DiffPR(456, "my-org/my-repo")
	require.NotNil(t, cmd)

	msg, ok := cmd().(DiffFetchedMsg)
	require.True(t, ok)
	require.Error(t, msg.Err)
	require.Equal(t, 456, msg.PrNumber)
	require.Empty(t, msg.Diffs)
}
