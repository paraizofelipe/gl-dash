package data

import (
	"fmt"
	"io"
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

func TestClearEnrichmentCache(t *testing.T) {
	// Save original state
	originalCachedClient := cachedClient
	defer func() {
		cachedClient = originalCachedClient
	}()

	t.Run("clears nil cache without panic", func(t *testing.T) {
		cachedClient = nil
		require.True(t, IsEnrichmentCacheCleared(), "cache should be cleared initially")

		ClearEnrichmentCache()
		require.True(t, IsEnrichmentCacheCleared(), "cache should remain cleared")
	})

	t.Run("clears non-nil cache", func(t *testing.T) {
		// Simulate having a cached client (we use an empty struct pointer
		// since we can't create a real GraphQL client without credentials)
		cachedClient = &graphql.Client{}
		require.False(
			t,
			IsEnrichmentCacheCleared(),
			"cache should not be cleared when client is set",
		)

		ClearEnrichmentCache()
		require.True(
			t,
			IsEnrichmentCacheCleared(),
			"cache should be cleared after ClearEnrichmentCache",
		)
	})
}

func TestIsEnrichmentCacheCleared(t *testing.T) {
	// Save original state
	originalCachedClient := cachedClient
	defer func() {
		cachedClient = originalCachedClient
	}()

	t.Run("returns true when cache is nil", func(t *testing.T) {
		cachedClient = nil
		require.True(t, IsEnrichmentCacheCleared())
	})

	t.Run("returns false when cache is set", func(t *testing.T) {
		cachedClient = &graphql.Client{}
		require.False(t, IsEnrichmentCacheCleared())
	})
}

func TestSetClient(t *testing.T) {
	// Save original state
	originalClient := client
	originalCachedClient := cachedClient
	defer func() {
		client = originalClient
		cachedClient = originalCachedClient
	}()

	t.Run("sets both client and cachedClient", func(t *testing.T) {
		client = nil
		cachedClient = nil

		// SetClient with nil should set both to nil
		SetClient(nil)
		require.Nil(t, client)
		require.True(t, IsEnrichmentCacheCleared())
	})
}

const singleMergeRequestProjectScopedResponse = `{"data":{"project":{"mergeRequests":{"nodes":[{
	"iid":"42","title":"Fix bug","state":"opened","draft":false,
	"author":{"username":"jdoe"},
	"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
	"webUrl":"https://gitlab.com/group/proj/-/merge_requests/42",
	"sourceBranch":"feature-x","targetBranch":"main",
	"detailedMergeStatus":"MERGEABLE","approved":true,
	"diffStatsSummary":{"additions":10,"deletions":2},
	"labels":{"nodes":[{"title":"bug","color":"#ff0000","description":"Bug label"}]}
}],"count":1,"pageInfo":{"hasNextPage":false,"startCursor":"","endCursor":""}}}}}`

func TestFetchPullRequests(t *testing.T) {
	t.Run("project scoped query maps merge request fields to PullRequestData", func(t *testing.T) {
		defer SetClient(nil)

		mockClient := newMockGraphQLClient(
			t,
			staticJSONHandler(http.StatusOK, singleMergeRequestProjectScopedResponse),
		)
		SetClient(mockClient)

		resp, err := FetchPullRequests("project:group/proj", 30, nil)
		require.NoError(t, err)
		require.Len(t, resp.Prs, 1)

		pr := resp.Prs[0]
		wantCreatedAt, timeErr := time.Parse(time.RFC3339, "2026-01-01T00:00:00Z")
		require.NoError(t, timeErr)
		wantUpdatedAt, timeErr := time.Parse(time.RFC3339, "2026-01-02T00:00:00Z")
		require.NoError(t, timeErr)

		assert.Equal(t, 42, pr.Number)
		assert.Equal(t, "Fix bug", pr.Title)
		assert.Equal(t, "jdoe", pr.Author.Login)
		assert.Equal(t, "", pr.AuthorAssociation)
		assert.Equal(t, "opened", pr.State)
		assert.False(t, pr.IsDraft)
		assert.Equal(t, "https://gitlab.com/group/proj/-/merge_requests/42", pr.Url)
		assert.Equal(t, "feature-x", pr.HeadRefName)
		assert.Equal(t, "main", pr.BaseRefName)
		assert.Equal(t, "MERGEABLE", pr.Mergeable)
		assert.Equal(t, "APPROVED", pr.ReviewDecision)
		assert.Equal(t, MergeStateStatus("CLEAN"), pr.MergeStateStatus)
		assert.False(t, pr.IsInMergeQueue)
		assert.Equal(t, 10, pr.Additions)
		assert.Equal(t, 2, pr.Deletions)
		assert.Equal(t, wantCreatedAt, pr.CreatedAt)
		assert.Equal(t, wantUpdatedAt, pr.UpdatedAt)
		require.Len(t, pr.Labels.Nodes, 1)
		assert.Equal(
			t,
			Label{Name: "bug", Color: "#ff0000", Description: "Bug label"},
			pr.Labels.Nodes[0],
		)
		assert.Equal(t, "group", pr.Repository.Owner.Login)
		assert.Equal(t, "proj", pr.Repository.Name)
		assert.Equal(t, "group/proj", pr.Repository.NameWithOwner)
		assert.Equal(t, 1, resp.TotalCount)
	})

	t.Run(
		"user scoped query maps authored merge requests and leaves repository empty",
		func(t *testing.T) {
			defer SetClient(nil)

			responseBody := `{"data":{"currentUser":{"authoredMergeRequests":{"nodes":[{
			"iid":"7","title":"My MR","state":"opened","draft":true,
			"author":{"username":"jdoe"},
			"createdAt":"2026-02-01T00:00:00Z","updatedAt":"2026-02-02T00:00:00Z",
			"webUrl":"https://gitlab.com/group/proj/-/merge_requests/7",
			"sourceBranch":"feature-y","targetBranch":"main",
			"detailedMergeStatus":"CONFLICT","approved":false,
			"diffStatsSummary":{"additions":3,"deletions":1},
			"labels":{"nodes":[]}
		}],"count":1,"pageInfo":{"hasNextPage":false,"startCursor":"","endCursor":""}}}}}`

			mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, responseBody))
			SetClient(mockClient)

			resp, err := FetchPullRequests("is:open", 30, nil)
			require.NoError(t, err)
			require.Len(t, resp.Prs, 1)

			pr := resp.Prs[0]
			assert.Equal(t, 7, pr.Number)
			assert.Equal(t, "My MR", pr.Title)
			assert.Equal(t, "jdoe", pr.Author.Login)
			assert.True(t, pr.IsDraft)
			assert.Equal(t, "CONFLICTING", pr.Mergeable)
			assert.Equal(t, "REVIEW_REQUIRED", pr.ReviewDecision)
			assert.Equal(t, "", pr.Repository.Owner.Login)
			assert.Equal(t, "", pr.Repository.Name)
			assert.Equal(t, "", pr.Repository.NameWithOwner)
		},
	)

	t.Run(
		"nested subgroup project path splits owner and name on the last slash",
		func(t *testing.T) {
			defer SetClient(nil)

			responseBody := `{"data":{"project":{"mergeRequests":{"nodes":[{
			"iid":"1","title":"Nested","state":"opened","draft":false,
			"author":{"username":"jdoe"},
			"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
			"webUrl":"https://gitlab.com/group/subgroup/proj/-/merge_requests/1",
			"sourceBranch":"x","targetBranch":"main",
			"detailedMergeStatus":"MERGEABLE","approved":true,
			"diffStatsSummary":{"additions":1,"deletions":0},
			"labels":{"nodes":[]}
		}],"count":1,"pageInfo":{"hasNextPage":false,"startCursor":"","endCursor":""}}}}}`

			mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, responseBody))
			SetClient(mockClient)

			resp, err := FetchPullRequests("project:group/subgroup/proj", 30, nil)
			require.NoError(t, err)
			require.Len(t, resp.Prs, 1)

			assert.Equal(t, "group/subgroup", resp.Prs[0].Repository.Owner.Login)
			assert.Equal(t, "proj", resp.Prs[0].Repository.Name)
		},
	)

	t.Run(
		"assignees are mapped from the response when present",
		func(t *testing.T) {
			defer SetClient(nil)

			responseBody := `{"data":{"project":{"mergeRequests":{"nodes":[{
			"iid":"1","title":"Has assignees","state":"opened","draft":false,
			"author":{"username":"jdoe"},
			"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
			"webUrl":"https://gitlab.com/group/proj/-/merge_requests/1",
			"sourceBranch":"x","targetBranch":"main",
			"detailedMergeStatus":"MERGEABLE","approved":true,
			"diffStatsSummary":{"additions":0,"deletions":0},
			"labels":{"nodes":[]},
			"assignees":{"nodes":[{"username":"alice"},{"username":"bob"}]}
		}],"count":1,"pageInfo":{"hasNextPage":false,"startCursor":"","endCursor":""}}}}}`

			mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, responseBody))
			SetClient(mockClient)

			resp, err := FetchPullRequests("project:group/proj", 30, nil)
			require.NoError(t, err)
			require.Len(t, resp.Prs, 1)

			require.Len(t, resp.Prs[0].Assignees.Nodes, 2)
			assert.Equal(t, Assignee{Login: "alice"}, resp.Prs[0].Assignees.Nodes[0])
			assert.Equal(t, Assignee{Login: "bob"}, resp.Prs[0].Assignees.Nodes[1])
		},
	)

	t.Run(
		"no assignees in the response maps to an empty assignees slice",
		func(t *testing.T) {
			defer SetClient(nil)

			responseBody := `{"data":{"project":{"mergeRequests":{"nodes":[{
			"iid":"2","title":"No assignees","state":"opened","draft":false,
			"author":{"username":"jdoe"},
			"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
			"webUrl":"https://gitlab.com/group/proj/-/merge_requests/2",
			"sourceBranch":"x","targetBranch":"main",
			"detailedMergeStatus":"MERGEABLE","approved":true,
			"diffStatsSummary":{"additions":0,"deletions":0},
			"labels":{"nodes":[]},
			"assignees":{"nodes":[]}
		}],"count":1,"pageInfo":{"hasNextPage":false,"startCursor":"","endCursor":""}}}}}`

			mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, responseBody))
			SetClient(mockClient)

			resp, err := FetchPullRequests("project:group/proj", 30, nil)
			require.NoError(t, err)
			require.Len(t, resp.Prs, 1)
			require.Empty(t, resp.Prs[0].Assignees.Nodes)
		},
	)

	t.Run(
		"page info, total count and incoming cursor are passed through correctly",
		func(t *testing.T) {
			defer SetClient(nil)

			responseBody := `{"data":{"project":{"mergeRequests":{"nodes":[],"count":99,"pageInfo":{"hasNextPage":true,"startCursor":"start-abc","endCursor":"end-xyz"}}}}}`

			var capturedBody string
			var mu sync.Mutex
			handler := func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				mu.Lock()
				capturedBody = string(raw)
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(responseBody))
			}
			mockClient := newMockGraphQLClient(t, handler)
			SetClient(mockClient)

			resp, err := FetchPullRequests(
				"project:group/proj",
				30,
				&PageInfo{EndCursor: "incoming-cursor-123"},
			)
			require.NoError(t, err)

			assert.True(t, resp.PageInfo.HasNextPage)
			assert.Equal(t, "start-abc", resp.PageInfo.StartCursor)
			assert.Equal(t, "end-xyz", resp.PageInfo.EndCursor)
			assert.Equal(t, 99, resp.TotalCount)

			mu.Lock()
			defer mu.Unlock()
			assert.Contains(t, capturedBody, "incoming-cursor-123")
		},
	)

	t.Run("propagates error when the server responds with http 500", func(t *testing.T) {
		defer SetClient(nil)

		mockClient := newMockGraphQLClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})
		SetClient(mockClient)

		_, err := FetchPullRequests("is:open", 30, nil)
		require.Error(t, err)
	})

	t.Run("propagates error when the response contains graphql errors", func(t *testing.T) {
		defer SetClient(nil)

		responseBody := `{"data":null,"errors":[{"message":"something went wrong"}]}`
		mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, responseBody))
		SetClient(mockClient)

		_, err := FetchPullRequests("is:open", 30, nil)
		require.Error(t, err)
	})

	t.Run(
		"involves qualifier does not break the search and still returns results",
		func(t *testing.T) {
			defer SetClient(nil)

			responseBody := `{"data":{"currentUser":{"authoredMergeRequests":{"nodes":[{
			"iid":"1","title":"Still works","state":"opened","draft":false,
			"author":{"username":"jdoe"},
			"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
			"webUrl":"https://gitlab.com/group/proj/-/merge_requests/1",
			"sourceBranch":"x","targetBranch":"main",
			"detailedMergeStatus":"MERGEABLE","approved":true,
			"diffStatsSummary":{"additions":1,"deletions":0},
			"labels":{"nodes":[]}
		}],"count":1,"pageInfo":{"hasNextPage":false,"startCursor":"","endCursor":""}}}}}`

			mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, responseBody))
			SetClient(mockClient)

			resp, err := FetchPullRequests("involves:@me", 30, nil)
			require.NoError(t, err)
			require.Len(t, resp.Prs, 1)
			assert.Equal(t, "Still works", resp.Prs[0].Title)
		},
	)

	t.Run(
		"not author qualifier does not break the search and still returns results",
		func(t *testing.T) {
			defer SetClient(nil)

			responseBody := `{"data":{"currentUser":{"authoredMergeRequests":{"nodes":[{
			"iid":"1","title":"Still works without not-author support","state":"opened","draft":false,
			"author":{"username":"jdoe"},
			"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
			"webUrl":"https://gitlab.com/group/proj/-/merge_requests/1",
			"sourceBranch":"x","targetBranch":"main",
			"detailedMergeStatus":"MERGEABLE","approved":true,
			"diffStatsSummary":{"additions":1,"deletions":0},
			"labels":{"nodes":[]}
		}],"count":1,"pageInfo":{"hasNextPage":false,"startCursor":"","endCursor":""}}}}}`

			mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, responseBody))
			SetClient(mockClient)

			resp, err := FetchPullRequests("-author:alice", 30, nil)
			require.NoError(t, err)
			require.Len(t, resp.Prs, 1)
			assert.Equal(t, "Still works without not-author support", resp.Prs[0].Title)
		},
	)
}

