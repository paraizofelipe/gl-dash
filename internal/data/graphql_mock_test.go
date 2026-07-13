package data

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"charm.land/log/v2"
	graphql "github.com/cli/shurcooL-graphql"
)

type graphQLRequestBody struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

func newMockGraphQLServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func newMockGraphQLClient(t *testing.T, handler http.HandlerFunc) *graphql.Client {
	t.Helper()
	server := newMockGraphQLServer(t, handler)
	return graphql.NewClient(server.URL+"/api/graphql", server.Client())
}

func decodeGraphQLRequestBody(t *testing.T, r *http.Request) graphQLRequestBody {
	t.Helper()
	var body graphQLRequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Errorf("failed to decode graphql request body: %v", err)
	}
	return body
}

func staticJSONHandler(statusCode int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(body))
	}
}

func captureLogOutput(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
	})
	return &buf
}

func usernameAuthor(username string) struct{ Username string } {
	return struct{ Username string }{Username: username}
}

func discussionWithNotes(notes ...gitlabNoteNode) gitlabDiscussionNode {
	var d gitlabDiscussionNode
	d.Notes.Nodes = notes
	return d
}
