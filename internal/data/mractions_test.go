package data

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAddMergeRequestReviewers_MergesWithExistingAndSendsReviewerIDs(t *testing.T) {
	defer SetRESTClient(nil)

	var captured map[string]any
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/users"):
			// resolveUserIDs: username -> id
			_, _ = w.Write([]byte(`[{"id":42,"username":"jdoe"}]`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/merge_requests/5"):
			// current reviewers on the MR
			_, _ = w.Write([]byte(`{"iid":5,"reviewers":[{"id":10,"username":"existing"}]}`))
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/merge_requests/5"):
			_ = json.NewDecoder(r.Body).Decode(&captured)
			_, _ = w.Write([]byte(`{"iid":5}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
	SetRESTClient(newMockRESTClient(t, handler))

	err := AddMergeRequestReviewers("group/proj", 5, []string{"jdoe"})
	require.NoError(t, err)

	require.NotNil(t, captured)
	ids, ok := captured["reviewer_ids"].([]any)
	require.Truef(t, ok, "reviewer_ids should be present, got: %v", captured)
	// The existing reviewer (10) is preserved and the new one (42) is added, sorted.
	require.Equal(t, []any{float64(10), float64(42)}, ids)
}