func TestFetchPullRequests_MergeableAndMergeStateStatusMapping(t *testing.T) {
	tests := []struct {
		name                 string
		detailedMergeStatus  string
		approved             bool
		wantMergeable        string
		wantReviewDecision   string
		wantMergeStateStatus MergeStateStatus
	}{
		{
			name:                 "conflict maps to conflicting and an empty merge state",
			detailedMergeStatus:  "CONFLICT",
			approved:             false,
			wantMergeable:        "CONFLICTING",
			wantReviewDecision:   "REVIEW_REQUIRED",
			wantMergeStateStatus: "",
		},
		{
			name:                 "mergeable maps to mergeable and a clean merge state",
			detailedMergeStatus:  "MERGEABLE",
			approved:             true,
			wantMergeable:        "MERGEABLE",
			wantReviewDecision:   "APPROVED",
			wantMergeStateStatus: "CLEAN",
		},
		{
			name:                 "ci still running maps to unknown and an unstable merge state",
			detailedMergeStatus:  "CI_STILL_RUNNING",
			approved:             false,
			wantMergeable:        "UNKNOWN",
			wantReviewDecision:   "REVIEW_REQUIRED",
			wantMergeStateStatus: "UNSTABLE",
		},
		{
			name:                 "discussions not resolved maps to unknown and a blocked merge state",
			detailedMergeStatus:  "DISCUSSIONS_NOT_RESOLVED",
			approved:             false,
			wantMergeable:        "UNKNOWN",
			wantReviewDecision:   "REVIEW_REQUIRED",
			wantMergeStateStatus: "BLOCKED",
		},
		{
			name:                 "not approved maps to unknown and a blocked merge state",
			detailedMergeStatus:  "NOT_APPROVED",
			approved:             false,
			wantMergeable:        "UNKNOWN",
			wantReviewDecision:   "REVIEW_REQUIRED",
			wantMergeStateStatus: "BLOCKED",
		},
		{
			name:                 "draft status maps to unknown and a blocked merge state",
			detailedMergeStatus:  "DRAFT_STATUS",
			approved:             false,
			wantMergeable:        "UNKNOWN",
			wantReviewDecision:   "REVIEW_REQUIRED",
			wantMergeStateStatus: "BLOCKED",
		},
		{
			name:                 "an unrecognized status maps to unknown and an empty merge state",
			detailedMergeStatus:  "SOME_FUTURE_STATUS",
			approved:             true,
			wantMergeable:        "UNKNOWN",
			wantReviewDecision:   "APPROVED",
			wantMergeStateStatus: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer SetClient(nil)

			responseBody := fmt.Sprintf(`{"data":{"project":{"mergeRequests":{"nodes":[{
				"iid":"1","title":"T","state":"opened","draft":false,
				"author":{"username":"jdoe"},
				"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
				"webUrl":"https://gitlab.com/group/proj/-/merge_requests/1",
				"sourceBranch":"x","targetBranch":"main",
				"detailedMergeStatus":"%s","approved":%t,
				"diffStatsSummary":{"additions":0,"deletions":0},
				"labels":{"nodes":[]}
			}],"count":1,"pageInfo":{"hasNextPage":false,"startCursor":"","endCursor":""}}}}}`, tt.detailedMergeStatus, tt.approved)

			mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, responseBody))
			SetClient(mockClient)

			resp, err := FetchPullRequests("project:group/proj", 30, nil)
			require.NoError(t, err)
			require.Len(t, resp.Prs, 1)

			assert.Equal(t, tt.wantMergeable, resp.Prs[0].Mergeable)
			assert.Equal(t, tt.wantReviewDecision, resp.Prs[0].ReviewDecision)
			assert.Equal(t, tt.wantMergeStateStatus, resp.Prs[0].MergeStateStatus)
		})
	}
}

