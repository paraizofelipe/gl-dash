package data

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	graphql "github.com/cli/shurcooL-graphql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/gitlab"
)

func TestIssueNodeNormalizesState(t *testing.T) {
	// GitLab returns issue states lowercase (opened/closed/locked); the TUI
	// renders the GitHub-style uppercase OPEN/CLOSED, so the adapter must
	// normalize or the issue status glyph never shows.
	cases := map[string]string{
		"opened": "OPEN",
		"closed": "CLOSED",
		"locked": "OPEN",
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			n := issueNode{State: in}
			assert.Equal(t, want, n.toIssueData("").State)
		})
	}
}

func TestFetchIssues(t *testing.T) {
	t.Run("project scoped query maps issue fields to IssueData", func(t *testing.T) {
		defer SetClient(nil)

		responseBody := `{"data":{"project":{"issues":{"nodes":[{
			"iid":"7","title":"Something broken","description":"desc","state":"opened",
			"author":{"username":"jdoe"},
			"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
			"webUrl":"https://gitlab.com/group/proj/-/issues/7"
		}],"count":1,"pageInfo":{"hasNextPage":false,"startCursor":"","endCursor":""}}}}}`

		mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, responseBody))
		SetClient(mockClient)

		resp, err := FetchIssues("project:group/proj", 30, nil)
		require.NoError(t, err)
		require.Len(t, resp.Issues, 1)

		issue := resp.Issues[0]
		wantCreatedAt, timeErr := time.Parse(time.RFC3339, "2026-01-01T00:00:00Z")
		require.NoError(t, timeErr)
		wantUpdatedAt, timeErr := time.Parse(time.RFC3339, "2026-01-02T00:00:00Z")
		require.NoError(t, timeErr)

		assert.Equal(t, 7, issue.Number)
		assert.Equal(t, "Something broken", issue.Title)
		assert.Equal(t, "desc", issue.Body)
		assert.Equal(t, "OPEN", issue.State)
		assert.Equal(t, "jdoe", issue.Author.Login)
		assert.Equal(t, "https://gitlab.com/group/proj/-/issues/7", issue.Url)
		assert.Equal(t, wantCreatedAt, issue.CreatedAt)
		assert.Equal(t, wantUpdatedAt, issue.UpdatedAt)
		assert.Equal(t, "group", issue.Repository.Owner.Login)
		assert.Equal(t, "proj", issue.Repository.Name)
		assert.Equal(t, "group/proj", issue.Repository.NameWithOwner)
		assert.Equal(t, 1, resp.TotalCount)
	})

	t.Run(
		"user scoped query maps root level issues and leaves repository empty",
		func(t *testing.T) {
			defer SetClient(nil)
			defer gitlab.SetClients(nil, nil)

			currentUserResponseBody := `{"data":{"currentUser":{"username":"jdoe"}}}`
			issuesResponseBody := `{"data":{"issues":{"nodes":[{
			"iid":"9","title":"My issue","description":"body","state":"closed",
			"author":{"username":"jdoe"},
			"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
			"webUrl":"https://gitlab.com/group/proj/-/issues/9"
		}],"count":1,"pageInfo":{"hasNextPage":false,"startCursor":"","endCursor":""}}}}`

			handler := func(w http.ResponseWriter, r *http.Request) {
				body := decodeGraphQLRequestBody(t, r)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				if strings.Contains(body.Query, "issues(") {
					_, _ = w.Write([]byte(issuesResponseBody))
					return
				}
				_, _ = w.Write([]byte(currentUserResponseBody))
			}

			mockClient := newMockGraphQLClient(t, handler)
			SetClient(mockClient)
			gitlab.SetClients(nil, mockClient)

			resp, err := FetchIssues("is:closed", 30, nil)
			require.NoError(t, err)
			require.Len(t, resp.Issues, 1)

			issue := resp.Issues[0]
			assert.Equal(t, 9, issue.Number)
			assert.Equal(t, "My issue", issue.Title)
			assert.Equal(t, "CLOSED", issue.State)
			assert.Equal(t, "", issue.Repository.Owner.Login)
			assert.Equal(t, "", issue.Repository.Name)
			assert.Equal(t, "", issue.Repository.NameWithOwner)
		},
	)

	t.Run("labels and assignees are populated from the response when present", func(t *testing.T) {
		defer SetClient(nil)

		responseBody := `{"data":{"project":{"issues":{"nodes":[{
			"iid":"3","title":"Has labels and assignees","description":"","state":"opened",
			"author":{"username":"jdoe"},
			"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
			"webUrl":"https://gitlab.com/group/proj/-/issues/3",
			"labels":{"nodes":[{"title":"bug","color":"#ff0000","description":"Bug label"}]},
			"assignees":{"nodes":[{"username":"alice"},{"username":"bob"}]}
		}],"count":1,"pageInfo":{"hasNextPage":false,"startCursor":"","endCursor":""}}}}}`

		mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, responseBody))
		SetClient(mockClient)

		resp, err := FetchIssues("project:group/proj", 30, nil)
		require.NoError(t, err)
		require.Len(t, resp.Issues, 1)

		issue := resp.Issues[0]
		require.Len(t, issue.Labels.Nodes, 1)
		assert.Equal(
			t,
			Label{Name: "bug", Color: "#ff0000", Description: "Bug label"},
			issue.Labels.Nodes[0],
		)
		require.Len(t, issue.Assignees.Nodes, 2)
		assert.Equal(t, Assignee{Login: "alice"}, issue.Assignees.Nodes[0])
		assert.Equal(t, Assignee{Login: "bob"}, issue.Assignees.Nodes[1])
	})

	t.Run("no labels or assignees in the response maps to empty slices", func(t *testing.T) {
		defer SetClient(nil)

		responseBody := `{"data":{"project":{"issues":{"nodes":[{
			"iid":"4","title":"No labels or assignees","description":"","state":"opened",
			"author":{"username":"jdoe"},
			"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
			"webUrl":"https://gitlab.com/group/proj/-/issues/4",
			"labels":{"nodes":[]},
			"assignees":{"nodes":[]}
		}],"count":1,"pageInfo":{"hasNextPage":false,"startCursor":"","endCursor":""}}}}}`

		mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, responseBody))
		SetClient(mockClient)

		resp, err := FetchIssues("project:group/proj", 30, nil)
		require.NoError(t, err)
		require.Len(t, resp.Issues, 1)

		issue := resp.Issues[0]
		require.Empty(t, issue.Labels.Nodes)
		require.Empty(t, issue.Assignees.Nodes)
	})

	t.Run(
		"nested subgroup project path splits owner and name on the last slash",
		func(t *testing.T) {
			defer SetClient(nil)

			responseBody := `{"data":{"project":{"issues":{"nodes":[{
			"iid":"1","title":"Nested","description":"","state":"opened",
			"author":{"username":"jdoe"},
			"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
			"webUrl":"https://gitlab.com/group/subgroup/proj/-/issues/1"
		}],"count":1,"pageInfo":{"hasNextPage":false,"startCursor":"","endCursor":""}}}}}`

			mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, responseBody))
			SetClient(mockClient)

			resp, err := FetchIssues("project:group/subgroup/proj", 30, nil)
			require.NoError(t, err)
			require.Len(t, resp.Issues, 1)

			assert.Equal(t, "group/subgroup", resp.Issues[0].Repository.Owner.Login)
			assert.Equal(t, "proj", resp.Issues[0].Repository.Name)
		},
	)

	t.Run("page info and total count are passed through from the response", func(t *testing.T) {
		defer SetClient(nil)

		responseBody := `{"data":{"project":{"issues":{"nodes":[],"count":55,"pageInfo":{"hasNextPage":true,"startCursor":"s1","endCursor":"e1"}}}}}`
		mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, responseBody))
		SetClient(mockClient)

		resp, err := FetchIssues("project:group/proj", 30, &PageInfo{EndCursor: "prev-cursor"})
		require.NoError(t, err)

		assert.True(t, resp.PageInfo.HasNextPage)
		assert.Equal(t, "s1", resp.PageInfo.StartCursor)
		assert.Equal(t, "e1", resp.PageInfo.EndCursor)
		assert.Equal(t, 55, resp.TotalCount)
	})

	t.Run("propagates error when the server responds with http 500", func(t *testing.T) {
		defer SetClient(nil)

		mockClient := newMockGraphQLClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})
		SetClient(mockClient)

		_, err := FetchIssues("is:open", 30, nil)
		require.Error(t, err)
	})

	t.Run("propagates error when the response contains graphql errors", func(t *testing.T) {
		defer SetClient(nil)

		responseBody := `{"data":null,"errors":[{"message":"boom"}]}`
		mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, responseBody))
		SetClient(mockClient)

		_, err := FetchIssues("is:open", 30, nil)
		require.Error(t, err)
	})
}

