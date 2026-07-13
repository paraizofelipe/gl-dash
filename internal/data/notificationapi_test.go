package data

import (
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gitlabapi "gitlab.com/gitlab-org/api/client-go"
)

func TestNotificationDataGetUrl(t *testing.T) {
	tests := []struct {
		name     string
		data     NotificationData
		expected string
	}{
		{
			name: "uses HtmlUrl from repository",
			data: NotificationData{
				Repository: NotificationRepository{
					FullName: "owner/repo",
					HtmlUrl:  "https://github.com/owner/repo",
				},
			},
			expected: "https://github.com/owner/repo",
		},
		{
			name: "uses GHE host from HtmlUrl",
			data: NotificationData{
				Repository: NotificationRepository{
					FullName: "org/repo",
					HtmlUrl:  "https://ghe.company.com/org/repo",
				},
			},
			expected: "https://ghe.company.com/org/repo",
		},
		{
			name: "trims trailing slash from HtmlUrl",
			data: NotificationData{
				Repository: NotificationRepository{
					FullName: "org/repo",
					HtmlUrl:  "https://ghe.company.com/org/repo/",
				},
			},
			expected: "https://ghe.company.com/org/repo",
		},
		{
			name: "empty HtmlUrl returns empty string",
			data: NotificationData{
				Repository: NotificationRepository{
					FullName: "owner/repo",
					HtmlUrl:  "",
				},
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.data.GetUrl()
			if result != tt.expected {
				t.Errorf("GetUrl() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func isolateGitHubAuthEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GH_CONFIG_DIR", t.TempDir())
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_ENTERPRISE_TOKEN", "")
	t.Setenv("GITHUB_ENTERPRISE_TOKEN", "")
	t.Setenv("GH_HOST", "")
	t.Setenv("GH_PATH", filepath.Join(t.TempDir(), "gh-binary-not-found"))
}

func mustMarshalTodos(t *testing.T, todos []*gitlabapi.Todo) []byte {
	t.Helper()
	body, err := json.Marshal(todos)
	require.NoError(t, err)
	return body
}

func TestFetchNotifications(t *testing.T) {
	t.Run(
		"first request without page info sends per_page, page one and pending state and maps returned todos",
		func(t *testing.T) {
			defer SetRESTClient(nil)
			isolateGitHubAuthEnv(t)

			todo := &gitlabapi.Todo{
				ID:         101,
				State:      "pending",
				ActionName: gitlabapi.TodoMentioned,
				TargetType: gitlabapi.TodoTargetIssue,
				Project: &gitlabapi.BasicProject{
					ID:                10,
					Name:              "proj",
					PathWithNamespace: "group/proj",
				},
				Author: &gitlabapi.BasicUser{Username: "alice"},
				Target: &gitlabapi.TodoTarget{
					Title:  "Fix bug",
					WebURL: "https://gitlab.com/group/proj/-/issues/1",
				},
				CreatedAt: gitlabapi.Ptr(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
			}

			var capturedQuery url.Values
			var capturedMethod, capturedPath string
			mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
				capturedMethod = r.Method
				capturedPath = r.URL.Path
				capturedQuery = r.URL.Query()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(mustMarshalTodos(t, []*gitlabapi.Todo{todo}))
			})
			SetRESTClient(mockClient)

			resp, err := FetchNotifications(20, nil, NotificationStateUnread, nil)
			require.NoError(t, err)

			assert.Equal(t, http.MethodGet, capturedMethod)
			assert.Contains(t, capturedPath, "/todos")
			require.NotNil(t, capturedQuery)
			assert.Equal(t, "20", capturedQuery.Get("per_page"))
			assert.Equal(t, "1", capturedQuery.Get("page"))
			assert.Equal(t, "pending", capturedQuery.Get("state"))

			require.Len(t, resp.Notifications, 1)
			assert.Equal(t, "101", resp.Notifications[0].Id)
			assert.Equal(t, "Fix bug", resp.Notifications[0].Subject.Title)
		},
	)

	t.Run(
		"reports has next page true when the server returns the x next page header",
		func(t *testing.T) {
			defer SetRESTClient(nil)
			isolateGitHubAuthEnv(t)

			mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Next-Page", "2")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`[]`))
			})
			SetRESTClient(mockClient)

			resp, err := FetchNotifications(20, nil, NotificationStateUnread, nil)
			require.NoError(t, err)
			assert.True(t, resp.PageInfo.HasNextPage)
			assert.Equal(t, "2", resp.PageInfo.EndCursor)
		},
	)

	t.Run(
		"reports has next page false when the server omits the x next page header",
		func(t *testing.T) {
			defer SetRESTClient(nil)
			isolateGitHubAuthEnv(t)

			mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`[]`))
			})
			SetRESTClient(mockClient)

			resp, err := FetchNotifications(20, nil, NotificationStateUnread, nil)
			require.NoError(t, err)
			assert.False(t, resp.PageInfo.HasNextPage)
			assert.Equal(t, "", resp.PageInfo.EndCursor)
		},
	)

	t.Run(
		"uses the page number carried in page info end cursor as the page query parameter",
		func(t *testing.T) {
			defer SetRESTClient(nil)
			isolateGitHubAuthEnv(t)

			var capturedQuery url.Values
			mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
				capturedQuery = r.URL.Query()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`[]`))
			})
			SetRESTClient(mockClient)

			_, err := FetchNotifications(
				20,
				nil,
				NotificationStateUnread,
				&PageInfo{EndCursor: "7"},
			)
			require.NoError(t, err)
			require.NotNil(t, capturedQuery)
			assert.Equal(t, "7", capturedQuery.Get("page"))
		},
	)

	t.Run("read state sends the done state query parameter", func(t *testing.T) {
		defer SetRESTClient(nil)
		isolateGitHubAuthEnv(t)

		var capturedQuery url.Values
		mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
			capturedQuery = r.URL.Query()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
		})
		SetRESTClient(mockClient)

		_, err := FetchNotifications(20, nil, NotificationStateRead, nil)
		require.NoError(t, err)
		require.NotNil(t, capturedQuery)
		assert.Equal(t, "done", capturedQuery.Get("state"))
	})

	t.Run("all state omits the state query parameter", func(t *testing.T) {
		defer SetRESTClient(nil)
		isolateGitHubAuthEnv(t)

		var capturedQuery url.Values
		mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
			capturedQuery = r.URL.Query()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
		})
		SetRESTClient(mockClient)

		_, err := FetchNotifications(20, nil, NotificationStateAll, nil)
		require.NoError(t, err)
		require.NotNil(t, capturedQuery)
		assert.Empty(t, capturedQuery.Get("state"))
	})

	t.Run(
		"repo filters keep only todos whose project path with namespace matches",
		func(t *testing.T) {
			defer SetRESTClient(nil)
			isolateGitHubAuthEnv(t)

			todos := []*gitlabapi.Todo{
				{
					ID:      1,
					State:   "pending",
					Project: &gitlabapi.BasicProject{PathWithNamespace: "group/proj-a"},
				},
				{
					ID:      2,
					State:   "pending",
					Project: &gitlabapi.BasicProject{PathWithNamespace: "group/proj-b"},
				},
			}
			mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(mustMarshalTodos(t, todos))
			})
			SetRESTClient(mockClient)

			resp, err := FetchNotifications(
				20,
				[]string{"group/proj-a"},
				NotificationStateUnread,
				nil,
			)
			require.NoError(t, err)
			require.Len(t, resp.Notifications, 1)
			assert.Equal(t, "1", resp.Notifications[0].Id)
			assert.Equal(t, "group/proj-a", resp.Notifications[0].Repository.FullName)
		},
	)

	t.Run(
		"propagates an error when the server responds with http 500 without panicking",
		func(t *testing.T) {
			defer SetRESTClient(nil)
			isolateGitHubAuthEnv(t)

			mockClient := newMockRESTClient(
				t,
				staticJSONHandler(http.StatusInternalServerError, ""),
			)
			SetRESTClient(mockClient)

			var err error
			require.NotPanics(t, func() {
				_, err = FetchNotifications(20, nil, NotificationStateUnread, nil)
			})
			require.Error(t, err)
		},
	)
}