func TestFetchPullRequests_TranslatesAuthorMeAndLabelsIntoRequestVariables(t *testing.T) {
	defer SetClient(nil)
	defer gitlab.SetClients(nil, nil)

	currentUserResponseBody := `{"data":{"currentUser":{"username":"jdoe"}}}`

	var capturedVariables map[string]any
	var mu sync.Mutex

	handler := func(w http.ResponseWriter, r *http.Request) {
		body := decodeGraphQLRequestBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if strings.Contains(body.Query, "mergeRequests") {
			mu.Lock()
			capturedVariables = body.Variables
			mu.Unlock()
			_, _ = w.Write([]byte(singleMergeRequestProjectScopedResponse))
			return
		}
		_, _ = w.Write([]byte(currentUserResponseBody))
	}

	server := newMockGraphQLServer(t, handler)
	mockClient := graphql.NewClient(server.URL+"/api/graphql", server.Client())
	SetClient(mockClient)
	gitlab.SetClients(nil, mockClient)

	resp, err := FetchPullRequests("project:group/proj author:@me label:bug", 30, nil)
	require.NoError(t, err)
	require.Len(t, resp.Prs, 1)

	mu.Lock()
	defer mu.Unlock()
	require.NotNil(t, capturedVariables)
	assert.Equal(t, "jdoe", capturedVariables["authorUsername"])
	labels, ok := capturedVariables["labels"].([]any)
	require.True(
		t,
		ok,
		"expected labels variable to be an array, got %T",
		capturedVariables["labels"],
	)
	assert.Contains(t, labels, "bug")
}