func TestFetchIssues_TranslatesAuthorMeAndLabelsIntoRequestVariables(t *testing.T) {
	defer SetClient(nil)
	defer gitlab.SetClients(nil, nil)

	currentUserResponseBody := `{"data":{"currentUser":{"username":"jdoe"}}}`
	issuesResponseBody := `{"data":{"project":{"issues":{"nodes":[{
		"iid":"7","title":"Something broken","description":"desc","state":"opened",
		"author":{"username":"jdoe"},
		"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
		"webUrl":"https://gitlab.com/group/proj/-/issues/7"
	}],"count":1,"pageInfo":{"hasNextPage":false,"startCursor":"","endCursor":""}}}}}`

	var capturedVariables map[string]any
	var mu sync.Mutex

	handler := func(w http.ResponseWriter, r *http.Request) {
		body := decodeGraphQLRequestBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if strings.Contains(body.Query, "issues") {
			mu.Lock()
			capturedVariables = body.Variables
			mu.Unlock()
			_, _ = w.Write([]byte(issuesResponseBody))
			return
		}
		_, _ = w.Write([]byte(currentUserResponseBody))
	}

	server := newMockGraphQLServer(t, handler)
	mockClient := graphql.NewClient(server.URL+"/api/graphql", server.Client())
	SetClient(mockClient)
	gitlab.SetClients(nil, mockClient)

	resp, err := FetchIssues("project:group/proj author:@me label:bug", 30, nil)
	require.NoError(t, err)
	require.Len(t, resp.Issues, 1)

	mu.Lock()
	defer mu.Unlock()
	require.NotNil(t, capturedVariables)
	assert.Equal(t, "jdoe", capturedVariables["authorUsername"])
	labelName, ok := capturedVariables["labelName"].([]any)
	require.True(
		t,
		ok,
		"expected labelName variable to be an array, got %T",
		capturedVariables["labelName"],
	)
	assert.Contains(t, labelName, "bug")
	_, hasOldLabelsKey := capturedVariables["labels"]
	assert.False(
		t,
		hasOldLabelsKey,
		"expected no legacy labels variable to be sent for issues, GitLab issues only accept labelName",
	)
}

