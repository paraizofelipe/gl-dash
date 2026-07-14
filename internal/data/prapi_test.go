package data

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	graphql "github.com/cli/shurcooL-graphql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gitlabapi "gitlab.com/gitlab-org/api/client-go"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
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

func withFeatureFlagDisabled(t *testing.T, name string) {
	t.Helper()
	original, wasSet := os.LookupEnv(name)
	require.NoError(t, os.Unsetenv(name))
	t.Cleanup(func() {
		if wasSet {
			require.NoError(t, os.Setenv(name, original))
		}
	})
}

func TestResolveGraphQLClient_ConcurrentAccess(t *testing.T) {
	defer SetClient(nil)
	defer gitlab.SetClients(nil, nil)
	withFeatureFlagDisabled(t, config.FF_MOCK_DATA)
	SetClient(nil)

	mockGQL := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, `{"data":{}}`))
	gitlab.SetClients(nil, mockGQL)

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
			results[i], errs[i] = resolveGraphQLClient()
		}(i)
	}
	close(start)
	wg.Wait()

	for i := range n {
		require.NoError(t, errs[i])
		require.NotNil(t, results[i])
		require.Same(t, mockGQL, results[i])
	}
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
		assert.Equal(t, "OPEN", pr.State)
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

	t.Run("maps multiple labels with names and colors", func(t *testing.T) {
		defer SetClient(nil)

		responseBody := `{"data":{"project":{"mergeRequests":{"nodes":[{
			"iid":"3","title":"Multi label","state":"opened","draft":false,
			"author":{"username":"jdoe"},
			"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
			"webUrl":"https://gitlab.com/group/proj/-/merge_requests/3",
			"sourceBranch":"feature-z","targetBranch":"main",
			"detailedMergeStatus":"MERGEABLE","approved":true,
			"diffStatsSummary":{"additions":5,"deletions":1},
			"labels":{"nodes":[
				{"title":"bug","color":"#ff0000","description":"Bug label"},
				{"title":"enhancement","color":"#00ff00","description":"Enhancement label"},
				{"title":"documentation","color":"#0000ff","description":""}
			]}
		}],"count":1,"pageInfo":{"hasNextPage":false,"startCursor":"","endCursor":""}}}}}`

		mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, responseBody))
		SetClient(mockClient)

		resp, err := FetchPullRequests("project:group/proj", 30, nil)
		require.NoError(t, err)
		require.Len(t, resp.Prs, 1)

		require.Len(t, resp.Prs[0].Labels.Nodes, 3)
		assert.Equal(
			t,
			Label{Name: "bug", Color: "#ff0000", Description: "Bug label"},
			resp.Prs[0].Labels.Nodes[0],
		)
		assert.Equal(
			t,
			Label{Name: "enhancement", Color: "#00ff00", Description: "Enhancement label"},
			resp.Prs[0].Labels.Nodes[1],
		)
		assert.Equal(
			t,
			Label{Name: "documentation", Color: "#0000ff", Description: ""},
			resp.Prs[0].Labels.Nodes[2],
		)
	})
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
		defer SetRESTClient(nil)

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
		SetRESTClient(newMockRESTClient(t, staticJSONHandler(http.StatusNotFound, "")))

		pr, err := FetchPullRequest("https://gitlab.com/group/proj/-/merge_requests/42")
		require.NoError(t, err)

		wantCreatedAt, timeErr := time.Parse(time.RFC3339, "2026-01-01T00:00:00Z")
		require.NoError(t, timeErr)
		wantUpdatedAt, timeErr := time.Parse(time.RFC3339, "2026-01-02T00:00:00Z")
		require.NoError(t, timeErr)

		assert.Equal(t, 42, pr.Number)
		assert.Equal(t, "Fix bug", pr.Title)
		assert.Equal(t, "OPEN", pr.State)
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
		defer SetRESTClient(nil)

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
		SetRESTClient(newMockRESTClient(t, staticJSONHandler(http.StatusNotFound, "")))

		pr, err := FetchPullRequest("https://gitlab.com/group/proj/-/merge_requests/42")
		require.NoError(t, err)

		assert.Equal(t, "Detailed description of the fix", pr.Body)
		require.Len(t, pr.Assignees.Nodes, 2)
		assert.Equal(t, Assignee{Login: "alice"}, pr.Assignees.Nodes[0])
		assert.Equal(t, Assignee{Login: "bob"}, pr.Assignees.Nodes[1])
	})

	t.Run("populates files changed from diffStats", func(t *testing.T) {
		defer SetClient(nil)
		defer SetRESTClient(nil)

		responseBody := `{"data":{"project":{"mergeRequest":{
			"iid":"42","title":"Fix bug","state":"opened","draft":false,
			"author":{"username":"jdoe"},
			"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
			"webUrl":"https://gitlab.com/group/proj/-/merge_requests/42",
			"sourceBranch":"feature-x","targetBranch":"main",
			"detailedMergeStatus":"MERGEABLE","approved":false,
			"diffStatsSummary":{"additions":10,"deletions":9},
			"labels":{"nodes":[]},
			"diffStats":[
				{"path":"main.go","additions":10,"deletions":2},
				{"path":"internal/data/prapi.go","additions":0,"deletions":7}
			],
			"commits":{"nodes":[
				{"sha":"abc123","title":"Fix the bug","webUrl":"https://gitlab.com/group/proj/-/commit/abc123",
				 "authoredDate":"2026-01-01T08:30:00Z","author":{"username":"jdoe"},"authorName":"John Doe"},
				{"sha":"def456","title":"External commit","webUrl":"https://gitlab.com/group/proj/-/commit/def456",
				 "authoredDate":"2026-01-01T09:00:00Z","authorName":"Jane External"}
			]}
		}}}}`

		mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, responseBody))
		SetClient(mockClient)
		SetRESTClient(newMockRESTClient(t, staticJSONHandler(http.StatusNotFound, "")))

		pr, err := FetchPullRequest("https://gitlab.com/group/proj/-/merge_requests/42")
		require.NoError(t, err)

		require.Len(t, pr.Files.Nodes, 2)
		assert.Equal(t, 2, pr.Files.TotalCount)
		assert.Equal(t, "main.go", pr.Files.Nodes[0].Path)
		assert.Equal(t, 10, pr.Files.Nodes[0].Additions)
		assert.Equal(t, 2, pr.Files.Nodes[0].Deletions)
		assert.Equal(t, "internal/data/prapi.go", pr.Files.Nodes[1].Path)
		assert.Equal(t, 7, pr.Files.Nodes[1].Deletions)

		require.Len(t, pr.Commits, 2)
		assert.Equal(t, "abc123", pr.Commits[0].Sha)
		assert.Equal(t, "Fix the bug", pr.Commits[0].Title)
		assert.Equal(t, "jdoe", pr.Commits[0].Author)
		assert.Equal(t, "Jane External", pr.Commits[1].Author)
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
	defer SetRESTClient(nil)

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
	SetRESTClient(newMockRESTClient(t, staticJSONHandler(http.StatusNotFound, "")))

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

func isolateGitLabAuthEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GITLAB_TOKEN", "")
	t.Setenv("GITLAB_HOST", "")
	t.Setenv("CI_JOB_TOKEN", "")
}