func TestFetchNotificationByThreadId(t *testing.T) {
	t.Run(
		"returns an error without any http call when thread id is not numeric",
		func(t *testing.T) {
			defer SetRESTClient(nil)
			isolateGitHubAuthEnv(t)

			calls := 0
			mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`[]`))
			})
			SetRESTClient(mockClient)

			notification, err := FetchNotificationByThreadId("not-a-number")
			require.Error(t, err)
			assert.Nil(t, notification)
			assert.Equal(t, 0, calls)
		},
	)

	t.Run("finds the todo on the second pending page", func(t *testing.T) {
		defer SetRESTClient(nil)
		isolateGitHubAuthEnv(t)

		mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
			state := r.URL.Query().Get("state")
			page := r.URL.Query().Get("page")
			w.Header().Set("Content-Type", "application/json")
			switch {
			case state == "pending" && (page == "" || page == "1"):
				w.Header().Set("X-Next-Page", "2")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(mustMarshalTodos(t, []*gitlabapi.Todo{{ID: 1, State: "pending"}}))
			case state == "pending" && page == "2":
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(mustMarshalTodos(t, []*gitlabapi.Todo{
					{ID: 42, State: "pending", Target: &gitlabapi.TodoTarget{Title: "Found me"}},
				}))
			default:
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`[]`))
			}
		})
		SetRESTClient(mockClient)

		notification, err := FetchNotificationByThreadId("42")
		require.NoError(t, err)
		require.NotNil(t, notification)
		assert.Equal(t, "42", notification.Id)
		assert.Equal(t, "Found me", notification.Subject.Title)
	})

	t.Run(
		"returns nil without error when the id is not found in any page or state",
		func(t *testing.T) {
			defer SetRESTClient(nil)
			isolateGitHubAuthEnv(t)

			mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(mustMarshalTodos(t, []*gitlabapi.Todo{{ID: 999, State: "pending"}}))
			})
			SetRESTClient(mockClient)

			notification, err := FetchNotificationByThreadId("123456")
			require.NoError(t, err)
			assert.Nil(t, notification)
		},
	)
}