func TestFetchPullRequests_TranslatesReviewRequestedMeIntoReviewerUsername(t *testing.T) {
	defer SetClient(nil)
	defer gitlab.SetClients(nil, nil)

	currentUserResponseBody := `{"data":{"currentUser":{"username":"jdoe"}}}`

	var capturedVariables map[string]any
	var mu sync.Mutex

	handler := func(w http.ResponseWriter, r *http.Request) {
		body := decodeGraphQLRequestBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if strings.Contains(body.Query, "mergeRequests") {
			mu.Lock()
			capturedVariables = body.Variables
			mu.Unlock()
			_, _ = w.Write([]byte(singleMergeRequestProjectScopedResponse))
			return
		}
		_, _ = w.Write([]byte(currentUserResponseBody))
	}

	server := newMockGraphQLServer(t, handler)
	mockClient := graphql.NewClient(server.URL+"/api/graphql", server.Client())
	SetClient(mockClient)
	gitlab.SetClients(nil, mockClient)

	resp, err := FetchPullRequests("project:group/proj review-requested:@me", 30, nil)
	require.NoError(t, err)
	require.Len(t, resp.Prs, 1)

	mu.Lock()
	defer mu.Unlock()
	require.NotNil(t, capturedVariables)
	assert.Equal(t, "jdoe", capturedVariables["reviewerUsername"])
}

func TestFetchPullRequests_OmitsAbsentQualifiersAsNullNotEmptyString(t *testing.T) {
	defer SetClient(nil)
	defer gitlab.SetClients(nil, nil)

	currentUserResponseBody := `{"data":{"currentUser":{"username":"jdoe"}}}`

	var capturedVariables map[string]any
	var mu sync.Mutex

	handler := func(w http.ResponseWriter, r *http.Request) {
		body := decodeGraphQLRequestBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if strings.Contains(body.Query, "mergeRequests") {
			mu.Lock()
			capturedVariables = body.Variables
			mu.Unlock()
			_, _ = w.Write([]byte(singleMergeRequestProjectScopedResponse))
			return
		}
		_, _ = w.Write([]byte(currentUserResponseBody))
	}

	server := newMockGraphQLServer(t, handler)
	mockClient := graphql.NewClient(server.URL+"/api/graphql", server.Client())
	SetClient(mockClient)
	gitlab.SetClients(nil, mockClient)

	resp, err := FetchPullRequests("project:group/proj", 30, nil)
	require.NoError(t, err)
	require.Len(t, resp.Prs, 1)

	mu.Lock()
	defer mu.Unlock()
	require.NotNil(t, capturedVariables)
	assert.Nil(
		t,
		capturedVariables["authorUsername"],
		"authorUsername should be absent or null, not an empty string, when the author qualifier is not present in the search",
	)
	assert.Nil(
		t,
		capturedVariables["assigneeUsername"],
		"assigneeUsername should be absent or null, not an empty string, when the assignee qualifier is not present in the search",
	)
	assert.Nil(
		t,
		capturedVariables["reviewerUsername"],
		"reviewerUsername should be absent or null, not an empty string, when the review-requested qualifier is not present in the search",
	)
	assert.Nil(
		t,
		capturedVariables["labels"],
		"labels should be absent or null, not an empty array, when no label qualifier is present in the search, since GitLab treats labels:[] as a filter for zero labels",
	)
	assert.Nil(
		t,
		capturedVariables["sourceBranches"],
		"sourceBranches should be absent or null, not an empty array, when the head qualifier is not present in the search",
	)
}

func TestFetchPullRequests_ProjectScopedDeclaresFullPathAsGraphQLID(t *testing.T) {
	defer SetClient(nil)

	currentUserResponseBody := `{"data":{"currentUser":{"username":"jdoe"}}}`

	var capturedBody graphQLRequestBody
	var mu sync.Mutex

	handler := func(w http.ResponseWriter, r *http.Request) {
		body := decodeGraphQLRequestBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if strings.Contains(body.Query, "mergeRequests(") {
			mu.Lock()
			capturedBody = body
			mu.Unlock()
			_, _ = w.Write([]byte(singleMergeRequestProjectScopedResponse))
			return
		}
		_, _ = w.Write([]byte(currentUserResponseBody))
	}

	mockClient := newMockGraphQLClient(t, handler)
	SetClient(mockClient)

	_, err := FetchPullRequests("project:group/proj", 30, nil)
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

func TestFetchPullRequests_ReviewRequestedMeWithoutProjectUsesReviewRequestedMergeRequests(
	t *testing.T,
) {
	defer SetClient(nil)
	defer gitlab.SetClients(nil, nil)

	currentUserResponseBody := `{"data":{"currentUser":{"username":"jdoe"}}}`
	reviewRequestedResponseBody := `{"data":{"currentUser":{"reviewRequestedMergeRequests":{"nodes":[{
		"iid":"1","title":"Please review","state":"opened","draft":false,
		"author":{"username":"alice"},
		"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
		"webUrl":"https://gitlab.com/group/proj/-/merge_requests/1",
		"sourceBranch":"x","targetBranch":"main",
		"detailedMergeStatus":"MERGEABLE","approved":false,
		"diffStatsSummary":{"additions":1,"deletions":0},
		"labels":{"nodes":[]}
	}],"count":1,"pageInfo":{"hasNextPage":false,"startCursor":"","endCursor":""}}}}}`

	var capturedQuery string
	var capturedVariables map[string]any
	var mu sync.Mutex

	handler := func(w http.ResponseWriter, r *http.Request) {
		body := decodeGraphQLRequestBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if strings.Contains(body.Query, "reviewRequestedMergeRequests") {
			mu.Lock()
			capturedQuery = body.Query
			capturedVariables = body.Variables
			mu.Unlock()
			_, _ = w.Write([]byte(reviewRequestedResponseBody))
			return
		}
		_, _ = w.Write([]byte(currentUserResponseBody))
	}

	server := newMockGraphQLServer(t, handler)
	mockClient := graphql.NewClient(server.URL+"/api/graphql", server.Client())
	SetClient(mockClient)
	gitlab.SetClients(nil, mockClient)

	resp, err := FetchPullRequests("review-requested:@me", 30, nil)
	require.NoError(t, err)
	require.Len(t, resp.Prs, 1)
	assert.Equal(t, "Please review", resp.Prs[0].Title)

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, capturedQuery)
	assert.Contains(t, capturedQuery, "reviewRequestedMergeRequests")
	assert.NotContains(t, capturedQuery, "authoredMergeRequests")
	assert.NotContains(
		t,
		capturedQuery,
		"reviewerUsername:",
		"reviewRequestedMergeRequests is already implicitly scoped to the current user, so GitLab rejects a redundant reviewerUsername argument",
	)
	require.NotNil(t, capturedVariables)
	assert.Nil(
		t,
		capturedVariables["labels"],
		"labels should be absent or null, not an empty array, when no label qualifier is present",
	)
}