func TestFetchIssues_WithoutProjectUsesRootLevelIssuesQueryNotCurrentUser(t *testing.T) {
	defer SetClient(nil)
	defer gitlab.SetClients(nil, nil)

	currentUserResponseBody := `{"data":{"currentUser":{"username":"jdoe"}}}`
	issuesResponseBody := `{"data":{"issues":{"nodes":[{
		"iid":"9","title":"My issue","description":"body","state":"opened",
		"author":{"username":"jdoe"},
		"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
		"webUrl":"https://gitlab.com/group/proj/-/issues/9"
	}],"count":1,"pageInfo":{"hasNextPage":false,"startCursor":"","endCursor":""}}}}`

	var capturedQuery string
	var capturedVariables map[string]any
	var mu sync.Mutex

	handler := func(w http.ResponseWriter, r *http.Request) {
		body := decodeGraphQLRequestBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if strings.Contains(body.Query, "issues(") {
			mu.Lock()
			capturedQuery = body.Query
			capturedVariables = body.Variables
			mu.Unlock()
			_, _ = w.Write([]byte(issuesResponseBody))
			return
		}
		_, _ = w.Write([]byte(currentUserResponseBody))
	}

	server := newMockGraphQLServer(t, handler)
	mockClient := graphql.NewClient(server.URL+"/api/graphql", server.Client())
	SetClient(mockClient)
	gitlab.SetClients(nil, mockClient)

	resp, err := FetchIssues("is:open", 30, nil)
	require.NoError(t, err)
	require.Len(t, resp.Issues, 1)
	assert.Equal(t, "My issue", resp.Issues[0].Title)

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, capturedQuery)
	assert.Contains(t, capturedQuery, "issues(")
	assert.NotContains(t, capturedQuery, "currentUser")
	assert.NotContains(t, capturedQuery, "authoredIssues")
	require.NotNil(t, capturedVariables)
	assert.Equal(t, "jdoe", capturedVariables["authorUsername"])
	assert.Nil(
		t,
		capturedVariables["labelName"],
		"labelName should be absent or null, not an empty array, when no label qualifier is present in the search",
	)
}