func TestNotificationFromTodo(t *testing.T) {
	t.Run("maps all fields from a fully populated todo", func(t *testing.T) {
		createdAt := time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)
		todo := &gitlabapi.Todo{
			ID:         55,
			State:      "pending",
			ActionName: gitlabapi.TodoAssigned,
			TargetType: gitlabapi.TodoTargetMergeRequest,
			Project: &gitlabapi.BasicProject{
				ID:                7,
				Name:              "proj",
				PathWithNamespace: "group/proj",
			},
			Author: &gitlabapi.BasicUser{Username: "alice"},
			Target: &gitlabapi.TodoTarget{
				IID:    42,
				Title:  "Fix the bug",
				WebURL: "https://gitlab.com/group/proj/-/merge_requests/9",
			},
			CreatedAt: gitlabapi.Ptr(createdAt),
		}

		result := notificationFromTodo(todo)

		assert.Equal(t, "55", result.Id)
		assert.True(t, result.Unread)
		assert.Equal(t, "assigned", result.Reason)
		assert.Equal(t, "MergeRequest", result.Subject.Type)
		assert.Equal(t, "Fix the bug", result.Subject.Title)
		assert.Equal(t, "https://gitlab.com/group/proj/-/merge_requests/9", result.Subject.Url)
		assert.Equal(t, int64(42), result.Subject.IID)
		assert.Equal(t, "group/proj", result.Repository.FullName)
		assert.Equal(t, "proj", result.Repository.Name)
		assert.Equal(t, 7, result.Repository.Id)
		assert.Equal(t, "https://gitlab.com/group/proj", result.Repository.HtmlUrl)
		assert.Equal(t, "alice", result.Actor)
		assert.Equal(t, createdAt, result.UpdatedAt)
	})

	t.Run(
		"derives the repository html url from the target web url merge request suffix",
		func(t *testing.T) {
			result := notificationFromTodo(&gitlabapi.Todo{
				ID:      1,
				State:   "pending",
				Project: &gitlabapi.BasicProject{PathWithNamespace: "group/proj"},
				Target: &gitlabapi.TodoTarget{
					WebURL: "https://gitlab.com/group/proj/-/merge_requests/9",
				},
			})
			assert.Equal(t, "https://gitlab.com/group/proj", result.Repository.HtmlUrl)
		},
	)

	t.Run(
		"derives the repository html url from the target web url issue suffix",
		func(t *testing.T) {
			result := notificationFromTodo(&gitlabapi.Todo{
				ID:      1,
				State:   "pending",
				Project: &gitlabapi.BasicProject{PathWithNamespace: "group/proj"},
				Target:  &gitlabapi.TodoTarget{WebURL: "https://gitlab.com/group/proj/-/issues/3"},
			})
			assert.Equal(t, "https://gitlab.com/group/proj", result.Repository.HtmlUrl)
		},
	)

	t.Run(
		"leaves the repository html url empty when the target web url has no dash segment",
		func(t *testing.T) {
			result := notificationFromTodo(&gitlabapi.Todo{
				ID:      1,
				State:   "pending",
				Project: &gitlabapi.BasicProject{PathWithNamespace: "group/proj"},
				Target:  &gitlabapi.TodoTarget{WebURL: "https://gitlab.com/group/proj"},
			})
			assert.Equal(t, "", result.Repository.HtmlUrl)
		},
	)

	t.Run("leaves the repository html url empty when the target is nil", func(t *testing.T) {
		result := notificationFromTodo(&gitlabapi.Todo{
			ID:      1,
			State:   "pending",
			Project: &gitlabapi.BasicProject{PathWithNamespace: "group/proj"},
		})
		assert.Equal(t, "", result.Repository.HtmlUrl)
	})

	t.Run("maps the target iid onto the subject iid", func(t *testing.T) {
		result := notificationFromTodo(&gitlabapi.Todo{
			ID:     1,
			State:  "pending",
			Target: &gitlabapi.TodoTarget{IID: 42},
		})
		assert.Equal(t, int64(42), result.Subject.IID)
	})

	t.Run("nil target leaves the subject iid at zero without panic", func(t *testing.T) {
		var result NotificationData
		require.NotPanics(t, func() {
			result = notificationFromTodo(&gitlabapi.Todo{ID: 1, State: "pending"})
		})
		assert.Equal(t, int64(0), result.Subject.IID)
	})

	t.Run("done state maps to unread false", func(t *testing.T) {
		result := notificationFromTodo(&gitlabapi.Todo{ID: 1, State: "done"})
		assert.False(t, result.Unread)
	})

	t.Run("pending state maps to unread true", func(t *testing.T) {
		result := notificationFromTodo(&gitlabapi.Todo{ID: 1, State: "pending"})
		assert.True(t, result.Unread)
	})

	actionCases := []struct {
		name   string
		action gitlabapi.TodoAction
		want   string
	}{
		{"assigned", gitlabapi.TodoAssigned, "assigned"},
		{"mentioned", gitlabapi.TodoMentioned, "mentioned"},
		{"build failed", gitlabapi.TodoBuildFailed, "build_failed"},
		{"marked", gitlabapi.TodoMarked, "marked"},
		{"approval required", gitlabapi.TodoApprovalRequired, "approval_required"},
		{"directly addressed", gitlabapi.TodoDirectlyAddressed, "directly_addressed"},
	}
	for _, tc := range actionCases {
		t.Run("maps todo action "+tc.name+" to reason", func(t *testing.T) {
			result := notificationFromTodo(
				&gitlabapi.Todo{ID: 1, State: "pending", ActionName: tc.action},
			)
			assert.Equal(t, tc.want, result.Reason)
		})
	}

	targetTypeCases := []struct {
		name       string
		targetType gitlabapi.TodoTargetType
		want       string
	}{
		{"alert management", gitlabapi.TodoTargetAlertManagement, "AlertManagement::Alert"},
		{"design management", gitlabapi.TodoTargetDesignManagement, "DesignManagement::Design"},
		{"issue", gitlabapi.TodoTargetIssue, "Issue"},
		{"merge request", gitlabapi.TodoTargetMergeRequest, "MergeRequest"},
	}
	for _, tc := range targetTypeCases {
		t.Run("maps todo target type "+tc.name+" to subject type", func(t *testing.T) {
			result := notificationFromTodo(
				&gitlabapi.Todo{ID: 1, State: "pending", TargetType: tc.targetType},
			)
			assert.Equal(t, tc.want, result.Subject.Type)
		})
	}

	t.Run("unknown action name is preserved as the raw reason without panic", func(t *testing.T) {
		var result NotificationData
		require.NotPanics(t, func() {
			result = notificationFromTodo(
				&gitlabapi.Todo{
					ID:         1,
					State:      "pending",
					ActionName: gitlabapi.TodoAction("unmergeable"),
				},
			)
		})
		assert.Equal(t, "unmergeable", result.Reason)
	})

	t.Run(
		"unknown target type is preserved as the raw subject type without panic",
		func(t *testing.T) {
			var result NotificationData
			require.NotPanics(t, func() {
				result = notificationFromTodo(
					&gitlabapi.Todo{
						ID:         1,
						State:      "pending",
						TargetType: gitlabapi.TodoTargetType("Commit"),
					},
				)
			})
			assert.Equal(t, "Commit", result.Subject.Type)
		},
	)

	t.Run(
		"nil target project and author do not panic and leave dependent fields as zero values",
		func(t *testing.T) {
			var result NotificationData
			require.NotPanics(t, func() {
				result = notificationFromTodo(&gitlabapi.Todo{
					ID:        1,
					State:     "pending",
					CreatedAt: gitlabapi.Ptr(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
				})
			})
			assert.Equal(t, "", result.Subject.Title)
			assert.Equal(t, "", result.Subject.Url)
			assert.Equal(t, "", result.Repository.FullName)
			assert.Equal(t, "", result.Repository.Name)
			assert.Equal(t, 0, result.Repository.Id)
			assert.Equal(t, "", result.Actor)
		},
	)

	t.Run(
		"nil created at does not panic and leaves updated at as the zero time",
		func(t *testing.T) {
			var result NotificationData
			require.NotPanics(t, func() {
				result = notificationFromTodo(&gitlabapi.Todo{ID: 1, State: "pending"})
			})
			assert.True(t, result.UpdatedAt.IsZero())
		},
	)
}