func TestFetchPullRequests_AssigneeMeWithoutProjectOrAuthorUsesAssignedMergeRequests(t *testing.T) {
	defer SetClient(nil)
	defer gitlab.SetClients(nil, nil)

	currentUserResponseBody := `{"data":{"currentUser":{"username":"jdoe"}}}`
	assignedResponseBody := `{"data":{"currentUser":{"assignedMergeRequests":{"nodes":[{
		"iid":"2","title":"Assigned to me","state":"opened","draft":false,
		"author":{"username":"alice"},
		"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
		"webUrl":"https://gitlab.com/group/proj/-/merge_requests/2",
		"sourceBranch":"x","targetBranch":"main",
		"detailedMergeStatus":"MERGEABLE","approved":false,
		"diffStatsSummary":{"additions":1,"deletions":0},
		"labels":{"nodes":[]}
	}],"count":1,"pageInfo":{"hasNextPage":false,"startCursor":"","endCursor":""}}}}}`

	var capturedQuery string
	var capturedVariables map[string]any
	var mu sync.Mutex

	handler := func(w http.ResponseWriter, r *http.Request) {
		body := decodeGraphQLRequestBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if strings.Contains(body.Query, "assignedMergeRequests") {
			mu.Lock()
			capturedQuery = body.Query
			capturedVariables = body.Variables
			mu.Unlock()
			_, _ = w.Write([]byte(assignedResponseBody))
			return
		}
		_, _ = w.Write([]byte(currentUserResponseBody))
	}

	server := newMockGraphQLServer(t, handler)
	mockClient := graphql.NewClient(server.URL+"/api/graphql", server.Client())
	SetClient(mockClient)
	gitlab.SetClients(nil, mockClient)

	resp, err := FetchPullRequests("assignee:@me", 30, nil)
	require.NoError(t, err)
	require.Len(t, resp.Prs, 1)
	assert.Equal(t, "Assigned to me", resp.Prs[0].Title)

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, capturedQuery)
	assert.Contains(t, capturedQuery, "assignedMergeRequests")
	assert.NotContains(t, capturedQuery, "authoredMergeRequests")
	assert.NotContains(
		t,
		capturedQuery,
		"assigneeUsername:",
		"assignedMergeRequests is already implicitly scoped to the current user, so GitLab rejects a redundant assigneeUsername argument",
	)
	require.NotNil(t, capturedVariables)
	assert.Nil(
		t,
		capturedVariables["labels"],
		"labels should be absent or null, not an empty array, when no label qualifier is present",
	)
}

func TestFetchPullRequests_EmptySearchWithoutProjectDefaultsAuthorUsernameToCurrentUser(
	t *testing.T,
) {
	defer SetClient(nil)
	defer gitlab.SetClients(nil, nil)

	currentUserResponseBody := `{"data":{"currentUser":{"username":"jdoe"}}}`
	authoredResponseBody := `{"data":{"currentUser":{"authoredMergeRequests":{"nodes":[{
		"iid":"7","title":"My MR","state":"opened","draft":false,
		"author":{"username":"jdoe"},
		"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
		"webUrl":"https://gitlab.com/group/proj/-/merge_requests/7",
		"sourceBranch":"x","targetBranch":"main",
		"detailedMergeStatus":"MERGEABLE","approved":true,
		"diffStatsSummary":{"additions":1,"deletions":0},
		"labels":{"nodes":[]}
	}],"count":1,"pageInfo":{"hasNextPage":false,"startCursor":"","endCursor":""}}}}}`

	var capturedQuery string
	var capturedVariables map[string]any
	var mu sync.Mutex

	handler := func(w http.ResponseWriter, r *http.Request) {
		body := decodeGraphQLRequestBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if strings.Contains(body.Query, "authoredMergeRequests") {
			mu.Lock()
			capturedQuery = body.Query
			capturedVariables = body.Variables
			mu.Unlock()
			_, _ = w.Write([]byte(authoredResponseBody))
			return
		}
		_, _ = w.Write([]byte(currentUserResponseBody))
	}

	server := newMockGraphQLServer(t, handler)
	mockClient := graphql.NewClient(server.URL+"/api/graphql", server.Client())
	SetClient(mockClient)
	gitlab.SetClients(nil, mockClient)

	resp, err := FetchPullRequests("is:open", 30, nil)
	require.NoError(t, err)
	require.Len(t, resp.Prs, 1)

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, capturedQuery)
	assert.Contains(t, capturedQuery, "authoredMergeRequests")
	assert.NotContains(
		t,
		capturedQuery,
		"authorUsername:",
		"authoredMergeRequests is already implicitly scoped to the current user, so GitLab rejects a redundant authorUsername argument",
	)
	assert.Contains(
		t,
		capturedQuery,
		"assigneeUsername:",
		"assigneeUsername should still be declared on authoredMergeRequests since GitLab accepts it as a cross filter",
	)
	require.NotNil(t, capturedVariables)
}