func TestFetchIssues_AssigneeMeWithoutProjectOrAuthorLeavesAuthorUsernameAbsent(t *testing.T) {
	defer SetClient(nil)
	defer gitlab.SetClients(nil, nil)

	currentUserResponseBody := `{"data":{"currentUser":{"username":"jdoe"}}}`
	issuesResponseBody := `{"data":{"issues":{"nodes":[],"count":0,"pageInfo":{"hasNextPage":false,"startCursor":"","endCursor":""}}}}`

	var capturedVariables map[string]any
	var mu sync.Mutex

	handler := func(w http.ResponseWriter, r *http.Request) {
		body := decodeGraphQLRequestBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if strings.Contains(body.Query, "issues(") {
			mu.Lock()
			capturedVariables = body.Variables
			mu.Unlock()
			_, _ = w.Write([]byte(issuesResponseBody))
			return
		}
		_, _ = w.Write([]byte(currentUserResponseBody))
	}

	server := newMockGraphQLServer(t, handler)
	mockClient := graphql.NewClient(server.URL+"/api/graphql", server.Client())
	SetClient(mockClient)
	gitlab.SetClients(nil, mockClient)

	resp, err := FetchIssues("assignee:@me", 30, nil)
	require.NoError(t, err)
	require.Empty(t, resp.Issues)

	mu.Lock()
	defer mu.Unlock()
	require.NotNil(t, capturedVariables)
	assert.Nil(
		t,
		capturedVariables["authorUsername"],
		"authorUsername should be absent or null when only assignee is specified in the search",
	)
	assert.Equal(t, "jdoe", capturedVariables["assigneeUsername"])
}

func TestFetchIssues_ProjectScopedClosedStateSendsClosedStateVariableWithIssuableStateType(
	t *testing.T,
) {
	defer SetClient(nil)

	currentUserResponseBody := `{"data":{"currentUser":{"username":"jdoe"}}}`
	issuesResponseBody := `{"data":{"project":{"issues":{"nodes":[],"count":0,"pageInfo":{"hasNextPage":false,"startCursor":"","endCursor":""}}}}}`

	var capturedBody graphQLRequestBody
	var mu sync.Mutex

	handler := func(w http.ResponseWriter, r *http.Request) {
		body := decodeGraphQLRequestBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if strings.Contains(body.Query, "issues(") {
			mu.Lock()
			capturedBody = body
			mu.Unlock()
			_, _ = w.Write([]byte(issuesResponseBody))
			return
		}
		_, _ = w.Write([]byte(currentUserResponseBody))
	}

	mockClient := newMockGraphQLClient(t, handler)
	SetClient(mockClient)

	_, err := FetchIssues("project:group/proj is:closed", 30, nil)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "closed", capturedBody.Variables["state"])
	assert.Contains(
		t,
		capturedBody.Query,
		"$state:IssuableState",
		"expected the state variable to be declared as IssuableState so GitLab accepts it, got query: %s",
		capturedBody.Query,
	)
	assert.Nil(
		t,
		capturedBody.Variables["labelName"],
		"labelName should be absent or null, not an empty array, when no label qualifier is present in the search",
	)
}

func TestFetchIssues_ProjectScopedDeclaresFullPathAsGraphQLID(t *testing.T) {
	defer SetClient(nil)

	currentUserResponseBody := `{"data":{"currentUser":{"username":"jdoe"}}}`
	issuesResponseBody := `{"data":{"project":{"issues":{"nodes":[],"count":0,"pageInfo":{"hasNextPage":false,"startCursor":"","endCursor":""}}}}}`

	var capturedBody graphQLRequestBody
	var mu sync.Mutex

	handler := func(w http.ResponseWriter, r *http.Request) {
		body := decodeGraphQLRequestBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if strings.Contains(body.Query, "issues(") {
			mu.Lock()
			capturedBody = body
			mu.Unlock()
			_, _ = w.Write([]byte(issuesResponseBody))
			return
		}
		_, _ = w.Write([]byte(currentUserResponseBody))
	}

	mockClient := newMockGraphQLClient(t, handler)
	SetClient(mockClient)

	_, err := FetchIssues("project:group/proj", 30, nil)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, capturedBody.Query)
	assert.Contains(
		t,
		capturedBody.Query,
		"$fullPath:ID!",
		"Query.project(fullPath:) requires ID in the real GitLab schema, got query: %s",
		capturedBody.Query,
	)
	assert.NotContains(t, capturedBody.Query, "$fullPath:String!")
	assert.Equal(t, "group/proj", capturedBody.Variables["fullPath"])
}