func TestCountJobsByState(t *testing.T) {
	t.Run("aggregates mixed job states preserving first occurrence order", func(t *testing.T) {
		jobs := []PipelineJob{
			{ID: 1, Status: StatusRunning},
			{ID: 2, Status: StatusSuccess},
			{ID: 3, Status: StatusRunning},
			{ID: 4, Status: StatusFailed},
			{ID: 5, Status: StatusRunning},
			{ID: 6, Status: StatusSuccess},
		}

		got := CountJobsByState(jobs)

		want := []JobCountByState{
			{State: StatusRunning, Count: 3},
			{State: StatusSuccess, Count: 2},
			{State: StatusFailed, Count: 1},
		}
		assert.Equal(t, want, got)
	})

	t.Run("nil slice returns an empty non-nil slice without panic", func(t *testing.T) {
		var got []JobCountByState
		require.NotPanics(t, func() {
			got = CountJobsByState(nil)
		})
		assert.Equal(t, []JobCountByState{}, got)
	})

	t.Run("empty slice returns an empty non-nil slice without panic", func(t *testing.T) {
		var got []JobCountByState
		require.NotPanics(t, func() {
			got = CountJobsByState([]PipelineJob{})
		})
		assert.Equal(t, []JobCountByState{}, got)
	})

	t.Run(
		"all jobs in the same state collapse into a single entry with the total count",
		func(t *testing.T) {
			jobs := []PipelineJob{
				{ID: 1, Status: StatusManual},
				{ID: 2, Status: StatusManual},
				{ID: 3, Status: StatusManual},
			}

			got := CountJobsByState(jobs)

			require.Len(t, got, 1)
			assert.Equal(t, JobCountByState{State: StatusManual, Count: 3}, got[0])
		},
	)
}

