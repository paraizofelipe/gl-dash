package data

import (
	"context"
	"net/http"
	"os"
	"sync"
	"time"

	"charm.land/log/v2"
	graphql "github.com/cli/shurcooL-graphql"
)

const githubAPIHost = "api.github.com"

type VersionResponse struct {
	Repository struct {
		LatestRelease struct {
			TagName string
		}
	} `graphql:"repository(owner: $owner, name: $name)"`
}

var (
	githubClient   *graphql.Client
	githubClientMu sync.Mutex
)

type githubTokenTransport struct {
	token string
}

func (t *githubTokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.token == "" || req.URL.Hostname() != githubAPIHost {
		return http.DefaultTransport.RoundTrip(req)
	}
	cloned := req.Clone(req.Context())
	cloned.Header.Set("Authorization", "bearer "+t.token)
	return http.DefaultTransport.RoundTrip(cloned)
}

func resolveGithubClient() (*graphql.Client, error) {
	githubClientMu.Lock()
	defer githubClientMu.Unlock()
	if githubClient != nil {
		return githubClient, nil
	}
	hc := &http.Client{
		Timeout:   10 * time.Second,
		Transport: &githubTokenTransport{token: os.Getenv("GITHUB_TOKEN")},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	githubClient = graphql.NewClient("https://"+githubAPIHost+"/graphql", hc)
	return githubClient, nil
}

func FetchLatestVersion() (VersionResponse, error) {
	var queryResult VersionResponse
	c, err := resolveGithubClient()
	if err != nil {
		return VersionResponse{}, err
	}

	variables := map[string]any{
		"owner": graphql.String("dlvhdr"),
		"name":  graphql.String("gh-dash"),
	}

	log.Debug("Fetching latest version")
	err = c.QueryNamed(context.Background(), "LatestVersion", &queryResult, variables)
	if err != nil {
		log.Debug("failed to fetch latest version from upstream", "err", err)
		return VersionResponse{}, err
	}
	log.Info("Successfully fetched latest version", "version",
		queryResult.Repository.LatestRelease.TagName)

	return queryResult, nil
}

type SponsorsResponse struct {
	User struct {
		Sponsors struct {
			Nodes []struct {
				Typename string `graphql:"__typename"`
				User     struct {
					Login string
					Url   string
				} `graphql:"... on User"`
				Organization struct {
					Name string
					Url  string
				} `graphql:"... on Organization"`
			}
		} `graphql:"sponsors(first: 100)"`
	} `graphql:"user(login: $login)"`
}

func FetchSponsors() (SponsorsResponse, error) {
	var queryResult SponsorsResponse
	c, err := resolveGithubClient()
	if err != nil {
		return SponsorsResponse{}, err
	}

	variables := map[string]any{
		"login": graphql.String("dlvhdr"),
	}

	log.Debug("Fetching sponsors")
	err = c.QueryNamed(context.Background(), "Sponsors", &queryResult, variables)
	if err != nil {
		log.Debug("failed to fetch sponsors from upstream", "err", err)
		return SponsorsResponse{}, err
	}
	log.Info("Successfully fetched sponsors")

	return queryResult, nil
}