func TestFetchIssues_ProjectScopedIsMergedQualifierOmitsStateVariableAndLogsWarning(t *testing.T) {
	defer SetClient(nil)

	logBuf := captureLogOutput(t)

	currentUserResponseBody := `{"data":{"currentUser":{"username":"jdoe"}}}`
	issuesResponseBody := `{"data":{"project":{"issues":{"nodes":[],"count":0,"pageInfo":{"hasNextPage":false,"startCursor":"","endCursor":""}}}}}`

	var capturedBody graphQLRequestBody
	var mu sync.Mutex

	handler := func(w http.ResponseWriter, r *http.Request) {
		body := decodeGraphQLRequestBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if strings.Contains(body.Query, "issues(") {
			mu.Lock()
			capturedBody = body
			mu.Unlock()
			_, _ = w.Write([]byte(issuesResponseBody))
			return
		}
		_, _ = w.Write([]byte(currentUserResponseBody))
	}

	mockClient := newMockGraphQLClient(t, handler)
	SetClient(mockClient)

	_, err := FetchIssues("project:group/proj is:merged", 30, nil)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	assert.Nil(
		t,
		capturedBody.Variables["state"],
		"is:merged has no IssuableState equivalent, so state should be omitted rather than sent as an invalid value",
	)
	assert.Contains(
		t,
		logBuf.String(),
		"WARN",
		"expected a warning to be logged when is:merged is applied to an issue search, since IssuableState has no merged value",
	)
}

func TestFetchIssues_IsMergedWithoutProjectDoesNotBreakSearchAndOmitsStateVariable(t *testing.T) {
	defer SetClient(nil)
	defer gitlab.SetClients(nil, nil)

	currentUserResponseBody := `{"data":{"currentUser":{"username":"jdoe"}}}`
	issuesResponseBody := `{"data":{"issues":{"nodes":[{
		"iid":"20","title":"Root level issue","description":"","state":"opened",
		"author":{"username":"jdoe"},
		"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
		"webUrl":"https://gitlab.com/group/proj/-/issues/20"
	}],"count":1,"pageInfo":{"hasNextPage":false,"startCursor":"","endCursor":""}}}}`

	var capturedVariables map[string]any
	var mu sync.Mutex

	handler := func(w http.ResponseWriter, r *http.Request) {
		body := decodeGraphQLRequestBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if strings.Contains(body.Query, "issues(") {
			mu.Lock()
			capturedVariables = body.Variables
			mu.Unlock()
			_, _ = w.Write([]byte(issuesResponseBody))
			return
		}
		_, _ = w.Write([]byte(currentUserResponseBody))
	}

	server := newMockGraphQLServer(t, handler)
	mockClient := graphql.NewClient(server.URL+"/api/graphql", server.Client())
	SetClient(mockClient)
	gitlab.SetClients(nil, mockClient)

	resp, err := FetchIssues("is:merged", 30, nil)
	require.NoError(t, err)
	require.Len(t, resp.Issues, 1)
	assert.Equal(t, "Root level issue", resp.Issues[0].Title)

	mu.Lock()
	defer mu.Unlock()
	require.NotNil(t, capturedVariables)
	assert.Nil(
		t,
		capturedVariables["state"],
		"is:merged has no IssuableState equivalent, so state should be omitted rather than sent as an invalid value",
	)
}

func TestFetchIssues_UnscopedSearchReturnsErrorWhenCurrentUserCannotBeResolved(t *testing.T) {
	defer SetClient(nil)
	defer gitlab.SetClients(nil, nil)

	mockClient := newMockGraphQLClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	SetClient(mockClient)
	gitlab.SetClients(nil, mockClient)

	_, err := FetchIssues("", 30, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot resolve current user for an unscoped issue search")
}

func TestFetchIssues_UnscopedSearchReturnsErrorWhenAnonymous(t *testing.T) {
	defer SetClient(nil)
	defer gitlab.SetClients(nil, nil)

	mockClient := newMockGraphQLClient(
		t,
		staticJSONHandler(http.StatusOK, `{"data":{"currentUser":null}}`),
	)
	SetClient(mockClient)
	gitlab.SetClients(nil, mockClient)

	_, err := FetchIssues("", 30, nil)
	require.Error(t, err)
	assert.Equal(t, "cannot resolve current user for an unscoped issue search", err.Error())
}

func TestParseIssueUrl(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		wantPath    string
		wantIid     string
		expectError bool
	}{
		{
			name:     "valid url with a nested subgroup",
			url:      "https://gitlab.com/group/subgroup/proj/-/issues/42",
			wantPath: "group/subgroup/proj",
			wantIid:  "42",
		},
		{
			name:     "valid url with a single level namespace",
			url:      "https://gitlab.com/group/proj/-/issues/7",
			wantPath: "group/proj",
			wantIid:  "7",
		},
		{
			name:        "url without an issues segment returns an error",
			url:         "https://gitlab.com/group/proj",
			expectError: true,
		},
		{
			name:        "malformed url returns an error",
			url:         "https://gitlab.com/group/proj/-/issues/42\x00",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fullPath, iid, err := parseIssueUrl(tt.url)
			if tt.expectError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantPath, fullPath)
			assert.Equal(t, tt.wantIid, iid)
		})
	}
}

