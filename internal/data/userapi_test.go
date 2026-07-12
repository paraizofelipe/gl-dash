package data

import (
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchRepoUsers(t *testing.T) {
	t.Run("maps project members to users and sends the composed full path", func(t *testing.T) {
		ClearUserCache()
		defer ClearUserCache()
		defer SetClient(nil)

		responseBody := `{"data":{"project":{"projectMembers":{"nodes":[{"user":{"username":"jdoe","name":"John Doe"}}]}}}}`

		var captured graphQLRequestBody
		handler := func(w http.ResponseWriter, r *http.Request) {
			captured = decodeGraphQLRequestBody(t, r)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(responseBody))
		}
		mockClient := newMockGraphQLClient(t, handler)
		SetClient(mockClient)

		users, err := FetchRepoUsers("group", "proj")
		require.NoError(t, err)

		require.Equal(t, "group/proj", captured.Variables["fullPath"])
		require.Equal(t, []User{{Login: "jdoe", Name: "John Doe"}}, users)
	})

	t.Run(
		"caches results so a second call for the same repo does not hit the server again",
		func(t *testing.T) {
			ClearUserCache()
			defer ClearUserCache()
			defer SetClient(nil)

			responseBody := `{"data":{"project":{"projectMembers":{"nodes":[{"user":{"username":"jdoe","name":"John Doe"}}]}}}}`

			var requestCount int64
			handler := func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt64(&requestCount, 1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(responseBody))
			}
			mockClient := newMockGraphQLClient(t, handler)
			SetClient(mockClient)

			firstUsers, err := FetchRepoUsers("group", "proj")
			require.NoError(t, err)
			secondUsers, err := FetchRepoUsers("group", "proj")
			require.NoError(t, err)

			assert.EqualValues(t, 1, atomic.LoadInt64(&requestCount))
			assert.Equal(t, firstUsers, secondUsers)
		},
	)

	t.Run("propagates error when the server responds with http 500", func(t *testing.T) {
		ClearUserCache()
		defer ClearUserCache()
		defer SetClient(nil)

		mockClient := newMockGraphQLClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})
		SetClient(mockClient)

		_, err := FetchRepoUsers("group", "other-proj")
		require.Error(t, err)
	})

	t.Run("declares fullPath as a GraphQL ID, not a String", func(t *testing.T) {
		ClearUserCache()
		defer ClearUserCache()
		defer SetClient(nil)

		responseBody := `{"data":{"project":{"projectMembers":{"nodes":[]}}}}`

		var captured graphQLRequestBody
		handler := func(w http.ResponseWriter, r *http.Request) {
			captured = decodeGraphQLRequestBody(t, r)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(responseBody))
		}
		mockClient := newMockGraphQLClient(t, handler)
		SetClient(mockClient)

		_, err := FetchRepoUsers("group", "fullpath-check")
		require.NoError(t, err)

		require.NotEmpty(t, captured.Query)
		assert.Contains(
			t,
			captured.Query,
			"$fullPath:ID!",
			"Query.project(fullPath:) requires ID in the real GitLab schema, got query: %s",
			captured.Query,
		)
		assert.NotContains(t, captured.Query, "$fullPath:String!")
		assert.Equal(t, "group/fullpath-check", captured.Variables["fullPath"])
	})
}
