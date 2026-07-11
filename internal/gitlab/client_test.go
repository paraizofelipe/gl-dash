package gitlab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	graphql "github.com/cli/shurcooL-graphql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gitlab "gitlab.com/gitlab-org/api/client-go"
)

func TestRESTClient(t *testing.T) {
	t.Run(
		"returns the authenticated user from the client returned by RESTClient",
		func(t *testing.T) {
			defer SetClients(nil, nil)

			server := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, http.MethodGet, r.Method)
					assert.Equal(t, "/api/v4/user", r.URL.Path)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{"id":1,"username":"octocat"}`))
				}),
			)
			defer server.Close()

			mockRest, err := gitlab.NewClient("test-token", gitlab.WithBaseURL(server.URL))
			require.NoError(t, err)
			SetClients(mockRest, nil)

			c, err := RESTClient()
			require.NoError(t, err)

			user, resp, err := c.Users.CurrentUser()
			require.NoError(t, err)
			require.Equal(t, "octocat", user.Username)
			require.Equal(t, http.StatusOK, resp.StatusCode)
		},
	)

	t.Run(
		"returns an error without panicking when the server responds unauthorized",
		func(t *testing.T) {
			defer SetClients(nil, nil)

			server := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusUnauthorized)
				}),
			)
			defer server.Close()

			mockRest, err := gitlab.NewClient("invalid-token", gitlab.WithBaseURL(server.URL))
			require.NoError(t, err)
			SetClients(mockRest, nil)

			c, err := RESTClient()
			require.NoError(t, err)

			var currentUserErr error
			require.NotPanics(t, func() {
				_, _, currentUserErr = c.Users.CurrentUser()
			})
			require.Error(t, currentUserErr)
		},
	)

	t.Run("caches the client pointer across consecutive calls", func(t *testing.T) {
		defer SetClients(nil, nil)

		mockRest, err := gitlab.NewClient(
			"cache-token",
			gitlab.WithBaseURL("http://example.invalid"),
		)
		require.NoError(t, err)
		mockGQL := graphql.NewClient("http://example.invalid/api/graphql", nil)
		SetClients(mockRest, mockGQL)

		first, err := RESTClient()
		require.NoError(t, err)
		second, err := RESTClient()
		require.NoError(t, err)

		require.Same(t, first, second)
	})
}

func TestGraphQLClient(t *testing.T) {
	t.Run("executes a query through the client returned by GraphQLClient", func(t *testing.T) {
		defer SetClients(nil, nil)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/api/graphql", r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"currentUser":{"username":"octocat"}}}`))
		}))
		defer server.Close()

		mockGQL := graphql.NewClient(server.URL+"/api/graphql", server.Client())
		SetClients(nil, mockGQL)

		c, err := GraphQLClient()
		require.NoError(t, err)

		var query struct {
			CurrentUser struct {
				Username graphql.String
			}
		}
		require.NoError(t, c.Query(context.Background(), &query, nil))
		require.Equal(t, graphql.String("octocat"), query.CurrentUser.Username)
	})

	t.Run("caches the client pointer across consecutive calls", func(t *testing.T) {
		defer SetClients(nil, nil)

		mockRest, err := gitlab.NewClient(
			"cache-token",
			gitlab.WithBaseURL("http://example.invalid"),
		)
		require.NoError(t, err)
		mockGQL := graphql.NewClient("http://example.invalid/api/graphql", nil)
		SetClients(mockRest, mockGQL)

		first, err := GraphQLClient()
		require.NoError(t, err)
		second, err := GraphQLClient()
		require.NoError(t, err)

		require.Same(t, first, second)
	})
}