func TestFetchIssue(t *testing.T) {
	t.Run("maps a single issue to IssueData", func(t *testing.T) {
		defer SetClient(nil)

		responseBody := `{"data":{"project":{"issue":{
			"iid":"7","title":"Something broken","description":"desc","state":"opened",
			"author":{"username":"jdoe"},
			"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
			"webUrl":"https://gitlab.com/group/proj/-/issues/7",
			"labels":{"nodes":[{"title":"bug","color":"#ff0000","description":"Bug label"}]}
		}}}}`

		mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, responseBody))
		SetClient(mockClient)

		issue, err := FetchIssue("https://gitlab.com/group/proj/-/issues/7")
		require.NoError(t, err)

		wantCreatedAt, timeErr := time.Parse(time.RFC3339, "2026-01-01T00:00:00Z")
		require.NoError(t, timeErr)
		wantUpdatedAt, timeErr := time.Parse(time.RFC3339, "2026-01-02T00:00:00Z")
		require.NoError(t, timeErr)

		assert.Equal(t, 7, issue.Number)
		assert.Equal(t, "Something broken", issue.Title)
		assert.Equal(t, "desc", issue.Body)
		assert.Equal(t, "OPEN", issue.State)
		assert.Equal(t, "jdoe", issue.Author.Login)
		assert.Equal(t, wantCreatedAt, issue.CreatedAt)
		assert.Equal(t, wantUpdatedAt, issue.UpdatedAt)
		assert.Equal(t, "https://gitlab.com/group/proj/-/issues/7", issue.Url)
		require.Len(t, issue.Labels.Nodes, 1)
		assert.Equal(
			t,
			Label{Name: "bug", Color: "#ff0000", Description: "Bug label"},
			issue.Labels.Nodes[0],
		)
		assert.Equal(t, "group", issue.Repository.Owner.Login)
		assert.Equal(t, "proj", issue.Repository.Name)
		assert.Equal(t, "group/proj", issue.Repository.NameWithOwner)
	})

	t.Run("populates assignees from the response", func(t *testing.T) {
		defer SetClient(nil)

		responseBody := `{"data":{"project":{"issue":{
			"iid":"7","title":"Something broken","description":"","state":"opened",
			"author":{"username":"jdoe"},
			"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
			"webUrl":"https://gitlab.com/group/proj/-/issues/7",
			"labels":{"nodes":[]},
			"assignees":{"nodes":[{"username":"alice"},{"username":"bob"}]}
		}}}}`

		mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, responseBody))
		SetClient(mockClient)

		issue, err := FetchIssue("https://gitlab.com/group/proj/-/issues/7")
		require.NoError(t, err)

		require.Len(t, issue.Assignees.Nodes, 2)
		assert.Equal(t, Assignee{Login: "alice"}, issue.Assignees.Nodes[0])
		assert.Equal(t, Assignee{Login: "bob"}, issue.Assignees.Nodes[1])
	})

	t.Run("propagates error when the server responds with http 500", func(t *testing.T) {
		defer SetClient(nil)

		mockClient := newMockGraphQLClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})
		SetClient(mockClient)

		_, err := FetchIssue("https://gitlab.com/group/proj/-/issues/7")
		require.Error(t, err)
	})

	t.Run("propagates error when the response contains graphql errors", func(t *testing.T) {
		defer SetClient(nil)

		responseBody := `{"data":null,"errors":[{"message":"boom"}]}`
		mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, responseBody))
		SetClient(mockClient)

		_, err := FetchIssue("https://gitlab.com/group/proj/-/issues/7")
		require.Error(t, err)
	})

	t.Run(
		"propagates error when the url has no issue segment to parse",
		func(t *testing.T) {
			defer SetClient(nil)

			mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, `{}`))
			SetClient(mockClient)

			_, err := FetchIssue("https://gitlab.com/group/proj")
			require.Error(t, err)
		},
	)
}

