package notificationssection

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	graphql "github.com/cli/shurcooL-graphql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gitlabapi "gitlab.com/gitlab-org/api/client-go"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/notificationrow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

func newMockRESTClient(t *testing.T, handler http.HandlerFunc) *gitlabapi.Client {
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

func newMockGraphQLClient(t *testing.T, handler http.HandlerFunc) *graphql.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return graphql.NewClient(server.URL+"/api/graphql", server.Client())
}

func unsetGitLabTodosFlag(t *testing.T) {
	t.Helper()
	original, wasSet := os.LookupEnv(config.FF_GITLAB_TODOS)
	require.NoError(t, os.Unsetenv(config.FF_GITLAB_TODOS))
	t.Cleanup(func() {
		if wasSet {
			require.NoError(t, os.Setenv(config.FF_GITLAB_TODOS, original))
		}
	})
}

func newTestModel(t *testing.T) Model {
	t.Helper()
	cfg, err := config.ParseConfig(config.Location{
		ConfigFlag:       "../../../config/testdata/test-config.yml",
		SkipGlobalConfig: true,
	})
	require.NoError(t, err)

	ctx := &context.ProgramContext{
		Config:    &cfg,
		StartTask: noopStartTask,
	}
	ctx.Theme = theme.ParseTheme(ctx.Config)
	ctx.Styles = context.InitStyles(ctx.Theme)

	return NewModel(0, ctx, config.NotificationsSectionConfig{}, time.Now())
}

func TestFetchNextPageSectionRows_GitLabTodosFeatureGate(t *testing.T) {
	t.Run(
		"flag disabled returns a single disabled task message without any http call",
		func(t *testing.T) {
			defer data.SetRESTClient(nil)
			unsetGitLabTodosFlag(t)

			httpCalled := false
			mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
				httpCalled = true
				w.WriteHeader(http.StatusOK)
			})
			data.SetRESTClient(mockClient)

			m := newTestModel(t)

			cmds := m.FetchNextPageSectionRows()
			require.Len(t, cmds, 1)
			require.NotNil(t, cmds[0])

			msg := cmds[0]()
			taskMsg, ok := msg.(constants.TaskFinishedMsg)
			require.True(t, ok, "expected constants.TaskFinishedMsg, got %T", msg)
			assert.Equal(t, "gitlab_todos_disabled", taskMsg.TaskId)
			assert.Equal(t, m.Id, taskMsg.SectionId)
			assert.Equal(t, m.Type, taskMsg.SectionType)
			require.Error(t, taskMsg.Err)
			assert.Contains(t, taskMsg.Err.Error(), "disabled")

			assert.False(t, httpCalled, "flag gate must short-circuit before any todos api call")
		},
	)

	t.Run(
		"flag enabled proceeds to the normal fetch flow and reaches the todos api",
		func(t *testing.T) {
			defer data.SetRESTClient(nil)
			t.Setenv(config.FF_GITLAB_TODOS, "1")

			httpCalled := false
			mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
				httpCalled = true
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`[]`))
			})
			data.SetRESTClient(mockClient)

			m := newTestModel(t)

			cmds := m.FetchNextPageSectionRows()
			require.GreaterOrEqual(t, len(cmds), 2)

			fetchCmd := cmds[len(cmds)-1]
			require.NotNil(t, fetchCmd)

			require.NotPanics(t, func() {
				fetchCmd()
			})

			assert.True(t, httpCalled, "expected the fetch command to reach the gitlab todos api")
		},
	)
}

func buildFailedNotificationWithoutResolvablePipeline(
	id, mergeRequestUrl string,
) notificationrow.Data {
	return notificationrow.Data{
		Notification: data.NotificationData{
			Id:     id,
			Reason: data.ReasonBuildFailed,
			Subject: data.NotificationSubject{
				Type: "MergeRequest",
				Url:  mergeRequestUrl,
			},
			Repository: data.NotificationRepository{
				FullName: "group/proj",
			},
		},
	}
}

func assertNoCommandResolvesNotificationUrl(t *testing.T, cmds []tea.Cmd) {
	t.Helper()
	require.NotEmpty(t, cmds)
	for _, cmd := range cmds {
		require.NotNil(t, cmd)
		var msg tea.Msg
		require.NotPanics(t, func() {
			msg = cmd()
		})
		_, isUrlMsg := msg.(UpdateNotificationUrlMsg)
		assert.False(
			t,
			isUrlMsg,
			"should not resolve a pipeline url when the merge request pipeline is not resolvable",
		)
	}
}

func TestFetchCommentCountsForNotifications_BuildFailedWithoutResolvableMergeRequest(t *testing.T) {
	t.Run(
		"pipeline lookup returning http 404 does not panic and does not resolve a url",
		func(t *testing.T) {
			defer data.SetRESTClient(nil)
			defer data.SetClient(nil)

			mockRest := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			})
			data.SetRESTClient(mockRest)

			mockGQL := newMockGraphQLClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			})
			data.SetClient(mockGQL)

			notif := buildFailedNotificationWithoutResolvablePipeline(
				"notif-build-failed-404",
				"https://gitlab.com/group/proj/-/merge_requests/9",
			)

			m := &Model{}
			cmds := m.fetchCommentCountsForNotifications([]notificationrow.Data{notif})
			assertNoCommandResolvesNotificationUrl(t, cmds)
		},
	)

	t.Run("pipeline with no id yet does not panic and does not resolve a url", func(t *testing.T) {
		defer data.SetRESTClient(nil)
		defer data.SetClient(nil)

		mockRest := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
		})
		data.SetRESTClient(mockRest)

		mockGQL := newMockGraphQLClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})
		data.SetClient(mockGQL)

		notif := buildFailedNotificationWithoutResolvablePipeline(
			"notif-build-failed-no-pipeline",
			"https://gitlab.com/group/proj/-/merge_requests/10",
		)

		m := &Model{}
		cmds := m.fetchCommentCountsForNotifications([]notificationrow.Data{notif})
		assertNoCommandResolvesNotificationUrl(t, cmds)
	})
}
