package data

import (
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFetchRepoLabels_CacheMissFetchesFromAPIAndPopulatesCache(t *testing.T) {
	defer SetRESTClient(nil)
	repo := "acme/cache-miss"
	t.Cleanup(func() { ClearRepoLabelCache(repo) })

	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Contains(t, r.URL.Path, "/labels")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(
			`[{"name":"bug","color":"#d73a4a","description":"Something is wrong"},` +
				`{"name":"feature","color":"#00ff00","description":"New capability"}]`,
		))
	})
	SetRESTClient(mockClient)

	labels, err := FetchRepoLabels(repo)

	require.NoError(t, err)
	require.Equal(t, []Label{
		{Name: "bug", Color: "#d73a4a", Description: "Something is wrong"},
		{Name: "feature", Color: "#00ff00", Description: "New capability"},
	}, labels)

	cached, ok := CachedRepoLabels(repo)
	require.True(t, ok)
	require.Equal(t, labels, cached)
	require.Equal(t, []string{"bug", "feature"}, LabelNames(labels))
}

func TestFetchRepoLabels_CacheHitDoesNotCallAPIAgain(t *testing.T) {
	defer SetRESTClient(nil)
	repo := "acme/cache-hit"
	t.Cleanup(func() { ClearRepoLabelCache(repo) })

	var requestCount atomic.Int32
	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"name":"bug","color":"#d73a4a","description":""}]`))
	})
	SetRESTClient(mockClient)

	first, err := FetchRepoLabels(repo)
	require.NoError(t, err)

	second, err := FetchRepoLabels(repo)
	require.NoError(t, err)

	require.Equal(t, int32(1), requestCount.Load())
	require.Equal(t, first, second)
}

func TestFetchRepoLabels_PaginatesAcrossMultiplePages(t *testing.T) {
	defer SetRESTClient(nil)
	repo := "acme/paginated"
	t.Cleanup(func() { ClearRepoLabelCache(repo) })

	var pagesRequested []string
	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		pagesRequested = append(pagesRequested, page)
		w.Header().Set("Content-Type", "application/json")
		if page == "2" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"name":"second-page","color":"#222222","description":""}]`))
			return
		}
		w.Header().Set("X-Next-Page", "2")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"name":"first-page","color":"#111111","description":""}]`))
	})
	SetRESTClient(mockClient)

	labels, err := FetchRepoLabels(repo)

	require.NoError(t, err)
	require.Equal(t, []string{"1", "2"}, pagesRequested)
	require.Equal(t, []Label{
		{Name: "first-page", Color: "#111111"},
		{Name: "second-page", Color: "#222222"},
	}, labels)
}

func TestFetchRepoLabels_APIErrorDoesNotPopulateCache(t *testing.T) {
	defer SetRESTClient(nil)
	repo := "acme/api-error"
	t.Cleanup(func() { ClearRepoLabelCache(repo) })

	mockClient := newMockRESTClient(t, staticJSONHandler(http.StatusInternalServerError, ""))
	SetRESTClient(mockClient)

	labels, err := FetchRepoLabels(repo)

	require.Error(t, err)
	require.Nil(t, labels)

	_, ok := CachedRepoLabels(repo)
	require.False(t, ok)
}

func TestFetchRepoLabels_FiltersLabelsWithEmptyName(t *testing.T) {
	defer SetRESTClient(nil)
	repo := "acme/empty-name"
	t.Cleanup(func() { ClearRepoLabelCache(repo) })

	mockClient := newMockRESTClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(
			`[{"name":"","color":"#fff","description":"blank"},` +
				`{"name":"valid","color":"#000","description":"kept"}]`,
		))
	})
	SetRESTClient(mockClient)

	labels, err := FetchRepoLabels(repo)

	require.NoError(t, err)
	require.Equal(t, []Label{{Name: "valid", Color: "#000", Description: "kept"}}, labels)
}