func TestMarkNotificationDone(t *testing.T) {
	t.Run("posts mark as done to the todo id parsed from the thread id", func(t *testing.T) {
		defer SetRESTClient(nil)
		isolateGitHubAuthEnv(t)

		var gotMethod, gotPath string
		mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			gotPath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		})
		SetRESTClient(mockClient)

		err := MarkNotificationDone("123")
		require.NoError(t, err)
		assert.Equal(t, http.MethodPost, gotMethod)
		assert.Contains(t, gotPath, "/todos/123/mark_as_done")
	})

	t.Run(
		"returns an error without calling the api when thread id is not numeric",
		func(t *testing.T) {
			defer SetRESTClient(nil)
			isolateGitHubAuthEnv(t)

			calls := 0
			mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(http.StatusNoContent)
			})
			SetRESTClient(mockClient)

			err := MarkNotificationDone("not-a-number")
			require.Error(t, err)
			assert.Equal(t, 0, calls)
		},
	)

	t.Run("propagates an error when the server responds with http 500", func(t *testing.T) {
		defer SetRESTClient(nil)
		isolateGitHubAuthEnv(t)

		mockClient := newMockRESTClient(t, staticJSONHandler(http.StatusInternalServerError, ""))
		SetRESTClient(mockClient)

		err := MarkNotificationDone("123")
		require.Error(t, err)
	})
}