func TestFetchPullRequests_EmptySearchStringWithoutProjectUsesAuthoredMergeRequests(t *testing.T) {
	defer SetClient(nil)
	defer gitlab.SetClients(nil, nil)

	currentUserResponseBody := `{"data":{"currentUser":{"username":"jdoe"}}}`
	authoredResponseBody := `{"data":{"currentUser":{"authoredMergeRequests":{"nodes":[{
		"iid":"9","title":"Empty search MR","state":"opened","draft":false,
		"author":{"username":"jdoe"},
		"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
		"webUrl":"https://gitlab.com/group/proj/-/merge_requests/9",
		"sourceBranch":"x","targetBranch":"main",
		"detailedMergeStatus":"MERGEABLE","approved":true,
		"diffStatsSummary":{"additions":1,"deletions":0},
		"labels":{"nodes":[]}
	}],"count":1,"pageInfo":{"hasNextPage":false,"startCursor":"","endCursor":""}}}}}`

	var capturedQuery string
	var capturedVariables map[string]any
	var mu sync.Mutex

	handler := func(w http.ResponseWriter, r *http.Request) {
		body := decodeGraphQLRequestBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if strings.Contains(body.Query, "authoredMergeRequests") {
			mu.Lock()
			capturedQuery = body.Query
			capturedVariables = body.Variables
			mu.Unlock()
			_, _ = w.Write([]byte(authoredResponseBody))
			return
		}
		_, _ = w.Write([]byte(currentUserResponseBody))
	}

	server := newMockGraphQLServer(t, handler)
	mockClient := graphql.NewClient(server.URL+"/api/graphql", server.Client())
	SetClient(mockClient)
	gitlab.SetClients(nil, mockClient)

	resp, err := FetchPullRequests("", 30, nil)
	require.NoError(t, err)
	require.Len(t, resp.Prs, 1)
	assert.Equal(t, "Empty search MR", resp.Prs[0].Title)

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, capturedQuery)
	assert.Contains(t, capturedQuery, "authoredMergeRequests")
	assert.NotContains(
		t,
		capturedQuery,
		"authorUsername:",
		"authoredMergeRequests is already implicitly scoped to the current user, so GitLab rejects a redundant authorUsername argument",
	)
	assert.Contains(
		t,
		capturedQuery,
		"assigneeUsername:",
		"assigneeUsername should still be declared on authoredMergeRequests since GitLab accepts it as a cross filter",
	)
	require.NotNil(t, capturedVariables)
	assert.Nil(
		t,
		capturedVariables["labels"],
		"labels should be absent or null, not an empty array, when no label qualifier is present in the search, since GitLab treats labels:[] as a filter for zero labels",
	)
}

func TestFetchPullRequests_AuthorDifferentFromCurrentUserWithoutProjectFallsBackAndWarns(
	t *testing.T,
) {
	defer SetClient(nil)
	defer gitlab.SetClients(nil, nil)

	logBuf := captureLogOutput(t)

	currentUserResponseBody := `{"data":{"currentUser":{"username":"jdoe"}}}`
	authoredResponseBody := `{"data":{"currentUser":{"authoredMergeRequests":{"nodes":[{
		"iid":"11","title":"Fallback for mismatched author","state":"opened","draft":false,
		"author":{"username":"jdoe"},
		"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
		"webUrl":"https://gitlab.com/group/proj/-/merge_requests/11",
		"sourceBranch":"x","targetBranch":"main",
		"detailedMergeStatus":"MERGEABLE","approved":true,
		"diffStatsSummary":{"additions":1,"deletions":0},
		"labels":{"nodes":[]}
	}],"count":1,"pageInfo":{"hasNextPage":false,"startCursor":"","endCursor":""}}}}}`

	var capturedQuery string
	var mu sync.Mutex

	handler := func(w http.ResponseWriter, r *http.Request) {
		body := decodeGraphQLRequestBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if strings.Contains(body.Query, "authoredMergeRequests") {
			mu.Lock()
			capturedQuery = body.Query
			mu.Unlock()
			_, _ = w.Write([]byte(authoredResponseBody))
			return
		}
		_, _ = w.Write([]byte(currentUserResponseBody))
	}

	server := newMockGraphQLServer(t, handler)
	mockClient := graphql.NewClient(server.URL+"/api/graphql", server.Client())
	SetClient(mockClient)
	gitlab.SetClients(nil, mockClient)

	resp, err := FetchPullRequests("author:outrapessoa", 30, nil)
	require.NoError(t, err)
	require.Len(t, resp.Prs, 1)
	assert.Equal(t, "Fallback for mismatched author", resp.Prs[0].Title)

	mu.Lock()
	require.NotEmpty(t, capturedQuery)
	assert.Contains(t, capturedQuery, "authoredMergeRequests")
	mu.Unlock()

	assert.Contains(
		t,
		logBuf.String(),
		"WARN",
		"expected a warning to be logged when author: targets a user other than the current one without project:",
	)
}

func TestFetchPullRequests_AssigneeDifferentFromCurrentUserWithoutProjectFallsBackAndWarns(
	t *testing.T,
) {
	defer SetClient(nil)
	defer gitlab.SetClients(nil, nil)

	logBuf := captureLogOutput(t)

	currentUserResponseBody := `{"data":{"currentUser":{"username":"jdoe"}}}`
	authoredResponseBody := `{"data":{"currentUser":{"authoredMergeRequests":{"nodes":[{
		"iid":"12","title":"Fallback for mismatched assignee","state":"opened","draft":false,
		"author":{"username":"jdoe"},
		"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
		"webUrl":"https://gitlab.com/group/proj/-/merge_requests/12",
		"sourceBranch":"x","targetBranch":"main",
		"detailedMergeStatus":"MERGEABLE","approved":true,
		"diffStatsSummary":{"additions":1,"deletions":0},
		"labels":{"nodes":[]}
	}],"count":1,"pageInfo":{"hasNextPage":false,"startCursor":"","endCursor":""}}}}}`

	var capturedQuery string
	var mu sync.Mutex

	handler := func(w http.ResponseWriter, r *http.Request) {
		body := decodeGraphQLRequestBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if strings.Contains(body.Query, "authoredMergeRequests") {
			mu.Lock()
			capturedQuery = body.Query
			mu.Unlock()
			_, _ = w.Write([]byte(authoredResponseBody))
			return
		}
		_, _ = w.Write([]byte(currentUserResponseBody))
	}

	server := newMockGraphQLServer(t, handler)
	mockClient := graphql.NewClient(server.URL+"/api/graphql", server.Client())
	SetClient(mockClient)
	gitlab.SetClients(nil, mockClient)

	resp, err := FetchPullRequests("assignee:outrapessoa", 30, nil)
	require.NoError(t, err)
	require.Len(t, resp.Prs, 1)
	assert.Equal(t, "Fallback for mismatched assignee", resp.Prs[0].Title)

	mu.Lock()
	require.NotEmpty(t, capturedQuery)
	assert.Contains(t, capturedQuery, "authoredMergeRequests")
	mu.Unlock()

	assert.Contains(
		t,
		logBuf.String(),
		"WARN",
		"expected a warning to be logged when assignee: targets a user other than the current one without project:",
	)
}

