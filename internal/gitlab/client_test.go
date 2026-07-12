package gitlab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

	t.Run(
		"uses job token header when the resolved token comes from ci job token",
		func(t *testing.T) {
			defer SetClients(nil, nil)

			var gotJobToken, gotPrivateToken string
			server := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					gotJobToken = r.Header.Get("Job-Token")
					gotPrivateToken = r.Header.Get("Private-Token")
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{"id":1,"username":"ci-bot"}`))
				}),
			)
			defer server.Close()

			host := strings.TrimPrefix(server.URL, "http://")
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			t.Setenv("GITLAB_TOKEN", "")
			t.Setenv("GITLAB_HOST", host)
			t.Setenv("CI_JOB_TOKEN", "ci-job-token-xyz")
			configDir := filepath.Join(homeDir, ".config", "glab-cli")
			require.NoError(t, os.MkdirAll(configDir, 0o755))
			cfgYAML := "hosts:\n  " + host + ":\n    api_protocol: http\n"
			require.NoError(
				t,
				os.WriteFile(filepath.Join(configDir, "config.yml"), []byte(cfgYAML), 0o644),
			)

			c, err := RESTClient()
			require.NoError(t, err)

			_, _, err = c.Users.CurrentUser()
			require.NoError(t, err)
			require.Equal(t, "ci-job-token-xyz", gotJobToken)
			require.Empty(t, gotPrivateToken)
		},
	)

	t.Run(
		"uses private token header when the resolved token comes from gitlab token",
		func(t *testing.T) {
			defer SetClients(nil, nil)

			var gotJobToken, gotPrivateToken string
			server := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					gotJobToken = r.Header.Get("Job-Token")
					gotPrivateToken = r.Header.Get("Private-Token")
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{"id":1,"username":"octocat"}`))
				}),
			)
			defer server.Close()

			host := strings.TrimPrefix(server.URL, "http://")
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			t.Setenv("GITLAB_TOKEN", "personal-token-rest")
			t.Setenv("GITLAB_HOST", host)
			t.Setenv("CI_JOB_TOKEN", "")
			configDir := filepath.Join(homeDir, ".config", "glab-cli")
			require.NoError(t, os.MkdirAll(configDir, 0o755))
			cfgYAML := "hosts:\n  " + host + ":\n    api_protocol: http\n"
			require.NoError(
				t,
				os.WriteFile(filepath.Join(configDir, "config.yml"), []byte(cfgYAML), 0o644),
			)

			c, err := RESTClient()
			require.NoError(t, err)

			_, _, err = c.Users.CurrentUser()
			require.NoError(t, err)
			require.Equal(t, "personal-token-rest", gotPrivateToken)
			require.Empty(t, gotJobToken)
		},
	)
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

	t.Run(
		"uses job token header when the resolved token comes from ci job token",
		func(t *testing.T) {
			defer SetClients(nil, nil)

			var gotJobToken, gotPrivateToken string
			server := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					gotJobToken = r.Header.Get("Job-Token")
					gotPrivateToken = r.Header.Get("Private-Token")
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"data":{"currentUser":{"username":"ci-bot"}}}`))
				}),
			)
			defer server.Close()

			host := strings.TrimPrefix(server.URL, "http://")
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			t.Setenv("GITLAB_TOKEN", "")
			t.Setenv("GITLAB_HOST", host)
			t.Setenv("CI_JOB_TOKEN", "ci-job-token-graphql")
			configDir := filepath.Join(homeDir, ".config", "glab-cli")
			require.NoError(t, os.MkdirAll(configDir, 0o755))
			cfgYAML := "hosts:\n  " + host + ":\n    api_protocol: http\n"
			require.NoError(
				t,
				os.WriteFile(filepath.Join(configDir, "config.yml"), []byte(cfgYAML), 0o644),
			)

			c, err := GraphQLClient()
			require.NoError(t, err)

			var query struct {
				CurrentUser struct {
					Username graphql.String
				}
			}
			require.NoError(t, c.Query(context.Background(), &query, nil))
			require.Equal(t, "ci-job-token-graphql", gotJobToken)
			require.Empty(t, gotPrivateToken)
		},
	)

	t.Run(
		"uses private token header when the resolved token comes from gitlab token",
		func(t *testing.T) {
			defer SetClients(nil, nil)

			var gotJobToken, gotPrivateToken string
			server := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					gotJobToken = r.Header.Get("Job-Token")
					gotPrivateToken = r.Header.Get("Private-Token")
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"data":{"currentUser":{"username":"octocat"}}}`))
				}),
			)
			defer server.Close()

			host := strings.TrimPrefix(server.URL, "http://")
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			t.Setenv("GITLAB_TOKEN", "personal-token-graphql")
			t.Setenv("GITLAB_HOST", host)
			t.Setenv("CI_JOB_TOKEN", "")
			configDir := filepath.Join(homeDir, ".config", "glab-cli")
			require.NoError(t, os.MkdirAll(configDir, 0o755))
			cfgYAML := "hosts:\n  " + host + ":\n    api_protocol: http\n"
			require.NoError(
				t,
				os.WriteFile(filepath.Join(configDir, "config.yml"), []byte(cfgYAML), 0o644),
			)

			c, err := GraphQLClient()
			require.NoError(t, err)

			var query struct {
				CurrentUser struct {
					Username graphql.String
				}
			}
			require.NoError(t, c.Query(context.Background(), &query, nil))
			require.Equal(t, "personal-token-graphql", gotPrivateToken)
			require.Empty(t, gotJobToken)
		},
	)
}

func setupConcurrentClientFixture(t *testing.T, server *httptest.Server, tokenEnvValue string) {
	t.Helper()

	host := strings.TrimPrefix(server.URL, "http://")
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("GITLAB_TOKEN", tokenEnvValue)
	t.Setenv("GITLAB_HOST", host)
	t.Setenv("CI_JOB_TOKEN", "")
	configDir := filepath.Join(homeDir, ".config", "glab-cli")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	cfgYAML := "hosts:\n  " + host + ":\n    api_protocol: http\n"
	require.NoError(
		t,
		os.WriteFile(filepath.Join(configDir, "config.yml"), []byte(cfgYAML), 0o644),
	)
}

func TestRESTClient_ConcurrentAccess(t *testing.T) {
	defer SetClients(nil, nil)

	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":1,"username":"concurrent-rest"}`))
		}),
	)
	defer server.Close()
	setupConcurrentClientFixture(t, server, "concurrent-rest-token")

	const n = 50
	var wg sync.WaitGroup
	results := make([]*gitlab.Client, n)
	errs := make([]error, n)
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = RESTClient()
		}(i)
	}
	close(start)
	wg.Wait()

	for i := range n {
		require.NoError(t, errs[i])
		require.NotNil(t, results[i])
		require.Same(t, results[0], results[i])
	}
}

func TestGraphQLClient_ConcurrentAccess(t *testing.T) {
	defer SetClients(nil, nil)

	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"currentUser":{"username":"concurrent-graphql"}}}`))
		}),
	)
	defer server.Close()
	setupConcurrentClientFixture(t, server, "concurrent-graphql-token")

	const n = 50
	var wg sync.WaitGroup
	results := make([]*graphql.Client, n)
	errs := make([]error, n)
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = GraphQLClient()
		}(i)
	}
	close(start)
	wg.Wait()

	for i := range n {
		require.NoError(t, errs[i])
		require.NotNil(t, results[i])
		require.Same(t, results[0], results[i])
	}
}