func TestFetchIssue_DeclaresFullPathAsGraphQLID(t *testing.T) {
	defer SetClient(nil)

	responseBody := `{"data":{"project":{"issue":{
		"iid":"7","title":"Something broken","description":"","state":"opened",
		"author":{"username":"jdoe"},
		"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
		"webUrl":"https://gitlab.com/group/proj/-/issues/7",
		"labels":{"nodes":[]}
	}}}}`

	var capturedBody graphQLRequestBody
	var mu sync.Mutex

	handler := func(w http.ResponseWriter, r *http.Request) {
		body := decodeGraphQLRequestBody(t, r)
		mu.Lock()
		capturedBody = body
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(responseBody))
	}

	mockClient := newMockGraphQLClient(t, handler)
	SetClient(mockClient)

	_, err := FetchIssue("https://gitlab.com/group/proj/-/issues/7")
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, capturedBody.Query)
	assert.Contains(
		t,
		capturedBody.Query,
		"$fullPath:ID!",
		"Query.project(fullPath:) requires ID in the real GitLab schema, got query: %s",
		capturedBody.Query,
	)
	assert.NotContains(t, capturedBody.Query, "$fullPath:String!")
	assert.Contains(
		t,
		capturedBody.Query,
		"$iid:String!",
		"Project.issue(iid:) requires String in the real GitLab schema, same as Project.mergeRequest(iid:), got query: %s",
		capturedBody.Query,
	)
	assert.Equal(t, "group/proj", capturedBody.Variables["fullPath"])
	assert.Equal(t, "7", capturedBody.Variables["iid"])
}

func TestCommentsFromDiscussions(t *testing.T) {
	updatedAt := time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC)

	t.Run("note becomes an issue comment", func(t *testing.T) {
		discussions := []gitlabDiscussionNode{
			discussionWithNotes(gitlabNoteNode{
				Author:    usernameAuthor("alice"),
				Body:      "thanks for reporting",
				UpdatedAt: updatedAt,
				System:    false,
			}),
		}

		comments := commentsFromDiscussions(discussions)

		require.Len(t, comments.Nodes, 1)
		assert.Equal(t, "alice", comments.Nodes[0].Author.Login)
		assert.Equal(t, "thanks for reporting", comments.Nodes[0].Body)
		assert.Equal(t, updatedAt, comments.Nodes[0].UpdatedAt)
	})

	t.Run("filters system notes", func(t *testing.T) {
		discussions := []gitlabDiscussionNode{
			discussionWithNotes(
				gitlabNoteNode{
					Author: usernameAuthor("ghost"),
					Body:   "changed the label",
					System: true,
				},
				gitlabNoteNode{
					Author: usernameAuthor("alice"),
					Body:   "real comment",
					System: false,
				},
			),
		}

		comments := commentsFromDiscussions(discussions)

		require.Len(t, comments.Nodes, 1)
		assert.Equal(t, "alice", comments.Nodes[0].Author.Login)
	})

	t.Run("empty discussions returns empty comments", func(t *testing.T) {
		comments := commentsFromDiscussions(nil)

		assert.Empty(t, comments.Nodes)
	})
}

func TestFetchIssue_PopulatesCommentsFromDiscussionNote(t *testing.T) {
	defer SetClient(nil)

	responseBody := `{"data":{"project":{"issue":{
		"iid":"7","title":"Something broken","description":"","state":"opened",
		"author":{"username":"jdoe"},
		"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
		"webUrl":"https://gitlab.com/group/proj/-/issues/7",
		"labels":{"nodes":[]},
		"discussions":{"nodes":[
			{"notes":{"nodes":[
				{"author":{"username":"alice"},"body":"thanks for reporting","updatedAt":"2026-01-04T00:00:00Z","system":false}
			]}}
		]},
		"upvotes":0,"downvotes":0
	}}}}`

	mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, responseBody))
	SetClient(mockClient)

	issue, err := FetchIssue("https://gitlab.com/group/proj/-/issues/7")
	require.NoError(t, err)

	wantUpdatedAt, timeErr := time.Parse(time.RFC3339, "2026-01-04T00:00:00Z")
	require.NoError(t, timeErr)

	require.Len(t, issue.Comments.Nodes, 1)
	assert.Equal(t, "alice", issue.Comments.Nodes[0].Author.Login)
	assert.Equal(t, "thanks for reporting", issue.Comments.Nodes[0].Body)
	assert.Equal(t, wantUpdatedAt, issue.Comments.Nodes[0].UpdatedAt)
}