func TestFetchPullRequests_HeadPipelineStatusPopulatesCommitsStatusCheckRollupLowercased(
	t *testing.T,
) {
	tests := []struct {
		name              string
		headPipelineField string
		wantNodes         int
		wantState         graphql.String
	}{
		{
			name:              "uppercase SUCCESS head pipeline status is lowered to success",
			headPipelineField: `{"status":"SUCCESS"}`,
			wantNodes:         1,
			wantState:         "success",
		},
		{
			name:              "uppercase RUNNING head pipeline status is lowered to running",
			headPipelineField: `{"status":"RUNNING"}`,
			wantNodes:         1,
			wantState:         "running",
		},
		{
			name:              "null head pipeline leaves commits nodes empty",
			headPipelineField: `null`,
			wantNodes:         0,
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
				"detailedMergeStatus":"MERGEABLE","approved":true,
				"diffStatsSummary":{"additions":0,"deletions":0},
				"labels":{"nodes":[]},
				"headPipeline":%s
			}],"count":1,"pageInfo":{"hasNextPage":false,"startCursor":"","endCursor":""}}}}}`, tt.headPipelineField)

			mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, responseBody))
			SetClient(mockClient)

			var resp PullRequestsResponse
			var err error
			require.NotPanics(t, func() {
				resp, err = FetchPullRequests("project:group/proj", 30, nil)
			})
			require.NoError(t, err)
			require.Len(t, resp.Prs, 1)

			require.Len(t, resp.Prs[0].Commits.Nodes, tt.wantNodes)
			if tt.wantNodes > 0 {
				assert.Equal(
					t,
					tt.wantState,
					resp.Prs[0].Commits.Nodes[0].Commit.StatusCheckRollup.State,
				)
			}
		})
	}
}

func TestFetchPullRequests_QueriesHeadPipelineStatusOnMergeRequestNode(t *testing.T) {
	defer SetClient(nil)

	var capturedBody graphQLRequestBody
	var mu sync.Mutex

	handler := func(w http.ResponseWriter, r *http.Request) {
		body := decodeGraphQLRequestBody(t, r)
		mu.Lock()
		capturedBody = body
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(singleMergeRequestProjectScopedResponse))
	}

	mockClient := newMockGraphQLClient(t, handler)
	SetClient(mockClient)

	_, err := FetchPullRequests("project:group/proj", 30, nil)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, capturedBody.Query)
	assert.Contains(t, capturedBody.Query, "headPipeline{status}")
}

func TestSetRESTClient(t *testing.T) {
	originalRESTClient := gitlabRESTClient
	defer func() { gitlabRESTClient = originalRESTClient }()

	t.Run("sets the package level rest client and nil clears it", func(t *testing.T) {
		mockClient, err := gitlabapi.NewClient(
			"token",
			gitlabapi.WithBaseURL("http://example.invalid"),
		)
		require.NoError(t, err)

		SetRESTClient(mockClient)
		require.Same(t, mockClient, gitlabRESTClient)

		SetRESTClient(nil)
		require.Nil(t, gitlabRESTClient)
	})
}

func TestResolveRESTClient(t *testing.T) {
	t.Run(
		"returns the client already injected via SetRESTClient without falling back",
		func(t *testing.T) {
			defer SetRESTClient(nil)

			mockClient, err := gitlabapi.NewClient(
				"token",
				gitlabapi.WithBaseURL("http://example.invalid"),
			)
			require.NoError(t, err)
			SetRESTClient(mockClient)

			got, err := resolveRESTClient()
			require.NoError(t, err)
			require.Same(t, mockClient, got)
		},
	)

	t.Run(
		"falls back to internal/gitlab RESTClient and caches the resolved pointer",
		func(t *testing.T) {
			defer SetRESTClient(nil)
			defer gitlab.SetClients(nil, nil)

			mockClient, err := gitlabapi.NewClient(
				"token",
				gitlabapi.WithBaseURL("http://example.invalid"),
			)
			require.NoError(t, err)
			gitlab.SetClients(mockClient, nil)

			first, err := resolveRESTClient()
			require.NoError(t, err)
			require.Same(t, mockClient, first)

			second, err := resolveRESTClient()
			require.NoError(t, err)
			require.Same(t, first, second)
		},
	)
}

func TestResolveRESTClient_ConcurrentAccess(t *testing.T) {
	defer SetRESTClient(nil)
	defer gitlab.SetClients(nil, nil)
	SetRESTClient(nil)

	mockRest := newMockRESTClient(t, staticJSONHandler(http.StatusOK, `[]`))
	gitlab.SetClients(mockRest, nil)

	const n = 50
	var wg sync.WaitGroup
	results := make([]*gitlabapi.Client, n)
	errs := make([]error, n)
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = resolveRESTClient()
		}(i)
	}
	close(start)
	wg.Wait()

	for i := range n {
		require.NoError(t, errs[i])
		require.NotNil(t, results[i])
		require.Same(t, mockRest, results[i])
	}
}

func TestFindPipelineForMR(t *testing.T) {
	t.Run(
		"selects the pipeline with the highest id when multiple pipelines are returned out of order",
		func(t *testing.T) {
			defer SetRESTClient(nil)

			mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Contains(t, r.URL.Path, "/merge_requests/5/pipelines")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`[
				{"id":10,"status":"success","web_url":"https://gitlab.example.com/group/proj/-/pipelines/10"},
				{"id":30,"status":"running","web_url":"https://gitlab.example.com/group/proj/-/pipelines/30"},
				{"id":20,"status":"failed","web_url":"https://gitlab.example.com/group/proj/-/pipelines/20"}
			]`))
			})
			SetRESTClient(mockClient)

			pipeline, err := FindPipelineForMR("group/proj", 5)
			require.NoError(t, err)
			assert.EqualValues(t, 30, pipeline.ID)
			assert.Equal(t, StatusRunning, pipeline.Status)
			assert.Equal(
				t,
				"https://gitlab.example.com/group/proj/-/pipelines/30",
				pipeline.WebURL,
			)
			assert.Empty(t, pipeline.Jobs)
		},
	)

	t.Run(
		"returns a zero value pipeline without an error when the mr has no pipelines yet",
		func(t *testing.T) {
			defer SetRESTClient(nil)

			mockClient := newMockRESTClient(t, staticJSONHandler(http.StatusOK, `[]`))
			SetRESTClient(mockClient)

			pipeline, err := FindPipelineForMR("group/proj", 5)
			require.NoError(t, err)
			assert.Equal(t, MergeRequestPipeline{}, pipeline)
		},
	)

	t.Run(
		"propagates an error when the server responds with http 404 not found",
		func(t *testing.T) {
			defer SetRESTClient(nil)

			mockClient := newMockRESTClient(t, staticJSONHandler(http.StatusNotFound, ""))
			SetRESTClient(mockClient)

			_, err := FindPipelineForMR("group/proj", 5)
			require.Error(t, err)
		},
	)

	t.Run(
		"propagates an error when the server responds with http 500 internal server error",
		func(t *testing.T) {
			defer SetRESTClient(nil)

			mockClient := newMockRESTClient(
				t,
				staticJSONHandler(http.StatusInternalServerError, ""),
			)
			SetRESTClient(mockClient)

			_, err := FindPipelineForMR("group/proj", 5)
			require.Error(t, err)
		},
	)
}

func TestListPipelineJobs(t *testing.T) {
	t.Run("converts every job field preserving the order returned by the api", func(t *testing.T) {
		defer SetRESTClient(nil)

		mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Contains(t, r.URL.Path, "/pipelines/55/jobs")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[
				{"id":100,"name":"build","stage":"build","status":"success","web_url":"https://gitlab.example.com/group/proj/-/jobs/100","allow_failure":false},
				{"id":101,"name":"deploy","stage":"deploy","status":"manual","web_url":"https://gitlab.example.com/group/proj/-/jobs/101","allow_failure":true}
			]`))
		})
		SetRESTClient(mockClient)

		jobs, err := ListPipelineJobs("group/proj", 55, "")
		require.NoError(t, err)
		require.Len(t, jobs, 2)
		assert.Equal(t, PipelineJob{
			ID:           100,
			Name:         "build",
			Stage:        "build",
			Status:       StatusSuccess,
			WebURL:       "https://gitlab.example.com/group/proj/-/jobs/100",
			AllowFailure: false,
		}, jobs[0])
		assert.Equal(t, PipelineJob{
			ID:           101,
			Name:         "deploy",
			Stage:        "deploy",
			Status:       StatusManual,
			WebURL:       "https://gitlab.example.com/group/proj/-/jobs/101",
			AllowFailure: true,
		}, jobs[1])
	})

	t.Run("follows pagination and aggregates jobs from every page", func(t *testing.T) {
		defer SetRESTClient(nil)

		mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
			page := r.URL.Query().Get("page")
			w.Header().Set("Content-Type", "application/json")
			switch page {
			case "", "1":
				// First page: only skipped jobs, and there is a next page.
				w.Header().Set("X-Next-Page", "2")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`[
					{"id":1,"name":"a","stage":"s","status":"skipped"},
					{"id":2,"name":"b","stage":"s","status":"skipped"}
				]`))
			case "2":
				// Last page: the failing job lives here (no X-Next-Page header).
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`[
					{"id":3,"name":"c","stage":"s","status":"failed"}
				]`))
			default:
				t.Fatalf("unexpected page requested: %q", page)
			}
		})
		SetRESTClient(mockClient)

		jobs, err := ListPipelineJobs("group/proj", 55, "")
		require.NoError(t, err)
		require.Len(t, jobs, 3, "must aggregate jobs across all pages, not just the first")

		statuses := make([]PipelineStatus, 0, len(jobs))
		for _, j := range jobs {
			statuses = append(statuses, j.Status)
		}
		assert.Contains(
			t,
			statuses,
			StatusFailed,
			"the failing job on a later page must not be dropped",
		)
	})

	t.Run("sends no scope query parameter when scope is empty", func(t *testing.T) {
		defer SetRESTClient(nil)

		var capturedRawQuery string
		mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
			capturedRawQuery = r.URL.RawQuery
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
		})
		SetRESTClient(mockClient)

		_, err := ListPipelineJobs("group/proj", 55, "")
		require.NoError(t, err)
		assert.NotContains(t, capturedRawQuery, "scope")
	})

	t.Run("sends scope[]=manual query parameter when scope is manual", func(t *testing.T) {
		defer SetRESTClient(nil)

		var capturedScope []string
		mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
			capturedScope = r.URL.Query()["scope[]"]
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
		})
		SetRESTClient(mockClient)

		_, err := ListPipelineJobs("group/proj", 55, "manual")
		require.NoError(t, err)
		assert.Equal(t, []string{"manual"}, capturedScope)
	})

	t.Run("returns an empty slice without error when the pipeline has no jobs", func(t *testing.T) {
		defer SetRESTClient(nil)

		mockClient := newMockRESTClient(t, staticJSONHandler(http.StatusOK, `[]`))
		SetRESTClient(mockClient)

		jobs, err := ListPipelineJobs("group/proj", 55, "")
		require.NoError(t, err)
		assert.Empty(t, jobs)
	})

	t.Run("propagates an error when the server responds with http 500", func(t *testing.T) {
		defer SetRESTClient(nil)

		mockClient := newMockRESTClient(t, staticJSONHandler(http.StatusInternalServerError, ""))
		SetRESTClient(mockClient)

		_, err := ListPipelineJobs("group/proj", 55, "")
		require.Error(t, err)
	})
}