func TestFetchPullRequests_ReviewRequestedDifferentFromCurrentUserWithoutProjectFallsBackAndWarns(
	t *testing.T,
) {
	defer SetClient(nil)
	defer gitlab.SetClients(nil, nil)

	logBuf := captureLogOutput(t)

	currentUserResponseBody := `{"data":{"currentUser":{"username":"jdoe"}}}`
	authoredResponseBody := `{"data":{"currentUser":{"authoredMergeRequests":{"nodes":[{
		"iid":"13","title":"Fallback for mismatched reviewer","state":"opened","draft":false,
		"author":{"username":"jdoe"},
		"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
		"webUrl":"https://gitlab.com/group/proj/-/merge_requests/13",
		"sourceBranch":"x","targetBranch":"main",
		"detailedMergeStatus":"MERGEABLE","approved":true,
		"diffStatsSummary":{"additions":1,"deletions":0},
		"labels":{"nodes":[]}
	}],"count":1,"pageInfo":{"hasNextPage":false,"startCursor":"","endCursor":""}}}}}`

	var capturedQuery string
	var mu sync.Mutex

	handler := func(w http.ResponseWriter, r *http.Request) {
		body := decodeGraphQLRequestBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if strings.Contains(body.Query, "authoredMergeRequests") {
			mu.Lock()
			capturedQuery = body.Query
			mu.Unlock()
			_, _ = w.Write([]byte(authoredResponseBody))
			return
		}
		_, _ = w.Write([]byte(currentUserResponseBody))
	}

	server := newMockGraphQLServer(t, handler)
	mockClient := graphql.NewClient(server.URL+"/api/graphql", server.Client())
	SetClient(mockClient)
	gitlab.SetClients(nil, mockClient)

	resp, err := FetchPullRequests("review-requested:outrapessoa", 30, nil)
	require.NoError(t, err)
	require.Len(t, resp.Prs, 1)
	assert.Equal(t, "Fallback for mismatched reviewer", resp.Prs[0].Title)

	mu.Lock()
	require.NotEmpty(t, capturedQuery)
	assert.Contains(t, capturedQuery, "authoredMergeRequests")
	mu.Unlock()

	assert.Contains(
		t,
		logBuf.String(),
		"WARN",
		"expected a warning to be logged when review-requested: targets a user other than the current one without project:",
	)
}

func TestFetchPullRequests_AuthorExplicitlyEqualsCurrentUserWithoutProjectBehavesLikeAuthorMe(
	t *testing.T,
) {
	defer SetClient(nil)
	defer gitlab.SetClients(nil, nil)

	logBuf := captureLogOutput(t)

	currentUserResponseBody := `{"data":{"currentUser":{"username":"jdoe"}}}`
	authoredResponseBody := `{"data":{"currentUser":{"authoredMergeRequests":{"nodes":[{
		"iid":"14","title":"Explicit self author","state":"opened","draft":false,
		"author":{"username":"jdoe"},
		"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
		"webUrl":"https://gitlab.com/group/proj/-/merge_requests/14",
		"sourceBranch":"x","targetBranch":"main",
		"detailedMergeStatus":"MERGEABLE","approved":true,
		"diffStatsSummary":{"additions":1,"deletions":0},
		"labels":{"nodes":[]}
	}],"count":1,"pageInfo":{"hasNextPage":false,"startCursor":"","endCursor":""}}}}}`

	var capturedQuery string
	var mu sync.Mutex

	handler := func(w http.ResponseWriter, r *http.Request) {
		body := decodeGraphQLRequestBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if strings.Contains(body.Query, "authoredMergeRequests") {
			mu.Lock()
			capturedQuery = body.Query
			mu.Unlock()
			_, _ = w.Write([]byte(authoredResponseBody))
			return
		}
		_, _ = w.Write([]byte(currentUserResponseBody))
	}

	server := newMockGraphQLServer(t, handler)
	mockClient := graphql.NewClient(server.URL+"/api/graphql", server.Client())
	SetClient(mockClient)
	gitlab.SetClients(nil, mockClient)

	resp, err := FetchPullRequests("author:jdoe", 30, nil)
	require.NoError(t, err)
	require.Len(t, resp.Prs, 1)
	assert.Equal(t, "Explicit self author", resp.Prs[0].Title)

	mu.Lock()
	require.NotEmpty(t, capturedQuery)
	assert.Contains(t, capturedQuery, "authoredMergeRequests")
	assert.NotContains(t, capturedQuery, "authorUsername:")
	mu.Unlock()

	assert.NotContains(
		t,
		logBuf.String(),
		"WARN",
		"author:jdoe should behave exactly like author:@me when the current user is jdoe, without any fallback warning",
	)
}

func TestFetchPullRequests_ProjectScopedClosedStateSendsClosedStateVariableWithMergeRequestStateType(
	t *testing.T,
) {
	defer SetClient(nil)

	currentUserResponseBody := `{"data":{"currentUser":{"username":"jdoe"}}}`
	mergeRequestsResponseBody := `{"data":{"project":{"mergeRequests":{"nodes":[],"count":0,"pageInfo":{"hasNextPage":false,"startCursor":"","endCursor":""}}}}}`

	var capturedBody graphQLRequestBody
	var mu sync.Mutex

	handler := func(w http.ResponseWriter, r *http.Request) {
		body := decodeGraphQLRequestBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if strings.Contains(body.Query, "mergeRequests(") {
			mu.Lock()
			capturedBody = body
			mu.Unlock()
			_, _ = w.Write([]byte(mergeRequestsResponseBody))
			return
		}
		_, _ = w.Write([]byte(currentUserResponseBody))
	}

	mockClient := newMockGraphQLClient(t, handler)
	SetClient(mockClient)

	_, err := FetchPullRequests("project:group/proj is:closed", 30, nil)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "closed", capturedBody.Variables["state"])
	assert.Contains(
		t,
		capturedBody.Query,
		"$state:MergeRequestState",
		"expected the state variable to be declared as MergeRequestState so GitLab accepts it, got query: %s",
		capturedBody.Query,
	)
}

