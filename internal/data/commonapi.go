package data

import (
	"sync"

	"charm.land/log/v2"
	gh "github.com/cli/go-gh/v2/pkg/api"
	graphql "github.com/cli/shurcooL-graphql"
)

type VersionResponse struct {
	Repository struct {
		LatestRelease struct {
			TagName string
		}
	} `graphql:"repository(owner: $owner, name: $name)"`
}

var (
	githubClient   *gh.GraphQLClient
	githubClientMu sync.Mutex
)

func resolveGithubClient() (*gh.GraphQLClient, error) {
	githubClientMu.Lock()
	defer githubClientMu.Unlock()
	if githubClient != nil {
		return githubClient, nil
	}
	c, err := gh.DefaultGraphQLClient()
	if err != nil {
		return nil, err
	}
	githubClient = c
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
	err = c.Query("LatestVersion", &queryResult, variables)
	if err != nil {
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
	err = c.Query("Sponsors", &queryResult, variables)
	if err != nil {
		return SponsorsResponse{}, err
	}
	log.Info("Successfully fetched sponsors")

	return queryResult, nil
}