func TestPlayJob(t *testing.T) {
	t.Run(
		"returns nil and sends a post request to the jobs play endpoint when the server accepts it",
		func(t *testing.T) {
			defer SetRESTClient(nil)

			var gotMethod, gotPath string
			mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				gotPath = r.URL.Path
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"id":77,"name":"deploy","status":"pending"}`))
			})
			SetRESTClient(mockClient)

			err := PlayJob("group/proj", 77)
			require.NoError(t, err)
			assert.Equal(t, http.MethodPost, gotMethod)
			assert.Contains(t, gotPath, "/jobs/77/play")
		},
	)

	t.Run(
		"propagates an error when the server responds with http 403 forbidden",
		func(t *testing.T) {
			defer SetRESTClient(nil)

			mockClient := newMockRESTClient(t, staticJSONHandler(http.StatusForbidden, ""))
			SetRESTClient(mockClient)

			err := PlayJob("group/proj", 77)
			require.Error(t, err)
		},
	)
}

const singleMergeRequestResponseForPipelineEnrichmentTests = `{"data":{"project":{"mergeRequest":{
	"iid":"42","title":"Fix bug","state":"opened","draft":false,
	"author":{"username":"jdoe"},
	"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
	"webUrl":"https://gitlab.com/group/proj/-/merge_requests/42",
	"sourceBranch":"feature-x","targetBranch":"main",
	"detailedMergeStatus":"MERGEABLE","approved":true,
	"diffStatsSummary":{"additions":10,"deletions":2},
	"labels":{"nodes":[]}
}}}}`

func TestFetchPullRequest_PopulatesPipelineAndJobsFromRESTBestEffort(t *testing.T) {
	t.Run(
		"combines graphql mr data with rest pipeline and job data end to end",
		func(t *testing.T) {
			defer SetClient(nil)
			defer SetRESTClient(nil)
			isolateGitLabAuthEnv(t)
			t.Setenv("GITLAB_TOKEN", "test-token")

			graphqlClient := newMockGraphQLClient(
				t,
				staticJSONHandler(
					http.StatusOK,
					singleMergeRequestResponseForPipelineEnrichmentTests,
				),
			)
			SetClient(graphqlClient)

			var jobsRawQuery string
			mockRest := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.Contains(r.URL.Path, "/merge_requests/42/pipelines"):
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write(
						[]byte(
							`[{"id":900,"status":"running","web_url":"https://gitlab.com/group/proj/-/pipelines/900"}]`,
						),
					)
				case strings.Contains(r.URL.Path, "/pipelines/900/jobs"):
					jobsRawQuery = r.URL.RawQuery
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`[
					{"id":1,"name":"build","stage":"build","status":"success","web_url":"https://gitlab.com/group/proj/-/jobs/1","allow_failure":false},
					{"id":2,"name":"test","stage":"test","status":"running","web_url":"https://gitlab.com/group/proj/-/jobs/2","allow_failure":false}
				]`))
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			})
			SetRESTClient(mockRest)

			pr, err := FetchPullRequest("https://gitlab.com/group/proj/-/merge_requests/42")
			require.NoError(t, err)

			assert.Equal(t, int64(900), pr.Pipeline.ID)
			assert.Equal(t, StatusRunning, pr.Pipeline.Status)
			assert.Equal(t, "https://gitlab.com/group/proj/-/pipelines/900", pr.Pipeline.WebURL)
			require.Len(t, pr.Pipeline.Jobs, 2)
			assert.Equal(t, PipelineJob{
				ID:           1,
				Name:         "build",
				Stage:        "build",
				Status:       StatusSuccess,
				WebURL:       "https://gitlab.com/group/proj/-/jobs/1",
				AllowFailure: false,
			}, pr.Pipeline.Jobs[0])
			assert.Equal(t, PipelineJob{
				ID:           2,
				Name:         "test",
				Stage:        "test",
				Status:       StatusRunning,
				WebURL:       "https://gitlab.com/group/proj/-/jobs/2",
				AllowFailure: false,
			}, pr.Pipeline.Jobs[1])

			assert.NotContains(t, jobsRawQuery, "scope")
		},
	)

	t.Run(
		"keeps a zero value pipeline without fetching jobs when the mr has no pipeline yet",
		func(t *testing.T) {
			defer SetClient(nil)
			defer SetRESTClient(nil)
			isolateGitLabAuthEnv(t)
			t.Setenv("GITLAB_TOKEN", "test-token")

			graphqlClient := newMockGraphQLClient(
				t,
				staticJSONHandler(
					http.StatusOK,
					singleMergeRequestResponseForPipelineEnrichmentTests,
				),
			)
			SetClient(graphqlClient)

			jobsEndpointHit := false
			mockRest := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.Contains(r.URL.Path, "/merge_requests/42/pipelines"):
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`[]`))
				case strings.Contains(r.URL.Path, "/jobs"):
					jobsEndpointHit = true
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`[]`))
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			})
			SetRESTClient(mockRest)

			pr, err := FetchPullRequest("https://gitlab.com/group/proj/-/merge_requests/42")
			require.NoError(t, err)
			assert.Equal(t, MergeRequestPipeline{}, pr.Pipeline)
			assert.False(t, jobsEndpointHit)
		},
	)

	t.Run(
		"returns the mr successfully with a zero value pipeline when the rest pipeline lookup fails",
		func(t *testing.T) {
			defer SetClient(nil)
			defer SetRESTClient(nil)
			isolateGitLabAuthEnv(t)
			t.Setenv("GITLAB_TOKEN", "test-token")

			logBuf := captureLogOutput(t)

			graphqlClient := newMockGraphQLClient(
				t,
				staticJSONHandler(
					http.StatusOK,
					singleMergeRequestResponseForPipelineEnrichmentTests,
				),
			)
			SetClient(graphqlClient)

			mockRest := newMockRESTClient(t, staticJSONHandler(http.StatusInternalServerError, ""))
			SetRESTClient(mockRest)

			pr, err := FetchPullRequest("https://gitlab.com/group/proj/-/merge_requests/42")
			require.NoError(t, err)
			assert.Equal(t, 42, pr.Number)
			assert.Equal(t, MergeRequestPipeline{}, pr.Pipeline)
			assert.Contains(t, logBuf.String(), "WARN")
		},
	)

	t.Run(
		"keeps the pipeline without jobs when the rest jobs lookup fails, logging a warning",
		func(t *testing.T) {
			defer SetClient(nil)
			defer SetRESTClient(nil)
			isolateGitLabAuthEnv(t)
			t.Setenv("GITLAB_TOKEN", "test-token")

			logBuf := captureLogOutput(t)

			graphqlClient := newMockGraphQLClient(
				t,
				staticJSONHandler(
					http.StatusOK,
					singleMergeRequestResponseForPipelineEnrichmentTests,
				),
			)
			SetClient(graphqlClient)

			mockRest := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.Contains(r.URL.Path, "/merge_requests/42/pipelines"):
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write(
						[]byte(
							`[{"id":900,"status":"running","web_url":"https://gitlab.com/group/proj/-/pipelines/900"}]`,
						),
					)
				case strings.Contains(r.URL.Path, "/jobs"):
					w.WriteHeader(http.StatusInternalServerError)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			})
			SetRESTClient(mockRest)

			pr, err := FetchPullRequest("https://gitlab.com/group/proj/-/merge_requests/42")
			require.NoError(t, err)
			assert.Equal(t, int64(900), pr.Pipeline.ID)
			assert.Equal(t, StatusRunning, pr.Pipeline.Status)
			assert.Empty(t, pr.Pipeline.Jobs)
			assert.Contains(t, logBuf.String(), "WARN")
		},
	)
}

func TestFetchPipelineBestEffort(t *testing.T) {
	t.Run(
		"returns a zero value pipeline without calling rest when mock data is enabled",
		func(t *testing.T) {
			defer SetRESTClient(nil)
			isolateGitLabAuthEnv(t)
			t.Setenv("GITLAB_TOKEN", "test-token")
			t.Setenv(config.FF_MOCK_DATA, "1")

			restHit := false
			SetRESTClient(newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
				restHit = true
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`[]`))
			}))

			got := fetchPipelineBestEffort("group/proj", "42")

			assert.Equal(t, MergeRequestPipeline{}, got)
			assert.False(
				t,
				restHit,
				"fetchPipelineBestEffort must not call the rest api when FF_MOCK_DATA is enabled",
			)
		},
	)

	t.Run(
		"returns a zero value pipeline without resolving a rest client when none is cached and no gitlab token is configured",
		func(t *testing.T) {
			SetRESTClient(nil)
			defer SetRESTClient(nil)
			isolateGitLabAuthEnv(t)

			got := fetchPipelineBestEffort("group/proj", "42")

			assert.Equal(t, MergeRequestPipeline{}, got)
			assert.Nil(
				t,
				gitlabRESTClient,
				"fetchPipelineBestEffort must not resolve a rest client without a configured gitlab token",
			)
		},
	)

	t.Run(
		"calls rest using an already cached client even when no gitlab token is configured",
		func(t *testing.T) {
			defer SetRESTClient(nil)
			isolateGitLabAuthEnv(t)

			restHit := false
			mockRest := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
				restHit = true
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.Contains(r.URL.Path, "/merge_requests/42/pipelines"):
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(
						`[{"id":900,"status":"running","web_url":"https://gitlab.com/group/proj/-/pipelines/900"}]`,
					))
				case strings.Contains(r.URL.Path, "/pipelines/900/jobs"):
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`[]`))
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			})
			SetRESTClient(mockRest)

			got := fetchPipelineBestEffort("group/proj", "42")

			assert.True(
				t,
				restHit,
				"fetchPipelineBestEffort must use an already cached rest client without checking for a gitlab token",
			)
			assert.EqualValues(t, 900, got.ID)
			assert.Equal(t, StatusRunning, got.Status)
		},
	)

	t.Run(
		"falls through the guard and resolves a rest client when none is cached but a gitlab token is configured",
		func(t *testing.T) {
			SetRESTClient(nil)
			defer SetRESTClient(nil)
			defer gitlab.SetClients(nil, nil)
			isolateGitLabAuthEnv(t)
			t.Setenv("GITLAB_TOKEN", "test-token")

			mockRest := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.Contains(r.URL.Path, "/merge_requests/42/pipelines"):
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(
						`[{"id":900,"status":"running","web_url":"https://gitlab.com/group/proj/-/pipelines/900"}]`,
					))
				case strings.Contains(r.URL.Path, "/pipelines/900/jobs"):
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`[]`))
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			})
			gitlab.SetClients(mockRest, nil)

			got := fetchPipelineBestEffort("group/proj", "42")

			assert.EqualValues(t, 900, got.ID)
			assert.Equal(t, StatusRunning, got.Status)
			assert.Same(t, mockRest, gitlabRESTClient)
		},
	)
}

func TestDiffPositionLineAndPath(t *testing.T) {
	t.Run("nil position returns empty path and zero line", func(t *testing.T) {
		path, line := diffPositionLineAndPath(nil)

		assert.Equal(t, "", path)
		assert.Equal(t, 0, line)
	})

	t.Run("non zero new line is used as the line", func(t *testing.T) {
		path, line := diffPositionLineAndPath(&gitlabNotePositionNode{
			FilePath: "main.go",
			NewLine:  10,
			OldLine:  5,
		})

		assert.Equal(t, "main.go", path)
		assert.Equal(t, 10, line)
	})

	t.Run("falls back to old line when new line is zero", func(t *testing.T) {
		path, line := diffPositionLineAndPath(&gitlabNotePositionNode{
			FilePath: "main.go",
			NewLine:  0,
			OldLine:  7,
		})

		assert.Equal(t, "main.go", path)
		assert.Equal(t, 7, line)
	})
}

func TestMergeRequestNodeNormalizesState(t *testing.T) {
	// GitLab returns merge request states lowercase (opened/closed/merged/
	// locked); the TUI renders the GitHub-style uppercase OPEN/CLOSED/MERGED,
	// so the adapter must normalize or the state glyph column shows "-".
	cases := map[string]string{
		"opened": "OPEN",
		"closed": "CLOSED",
		"merged": "MERGED",
		"locked": "OPEN",
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			n := mergeRequestNode{State: in}
			assert.Equal(t, want, n.toPullRequestData("").State)
			assert.Equal(t, want, n.toEnrichedPullRequestData("").State)
		})
	}
}

func TestCommitsFromNodes(t *testing.T) {
	authoredAt := time.Date(2026, 1, 4, 8, 30, 0, 0, time.UTC)

	t.Run("maps each commit node preferring the GitLab author username", func(t *testing.T) {
		commits := commitsFromNodes([]gitlabCommitNode{
			{
				Sha:          "abc123",
				Title:        "Fix the bug",
				WebUrl:       "https://gitlab.com/group/proj/-/commit/abc123",
				AuthoredDate: authoredAt,
				Author:       struct{ Username string }{Username: "jdoe"},
				AuthorName:   "John Doe",
			},
		})

		require.Len(t, commits, 1)
		assert.Equal(t, "abc123", commits[0].Sha)
		assert.Equal(t, "Fix the bug", commits[0].Title)
		assert.Equal(t, "jdoe", commits[0].Author)
		assert.Equal(t, authoredAt, commits[0].CreatedAt)
		assert.Equal(t, "https://gitlab.com/group/proj/-/commit/abc123", commits[0].Url)
	})

	t.Run("falls back to authorName when no GitLab user is linked", func(t *testing.T) {
		commits := commitsFromNodes([]gitlabCommitNode{
			{Sha: "def456", Title: "External commit", AuthorName: "Jane External"},
		})

		require.Len(t, commits, 1)
		assert.Equal(t, "Jane External", commits[0].Author)
	})

	t.Run("empty nodes returns empty commits", func(t *testing.T) {
		assert.Empty(t, commitsFromNodes(nil))
	})
}

func TestChangedFilesFromDiffStats(t *testing.T) {
	t.Run("maps each diff stat to a changed file", func(t *testing.T) {
		files := changedFilesFromDiffStats([]gitlabDiffStatNode{
			{Path: "main.go", Additions: 10, Deletions: 2},
			{Path: "internal/data/prapi.go", Additions: 0, Deletions: 7},
		})

		require.Len(t, files.Nodes, 2)
		assert.Equal(t, 2, files.TotalCount)
		assert.Equal(t, "main.go", files.Nodes[0].Path)
		assert.Equal(t, 10, files.Nodes[0].Additions)
		assert.Equal(t, 2, files.Nodes[0].Deletions)
		assert.Equal(t, "internal/data/prapi.go", files.Nodes[1].Path)
		assert.Equal(t, 0, files.Nodes[1].Additions)
		assert.Equal(t, 7, files.Nodes[1].Deletions)
	})

	t.Run("empty diff stats returns empty changed files", func(t *testing.T) {
		files := changedFilesFromDiffStats(nil)

		assert.Empty(t, files.Nodes)
		assert.Equal(t, 0, files.TotalCount)
	})
}

func TestCommentsAndReviewThreadsFromDiscussions(t *testing.T) {
	updatedAt := time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC)

	t.Run("note without position becomes a top level comment", func(t *testing.T) {
		discussions := []gitlabDiscussionNode{
			discussionWithNotes(gitlabNoteNode{
				Author:    usernameAuthor("alice"),
				Body:      "LGTM",
				UpdatedAt: updatedAt,
				System:    false,
			}),
		}

		comments, threads := commentsAndReviewThreadsFromDiscussions(discussions)

		require.Len(t, comments.Nodes, 1)
		assert.Equal(t, "alice", comments.Nodes[0].Author.Login)
		assert.Equal(t, "LGTM", comments.Nodes[0].Body)
		assert.Equal(t, updatedAt, comments.Nodes[0].UpdatedAt)
		assert.Empty(t, threads.Nodes)
	})

	t.Run("note with position becomes a review thread comment", func(t *testing.T) {
		discussions := []gitlabDiscussionNode{
			discussionWithNotes(gitlabNoteNode{
				Author:    usernameAuthor("bob"),
				Body:      "fix this",
				UpdatedAt: updatedAt,
				System:    false,
				Position: &gitlabNotePositionNode{
					FilePath: "main.go",
					NewLine:  10,
				},
			}),
		}

		comments, threads := commentsAndReviewThreadsFromDiscussions(discussions)

		assert.Empty(t, comments.Nodes)
		require.Len(t, threads.Nodes, 1)
		thread := threads.Nodes[0]
		assert.Equal(t, "main.go", thread.Path)
		assert.Equal(t, 10, thread.Line)
		assert.Equal(t, 10, thread.OriginalLine)
		assert.Equal(t, 10, thread.StartLine)
		require.Len(t, thread.Comments.Nodes, 1)
		reviewComment := thread.Comments.Nodes[0]
		assert.Equal(t, "bob", reviewComment.Author.Login)
		assert.Equal(t, "fix this", reviewComment.Body)
		assert.Equal(t, updatedAt, reviewComment.UpdatedAt)
		assert.Equal(t, 10, reviewComment.StartLine)
		assert.Equal(t, 10, reviewComment.Line)
	})

	t.Run("review thread falls back to old line when new line is zero", func(t *testing.T) {
		discussions := []gitlabDiscussionNode{
			discussionWithNotes(gitlabNoteNode{
				Author:    usernameAuthor("bob"),
				Body:      "fix this",
				UpdatedAt: updatedAt,
				System:    false,
				Position: &gitlabNotePositionNode{
					FilePath: "main.go",
					NewLine:  0,
					OldLine:  9,
				},
			}),
		}

		_, threads := commentsAndReviewThreadsFromDiscussions(discussions)

		require.Len(t, threads.Nodes, 1)
		assert.Equal(t, 9, threads.Nodes[0].Line)
		assert.Equal(t, 9, threads.Nodes[0].OriginalLine)
		assert.Equal(t, 9, threads.Nodes[0].StartLine)
	})

	t.Run("reply without position stays in the same review thread", func(t *testing.T) {
		// In GitLab only the first note of a diff discussion carries a
		// position; replies arrive with position: null. They must stay in the
		// same review thread instead of becoming top-level comments.
		discussions := []gitlabDiscussionNode{
			discussionWithNotes(
				gitlabNoteNode{
					Author:    usernameAuthor("bob"),
					Body:      "fix this",
					UpdatedAt: updatedAt,
					System:    false,
					Position: &gitlabNotePositionNode{
						FilePath: "main.go",
						NewLine:  10,
					},
				},
				gitlabNoteNode{
					Author:    usernameAuthor("alice"),
					Body:      "done",
					UpdatedAt: updatedAt,
					System:    false,
				},
			),
		}

		comments, threads := commentsAndReviewThreadsFromDiscussions(discussions)

		assert.Empty(t, comments.Nodes)
		require.Len(t, threads.Nodes, 1)
		thread := threads.Nodes[0]
		assert.Equal(t, "main.go", thread.Path)
		assert.Equal(t, 10, thread.Line)
		require.Len(t, thread.Comments.Nodes, 2)
		assert.Equal(t, "bob", thread.Comments.Nodes[0].Author.Login)
		assert.Equal(t, "fix this", thread.Comments.Nodes[0].Body)
		assert.Equal(t, "alice", thread.Comments.Nodes[1].Author.Login)
		assert.Equal(t, "done", thread.Comments.Nodes[1].Body)
		assert.Equal(t, 10, thread.Comments.Nodes[1].Line)
	})

	t.Run("filters system notes from both comments and review threads", func(t *testing.T) {
		discussions := []gitlabDiscussionNode{
			discussionWithNotes(
				gitlabNoteNode{
					Author: usernameAuthor("ghost"),
					Body:   "closed this merge request",
					System: true,
				},
				gitlabNoteNode{
					Author: usernameAuthor("ghost"),
					Body:   "changed the description",
					System: true,
					Position: &gitlabNotePositionNode{
						FilePath: "main.go",
						NewLine:  3,
					},
				},
				gitlabNoteNode{
					Author: usernameAuthor("alice"),
					Body:   "real comment",
					System: false,
				},
			),
		}

		comments, threads := commentsAndReviewThreadsFromDiscussions(discussions)

		require.Len(t, comments.Nodes, 1)
		assert.Equal(t, "alice", comments.Nodes[0].Author.Login)
		assert.Empty(t, threads.Nodes)
	})

	t.Run("empty discussions returns empty comments and review threads", func(t *testing.T) {
		comments, threads := commentsAndReviewThreadsFromDiscussions(nil)

		assert.Empty(t, comments.Nodes)
		assert.Empty(t, threads.Nodes)
	})
}

func TestReviewsFromApprovedBy(t *testing.T) {
	t.Run("maps each approved by user to an approved review", func(t *testing.T) {
		reviews := reviewsFromApprovedBy([]gitlabUserNode{
			{Username: "carol"},
			{Username: "dave"},
		})

		require.Len(t, reviews.Nodes, 2)
		assert.Equal(t, "carol", reviews.Nodes[0].Author.Login)
		assert.Equal(t, "APPROVED", reviews.Nodes[0].State)
		assert.Equal(t, "dave", reviews.Nodes[1].Author.Login)
		assert.Equal(t, "APPROVED", reviews.Nodes[1].State)
		assert.Equal(t, 2, reviews.TotalCount)
	})

	t.Run("empty nodes returns empty reviews", func(t *testing.T) {
		reviews := reviewsFromApprovedBy(nil)

		assert.Empty(t, reviews.Nodes)
		assert.Equal(t, 0, reviews.TotalCount)
	})
}

func TestReviewRequestsFromReviewers(t *testing.T) {
	t.Run("maps each reviewer to a requested reviewer user", func(t *testing.T) {
		requests := reviewRequestsFromReviewers([]gitlabUserNode{{Username: "erin"}})

		require.Len(t, requests.Nodes, 1)
		node := requests.Nodes[0]
		assert.Equal(t, "erin", node.RequestedReviewer.User.Login)
		assert.Equal(t, "erin", node.GetReviewerDisplayName())
		assert.False(t, node.AsCodeOwner)
		assert.Empty(t, node.RequestedReviewer.Team.Slug)
		assert.Empty(t, node.RequestedReviewer.Bot.Login)
		assert.Empty(t, node.RequestedReviewer.Mannequin.Login)
	})

	t.Run("empty nodes returns empty review requests", func(t *testing.T) {
		requests := reviewRequestsFromReviewers(nil)

		assert.Empty(t, requests.Nodes)
	})
}

func TestFetchPullRequest_PopulatesCommentsFromDiscussionNoteWithoutPosition(t *testing.T) {
	defer SetClient(nil)
	defer SetRESTClient(nil)

	responseBody := `{"data":{"project":{"mergeRequest":{
		"iid":"42","title":"Fix bug","state":"opened","draft":false,
		"author":{"username":"jdoe"},
		"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
		"webUrl":"https://gitlab.com/group/proj/-/merge_requests/42",
		"sourceBranch":"feature-x","targetBranch":"main",
		"detailedMergeStatus":"MERGEABLE","approved":true,
		"diffStatsSummary":{"additions":10,"deletions":2},
		"labels":{"nodes":[]},
		"discussions":{"nodes":[
			{"notes":{"nodes":[
				{"author":{"username":"alice"},"body":"LGTM","createdAt":"2026-01-03T00:00:00Z","updatedAt":"2026-01-04T00:00:00Z","system":false,"position":null}
			]}}
		]},
		"approvedBy":{"nodes":[]},
		"reviewers":{"nodes":[]}
	}}}}`

	mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, responseBody))
	SetClient(mockClient)
	SetRESTClient(newMockRESTClient(t, staticJSONHandler(http.StatusNotFound, "")))

	pr, err := FetchPullRequest("https://gitlab.com/group/proj/-/merge_requests/42")
	require.NoError(t, err)

	wantUpdatedAt, timeErr := time.Parse(time.RFC3339, "2026-01-04T00:00:00Z")
	require.NoError(t, timeErr)

	require.Len(t, pr.Comments.Nodes, 1)
	assert.Equal(t, "alice", pr.Comments.Nodes[0].Author.Login)
	assert.Equal(t, "LGTM", pr.Comments.Nodes[0].Body)
	assert.Equal(t, wantUpdatedAt, pr.Comments.Nodes[0].UpdatedAt)
	assert.Empty(t, pr.ReviewThreads.Nodes)
}

func TestFetchPullRequest_PopulatesReviewThreadFromDiscussionNoteWithPosition(t *testing.T) {
	defer SetClient(nil)
	defer SetRESTClient(nil)

	responseBody := `{"data":{"project":{"mergeRequest":{
		"iid":"42","title":"Fix bug","state":"opened","draft":false,
		"author":{"username":"jdoe"},
		"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
		"webUrl":"https://gitlab.com/group/proj/-/merge_requests/42",
		"sourceBranch":"feature-x","targetBranch":"main",
		"detailedMergeStatus":"MERGEABLE","approved":true,
		"diffStatsSummary":{"additions":10,"deletions":2},
		"labels":{"nodes":[]},
		"discussions":{"nodes":[
			{"notes":{"nodes":[
				{"author":{"username":"bob"},"body":"fix this","createdAt":"2026-01-03T00:00:00Z","updatedAt":"2026-01-04T00:00:00Z","system":false,
				 "position":{"filePath":"main.go","newLine":10,"oldLine":0}}
			]}}
		]},
		"approvedBy":{"nodes":[]},
		"reviewers":{"nodes":[]}
	}}}}`

	mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, responseBody))
	SetClient(mockClient)
	SetRESTClient(newMockRESTClient(t, staticJSONHandler(http.StatusNotFound, "")))

	pr, err := FetchPullRequest("https://gitlab.com/group/proj/-/merge_requests/42")
	require.NoError(t, err)

	assert.Empty(t, pr.Comments.Nodes)
	require.Len(t, pr.ReviewThreads.Nodes, 1)
	thread := pr.ReviewThreads.Nodes[0]
	assert.Equal(t, "main.go", thread.Path)
	assert.Equal(t, 10, thread.Line)
	require.Len(t, thread.Comments.Nodes, 1)
	assert.Equal(t, "bob", thread.Comments.Nodes[0].Author.Login)
	assert.Equal(t, "fix this", thread.Comments.Nodes[0].Body)
	assert.Equal(t, 10, thread.Comments.Nodes[0].StartLine)
	assert.Equal(t, 10, thread.Comments.Nodes[0].Line)
}

func TestFetchPullRequest_FiltersSystemNoteFromCommentsAndReviewThreads(t *testing.T) {
	defer SetClient(nil)
	defer SetRESTClient(nil)

	responseBody := `{"data":{"project":{"mergeRequest":{
		"iid":"42","title":"Fix bug","state":"opened","draft":false,
		"author":{"username":"jdoe"},
		"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
		"webUrl":"https://gitlab.com/group/proj/-/merge_requests/42",
		"sourceBranch":"feature-x","targetBranch":"main",
		"detailedMergeStatus":"MERGEABLE","approved":true,
		"diffStatsSummary":{"additions":10,"deletions":2},
		"labels":{"nodes":[]},
		"discussions":{"nodes":[
			{"notes":{"nodes":[
				{"author":{"username":"ghost"},"body":"changed the description","system":true,"position":null},
				{"author":{"username":"ghost"},"body":"resolved a thread","system":true,"position":{"filePath":"main.go","newLine":3,"oldLine":0}}
			]}}
		]},
		"approvedBy":{"nodes":[]},
		"reviewers":{"nodes":[]}
	}}}}`

	mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, responseBody))
	SetClient(mockClient)
	SetRESTClient(newMockRESTClient(t, staticJSONHandler(http.StatusNotFound, "")))

	pr, err := FetchPullRequest("https://gitlab.com/group/proj/-/merge_requests/42")
	require.NoError(t, err)

	assert.Empty(t, pr.Comments.Nodes)
	assert.Empty(t, pr.ReviewThreads.Nodes)
}

func TestFetchPullRequest_PopulatesReviewsFromApprovedByWithTwoUsers(t *testing.T) {
	defer SetClient(nil)
	defer SetRESTClient(nil)

	responseBody := `{"data":{"project":{"mergeRequest":{
		"iid":"42","title":"Fix bug","state":"opened","draft":false,
		"author":{"username":"jdoe"},
		"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
		"webUrl":"https://gitlab.com/group/proj/-/merge_requests/42",
		"sourceBranch":"feature-x","targetBranch":"main",
		"detailedMergeStatus":"MERGEABLE","approved":true,
		"diffStatsSummary":{"additions":10,"deletions":2},
		"labels":{"nodes":[]},
		"discussions":{"nodes":[]},
		"approvedBy":{"nodes":[{"username":"carol"},{"username":"dave"}]},
		"reviewers":{"nodes":[]}
	}}}}`

	mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, responseBody))
	SetClient(mockClient)
	SetRESTClient(newMockRESTClient(t, staticJSONHandler(http.StatusNotFound, "")))

	pr, err := FetchPullRequest("https://gitlab.com/group/proj/-/merge_requests/42")
	require.NoError(t, err)

	require.Len(t, pr.Reviews.Nodes, 2)
	assert.Equal(t, "carol", pr.Reviews.Nodes[0].Author.Login)
	assert.Equal(t, "APPROVED", pr.Reviews.Nodes[0].State)
	assert.Equal(t, "dave", pr.Reviews.Nodes[1].Author.Login)
	assert.Equal(t, "APPROVED", pr.Reviews.Nodes[1].State)
	assert.Equal(t, 2, pr.Reviews.TotalCount)
}

func TestFetchPullRequest_PopulatesReviewRequestsFromReviewersWithOneUser(t *testing.T) {
	defer SetClient(nil)
	defer SetRESTClient(nil)

	responseBody := `{"data":{"project":{"mergeRequest":{
		"iid":"42","title":"Fix bug","state":"opened","draft":false,
		"author":{"username":"jdoe"},
		"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
		"webUrl":"https://gitlab.com/group/proj/-/merge_requests/42",
		"sourceBranch":"feature-x","targetBranch":"main",
		"detailedMergeStatus":"MERGEABLE","approved":true,
		"diffStatsSummary":{"additions":10,"deletions":2},
		"labels":{"nodes":[]},
		"discussions":{"nodes":[]},
		"approvedBy":{"nodes":[]},
		"reviewers":{"nodes":[{"username":"erin"}]}
	}}}}`

	mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, responseBody))
	SetClient(mockClient)
	SetRESTClient(newMockRESTClient(t, staticJSONHandler(http.StatusNotFound, "")))

	pr, err := FetchPullRequest("https://gitlab.com/group/proj/-/merge_requests/42")
	require.NoError(t, err)

	require.Len(t, pr.ReviewRequests.Nodes, 1)
	assert.Equal(t, "erin", pr.ReviewRequests.Nodes[0].RequestedReviewer.User.Login)
	assert.Equal(t, "erin", pr.ReviewRequests.Nodes[0].GetReviewerDisplayName())
}

func TestFetchPullRequest_EmptyDiscussionsApprovedByAndReviewersLeaveActivityFieldsEmpty(
	t *testing.T,
) {
	defer SetClient(nil)
	defer SetRESTClient(nil)

	responseBody := `{"data":{"project":{"mergeRequest":{
		"iid":"42","title":"Fix bug","state":"opened","draft":false,
		"author":{"username":"jdoe"},
		"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
		"webUrl":"https://gitlab.com/group/proj/-/merge_requests/42",
		"sourceBranch":"feature-x","targetBranch":"main",
		"detailedMergeStatus":"MERGEABLE","approved":true,
		"diffStatsSummary":{"additions":10,"deletions":2},
		"labels":{"nodes":[]},
		"discussions":{"nodes":[]},
		"approvedBy":{"nodes":[]},
		"reviewers":{"nodes":[]}
	}}}}`

	mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, responseBody))
	SetClient(mockClient)
	SetRESTClient(newMockRESTClient(t, staticJSONHandler(http.StatusNotFound, "")))

	pr, err := FetchPullRequest("https://gitlab.com/group/proj/-/merge_requests/42")
	require.NoError(t, err)

	assert.Empty(t, pr.Comments.Nodes)
	assert.Empty(t, pr.ReviewThreads.Nodes)
	assert.Empty(t, pr.Reviews.Nodes)
	assert.Equal(t, 0, pr.Reviews.TotalCount)
	assert.Empty(t, pr.ReviewRequests.Nodes)
}

func TestFetchPullRequest_SuggestedReviewersStaysEmptyWhenActivityIsPopulated(t *testing.T) {
	defer SetClient(nil)
	defer SetRESTClient(nil)

	responseBody := `{"data":{"project":{"mergeRequest":{
		"iid":"42","title":"Fix bug","state":"opened","draft":false,
		"author":{"username":"jdoe"},
		"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
		"webUrl":"https://gitlab.com/group/proj/-/merge_requests/42",
		"sourceBranch":"feature-x","targetBranch":"main",
		"detailedMergeStatus":"MERGEABLE","approved":true,
		"diffStatsSummary":{"additions":10,"deletions":2},
		"labels":{"nodes":[]},
		"discussions":{"nodes":[
			{"notes":{"nodes":[
				{"author":{"username":"alice"},"body":"LGTM","updatedAt":"2026-01-04T00:00:00Z","system":false}
			]}}
		]},
		"approvedBy":{"nodes":[{"username":"carol"}]},
		"reviewers":{"nodes":[{"username":"erin"}]}
	}}}}`

	mockClient := newMockGraphQLClient(t, staticJSONHandler(http.StatusOK, responseBody))
	SetClient(mockClient)
	SetRESTClient(newMockRESTClient(t, staticJSONHandler(http.StatusNotFound, "")))

	pr, err := FetchPullRequest("https://gitlab.com/group/proj/-/merge_requests/42")
	require.NoError(t, err)

	require.NotEmpty(t, pr.Comments.Nodes)
	require.NotEmpty(t, pr.Reviews.Nodes)
	require.NotEmpty(t, pr.ReviewRequests.Nodes)
	assert.Empty(t, pr.SuggestedReviewers)
}

func TestFetchPullRequest_QueryDeclaresDiscussionsApprovedByAndReviewers(t *testing.T) {
	defer SetClient(nil)
	defer SetRESTClient(nil)

	responseBody := `{"data":{"project":{"mergeRequest":{
		"iid":"42","title":"Fix bug","state":"opened","draft":false,
		"author":{"username":"jdoe"},
		"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z",
		"webUrl":"https://gitlab.com/group/proj/-/merge_requests/42",
		"sourceBranch":"feature-x","targetBranch":"main",
		"detailedMergeStatus":"MERGEABLE","approved":true,
		"diffStatsSummary":{"additions":10,"deletions":2},
		"labels":{"nodes":[]},
		"discussions":{"nodes":[]},
		"approvedBy":{"nodes":[]},
		"reviewers":{"nodes":[]}
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
	SetRESTClient(newMockRESTClient(t, staticJSONHandler(http.StatusNotFound, "")))

	_, err := FetchPullRequest("https://gitlab.com/group/proj/-/merge_requests/42")
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, capturedBody.Query)
	assert.Contains(t, capturedBody.Query, "discussions")
	assert.Contains(t, capturedBody.Query, "approvedBy")
	assert.Contains(t, capturedBody.Query, "reviewers")
}

func TestFetchPullRequests_ListingQueryDoesNotDeclareDiscussionsApprovedByOrReviewers(
	t *testing.T,
) {
	defer SetClient(nil)

	var capturedBody graphQLRequestBody
	var mu sync.Mutex

	handler := func(w http.ResponseWriter, r *http.Request) {
		body := decodeGraphQLRequestBody(t, r)
		mu.Lock()
		capturedBody = body
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(singleMergeRequestProjectScopedResponse))
	}

	mockClient := newMockGraphQLClient(t, handler)
	SetClient(mockClient)

	_, err := FetchPullRequests("project:group/proj", 30, nil)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, capturedBody.Query)
	assert.NotContains(t, capturedBody.Query, "discussions")
	assert.NotContains(t, capturedBody.Query, "approvedBy")
	assert.NotContains(t, capturedBody.Query, "reviewers")
}
