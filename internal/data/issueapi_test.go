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
		assert.Equal(t, "opened", issue.State)
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
			assert.Equal(t, "closed", issue.State)
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