func TestMarkNotificationRead(t *testing.T) {
	t.Run("posts mark as done to the todo id parsed from the thread id", func(t *testing.T) {
		defer SetRESTClient(nil)
		isolateGitHubAuthEnv(t)

		var gotMethod, gotPath string
		mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			gotPath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		})
		SetRESTClient(mockClient)

		err := MarkNotificationRead("123")
		require.NoError(t, err)
		assert.Equal(t, http.MethodPost, gotMethod)
		assert.Contains(t, gotPath, "/todos/123/mark_as_done")
	})

	t.Run(
		"returns an error without calling the api when thread id is not numeric",
		func(t *testing.T) {
			defer SetRESTClient(nil)
			isolateGitHubAuthEnv(t)

			calls := 0
			mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(http.StatusNoContent)
			})
			SetRESTClient(mockClient)

			err := MarkNotificationRead("not-a-number")
			require.Error(t, err)
			assert.Equal(t, 0, calls)
		},
	)

	t.Run("propagates an error when the server responds with http 500", func(t *testing.T) {
		defer SetRESTClient(nil)
		isolateGitHubAuthEnv(t)

		mockClient := newMockRESTClient(t, staticJSONHandler(http.StatusInternalServerError, ""))
		SetRESTClient(mockClient)

		err := MarkNotificationRead("123")
		require.Error(t, err)
	})
}