func TestParseMergeRequestUrl(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		wantPath    string
		wantIid     string
		expectError bool
	}{
		{
			name:     "valid url with a nested subgroup",
			url:      "https://gitlab.com/group/subgroup/proj/-/merge_requests/42",
			wantPath: "group/subgroup/proj",
			wantIid:  "42",
		},
		{
			name:     "valid url with a single level namespace",
			url:      "https://gitlab.com/group/proj/-/merge_requests/7",
			wantPath: "group/proj",
			wantIid:  "7",
		},
		{
			name:        "url without a merge requests segment returns an error",
			url:         "https://gitlab.com/group/proj",
			expectError: true,
		},
		{
			name:        "malformed url returns an error",
			url:         "https://gitlab.com/group/proj/-/merge_requests/42\x00",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fullPath, iid, err := parseMergeRequestUrl(tt.url)
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

func TestFetchPullRequest(t *testing.T) {
	t.Run("maps a single merge request to EnrichedPullRequestData", func(t *testing.T) {
		defer SetClient(nil)

		responseBody := `{"data":{"project":{"mergeRequest":{
			"iid":"42","title":"Fix bug","state":"opened","draft":false,
			"author":{"username":"jdoe"},
			"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
			"webUrl":"https://gitlab.com/group/proj/-/merge_requests/42",
			"sourceBranch":"feature-x","targetBranch":"main",
			"detailedMergeStatus":"MERGEABLE","approved":true,
			"diffStatsSummary":{"additions":10,"deletions":2},
			"labels":{"nodes":[{"title":"bug","color":"#ff0000","description":"Bug label"}]}
		}}}}`

		mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, responseBody))
		SetClient(mockClient)

		pr, err := FetchPullRequest("https://gitlab.com/group/proj/-/merge_requests/42")
		require.NoError(t, err)

		wantCreatedAt, timeErr := time.Parse(time.RFC3339, "2026-01-01T00:00:00Z")
		require.NoError(t, timeErr)
		wantUpdatedAt, timeErr := time.Parse(time.RFC3339, "2026-01-02T00:00:00Z")
		require.NoError(t, timeErr)

		assert.Equal(t, 42, pr.Number)
		assert.Equal(t, "Fix bug", pr.Title)
		assert.Equal(t, "opened", pr.State)
		assert.False(t, pr.IsDraft)
		assert.Equal(t, "jdoe", pr.Author.Login)
		assert.Equal(t, wantCreatedAt, pr.CreatedAt)
		assert.Equal(t, wantUpdatedAt, pr.UpdatedAt)
		assert.Equal(t, "MERGEABLE", pr.Mergeable)
		assert.Equal(t, "APPROVED", pr.ReviewDecision)
		assert.Equal(t, 10, pr.Additions)
		assert.Equal(t, 2, pr.Deletions)
		assert.Equal(t, "feature-x", pr.HeadRefName)
		assert.Equal(t, "main", pr.BaseRefName)
		assert.Equal(t, "https://gitlab.com/group/proj/-/merge_requests/42", pr.Url)
		require.Len(t, pr.Labels.Nodes, 1)
		assert.Equal(
			t,
			Label{Name: "bug", Color: "#ff0000", Description: "Bug label"},
			pr.Labels.Nodes[0],
		)
		assert.Equal(t, "group", pr.Repository.Owner.Login)
		assert.Equal(t, "proj", pr.Repository.Name)
	})

	t.Run("populates body from description and assignees from the response", func(t *testing.T) {
		defer SetClient(nil)

		responseBody := `{"data":{"project":{"mergeRequest":{
			"iid":"42","title":"Fix bug","state":"opened","draft":false,
			"description":"Detailed description of the fix",
			"author":{"username":"jdoe"},
			"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
			"webUrl":"https://gitlab.com/group/proj/-/merge_requests/42",
			"sourceBranch":"feature-x","targetBranch":"main",
			"detailedMergeStatus":"MERGEABLE","approved":true,
			"diffStatsSummary":{"additions":10,"deletions":2},
			"labels":{"nodes":[]},
			"assignees":{"nodes":[{"username":"alice"},{"username":"bob"}]}
		}}}}`

		mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, responseBody))
		SetClient(mockClient)

		pr, err := FetchPullRequest("https://gitlab.com/group/proj/-/merge_requests/42")
		require.NoError(t, err)

		assert.Equal(t, "Detailed description of the fix", pr.Body)
		require.Len(t, pr.Assignees.Nodes, 2)
		assert.Equal(t, Assignee{Login: "alice"}, pr.Assignees.Nodes[0])
		assert.Equal(t, Assignee{Login: "bob"}, pr.Assignees.Nodes[1])
	})

	t.Run("propagates error when the server responds with http 500", func(t *testing.T) {
		defer SetClient(nil)

		mockClient := newMockGraphQLClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})
		SetClient(mockClient)

		_, err := FetchPullRequest("https://gitlab.com/group/proj/-/merge_requests/42")
		require.Error(t, err)
	})

	t.Run(
		"propagates error when the url has no merge request segment to parse",
		func(t *testing.T) {
			defer SetClient(nil)

			mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, `{}`))
			SetClient(mockClient)

			_, err := FetchPullRequest("https://gitlab.com/group/proj")
			require.Error(t, err)
		},
	)
}

func TestFetchPullRequest_DeclaresFullPathAsGraphQLID(t *testing.T) {
	defer SetClient(nil)

	responseBody := `{"data":{"project":{"mergeRequest":{
		"iid":"42","title":"Fix bug","state":"opened","draft":false,
		"author":{"username":"jdoe"},
		"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
		"webUrl":"https://gitlab.com/group/proj/-/merge_requests/42",
		"sourceBranch":"feature-x","targetBranch":"main",
		"detailedMergeStatus":"MERGEABLE","approved":true,
		"diffStatsSummary":{"additions":10,"deletions":2},
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

	_, err := FetchPullRequest("https://gitlab.com/group/proj/-/merge_requests/42")
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