func TestFetchIssue_FiltersSystemNoteFromComments(t *testing.T) {
	defer SetClient(nil)

	responseBody := `{"data":{"project":{"issue":{
		"iid":"7","title":"Something broken","description":"","state":"opened",
		"author":{"username":"jdoe"},
		"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
		"webUrl":"https://gitlab.com/group/proj/-/issues/7",
		"labels":{"nodes":[]},
		"discussions":{"nodes":[
			{"notes":{"nodes":[
				{"author":{"username":"ghost"},"body":"changed the label","system":true},
				{"author":{"username":"alice"},"body":"real comment","system":false}
			]}}
		]},
		"upvotes":0,"downvotes":0
	}}}}`

	mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, responseBody))
	SetClient(mockClient)

	issue, err := FetchIssue("https://gitlab.com/group/proj/-/issues/7")
	require.NoError(t, err)

	require.Len(t, issue.Comments.Nodes, 1)
	assert.Equal(t, "alice", issue.Comments.Nodes[0].Author.Login)
}

func TestFetchIssue_ReactionsTotalCountSumsUpvotesAndDownvotes(t *testing.T) {
	defer SetClient(nil)

	responseBody := `{"data":{"project":{"issue":{
		"iid":"7","title":"Something broken","description":"","state":"opened",
		"author":{"username":"jdoe"},
		"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
		"webUrl":"https://gitlab.com/group/proj/-/issues/7",
		"labels":{"nodes":[]},
		"discussions":{"nodes":[]},
		"upvotes":3,"downvotes":1
	}}}}`

	mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, responseBody))
	SetClient(mockClient)

	issue, err := FetchIssue("https://gitlab.com/group/proj/-/issues/7")
	require.NoError(t, err)

	assert.Equal(t, 4, issue.Reactions.TotalCount)
}

func TestFetchIssue_EmptyDiscussionsAndZeroVotesLeaveCommentsAndReactionsEmpty(t *testing.T) {
	defer SetClient(nil)

	responseBody := `{"data":{"project":{"issue":{
		"iid":"7","title":"Something broken","description":"","state":"opened",
		"author":{"username":"jdoe"},
		"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
		"webUrl":"https://gitlab.com/group/proj/-/issues/7",
		"labels":{"nodes":[]},
		"discussions":{"nodes":[]},
		"upvotes":0,"downvotes":0
	}}}}`

	mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, responseBody))
	SetClient(mockClient)

	issue, err := FetchIssue("https://gitlab.com/group/proj/-/issues/7")
	require.NoError(t, err)

	assert.Empty(t, issue.Comments.Nodes)
	assert.Equal(t, 0, issue.Reactions.TotalCount)
}

func TestFetchIssues_ListingQueryDoesNotDeclareDiscussionsField(t *testing.T) {
	defer SetClient(nil)

	currentUserResponseBody := `{"data":{"currentUser":{"username":"jdoe"}}}`
	issuesResponseBody := `{"data":{"project":{"issues":{"nodes":[{
		"iid":"7","title":"Something broken","description":"desc","state":"opened",
		"author":{"username":"jdoe"},
		"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
		"webUrl":"https://gitlab.com/group/proj/-/issues/7"
	}],"count":1,"pageInfo":{"hasNextPage":false,"startCursor":"","endCursor":""}}}}}`

	var capturedBody graphQLRequestBody
	var mu sync.Mutex

	handler := func(w http.ResponseWriter, r *http.Request) {
		body := decodeGraphQLRequestBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if strings.Contains(body.Query, "issues(") {
			mu.Lock()
			capturedBody = body
			mu.Unlock()
			_, _ = w.Write([]byte(issuesResponseBody))
			return
		}
		_, _ = w.Write([]byte(currentUserResponseBody))
	}

	mockClient := newMockGraphQLClient(t, handler)
	SetClient(mockClient)

	_, err := FetchIssues("project:group/proj", 30, nil)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, capturedBody.Query)
	assert.NotContains(t, capturedBody.Query, "discussions")
}