func TestMarkNotificationDoneAndReadConvergeToTheSameServerCall(t *testing.T) {
	t.Run(
		"both functions post to the same mark as done endpoint for the same thread id",
		func(t *testing.T) {
			defer SetRESTClient(nil)
			isolateGitHubAuthEnv(t)

			var doneCall, readCall string

			doneClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
				doneCall = r.Method + " " + r.URL.Path
				w.WriteHeader(http.StatusNoContent)
			})
			SetRESTClient(doneClient)
			require.NoError(t, MarkNotificationDone("456"))

			readClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
				readCall = r.Method + " " + r.URL.Path
				w.WriteHeader(http.StatusNoContent)
			})
			SetRESTClient(readClient)
			require.NoError(t, MarkNotificationRead("456"))

			assert.NotEmpty(t, doneCall)
			assert.Equal(t, doneCall, readCall)
		},
	)
}

func TestMarkAllNotificationsRead(t *testing.T) {
	t.Run("posts to the mark all todos as done endpoint", func(t *testing.T) {
		defer SetRESTClient(nil)
		isolateGitHubAuthEnv(t)

		var gotMethod, gotPath string
		mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			gotPath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		})
		SetRESTClient(mockClient)

		err := MarkAllNotificationsRead()
		require.NoError(t, err)
		assert.Equal(t, http.MethodPost, gotMethod)
		assert.Contains(t, gotPath, "/todos/mark_as_done")
	})

	t.Run("propagates an error when the server responds with http 500", func(t *testing.T) {
		defer SetRESTClient(nil)
		isolateGitHubAuthEnv(t)

		mockClient := newMockRESTClient(t, staticJSONHandler(http.StatusInternalServerError, ""))
		SetRESTClient(mockClient)

		err := MarkAllNotificationsRead()
		require.Error(t, err)
	})
}

func TestUnsubscribeFromThread(t *testing.T) {
	t.Run("returns the expected error without making any http call", func(t *testing.T) {
		defer SetRESTClient(nil)
		isolateGitHubAuthEnv(t)

		httpCalled := false
		mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
			httpCalled = true
			w.WriteHeader(http.StatusOK)
		})
		SetRESTClient(mockClient)

		err := UnsubscribeFromThread("123")
		require.Error(t, err)
		assert.Equal(t, "unsubscribe is not supported for GitLab todos", err.Error())
		assert.False(t, httpCalled)
	})
}

func TestFetchPipelineForTodo(t *testing.T) {
	t.Run("returns pipeline status and url on success", func(t *testing.T) {
		defer SetRESTClient(nil)

		mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch {
			case strings.Contains(r.URL.Path, "/merge_requests/9/pipelines"):
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(
					`[{"id":900,"status":"success","web_url":"https://gitlab.com/group/proj/-/pipelines/900"}]`,
				))
			default:
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`[]`))
			}
		})
		SetRESTClient(mockClient)

		enrichment, err := FetchPipelineForTodo("group/proj", 9)
		require.NoError(t, err)
		require.NotNil(t, enrichment)
		assert.Equal(t, StatusSuccess, enrichment.Status)
		assert.Equal(t, "https://gitlab.com/group/proj/-/pipelines/900", enrichment.Url)
	})

	t.Run("returns nil without error when the mr has no pipeline yet", func(t *testing.T) {
		defer SetRESTClient(nil)

		mockClient := newMockRESTClient(t, staticJSONHandler(http.StatusOK, `[]`))
		SetRESTClient(mockClient)

		enrichment, err := FetchPipelineForTodo("group/proj", 9)
		require.NoError(t, err)
		assert.Nil(t, enrichment)
	})

	t.Run(
		"returns nil without error when the pipeline lookup fails with a server error",
		func(t *testing.T) {
			defer SetRESTClient(nil)

			mockClient := newMockRESTClient(
				t,
				staticJSONHandler(http.StatusInternalServerError, ""),
			)
			SetRESTClient(mockClient)

			var enrichment *PipelineEnrichment
			var err error
			require.NotPanics(t, func() {
				enrichment, err = FetchPipelineForTodo("group/proj", 9)
			})
			require.NoError(t, err)
			assert.Nil(t, enrichment)
		},
	)

	t.Run(
		"returns nil without error when the mr pipeline lookup responds not found",
		func(t *testing.T) {
			defer SetRESTClient(nil)

			mockClient := newMockRESTClient(t, staticJSONHandler(http.StatusNotFound, ""))
			SetRESTClient(mockClient)

			enrichment, err := FetchPipelineForTodo("group/proj", 9)
			require.NoError(t, err)
			assert.Nil(t, enrichment)
		},
	)
}
