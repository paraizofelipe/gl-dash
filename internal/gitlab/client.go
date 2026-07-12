package gitlab

import (
	"net/http"
	"sync"

	graphql "github.com/cli/shurcooL-graphql"
	gitlab "gitlab.com/gitlab-org/api/client-go"
)

var (
	restClient      *gitlab.Client
	restClientMu    sync.Mutex
	graphqlClient   *graphql.Client
	graphqlClientMu sync.Mutex
)

func SetClients(rest *gitlab.Client, gql *graphql.Client) {
	restClientMu.Lock()
	restClient = rest
	restClientMu.Unlock()

	graphqlClientMu.Lock()
	graphqlClient = gql
	graphqlClientMu.Unlock()
}

func RESTClient() (*gitlab.Client, error) {
	restClientMu.Lock()
	defer restClientMu.Unlock()
	if restClient != nil {
		return restClient, nil
	}
	auth, err := LoadAuthConfig()
	if err != nil {
		return nil, err
	}
	newClient := gitlab.NewClient
	if auth.IsJobToken {
		newClient = gitlab.NewJobClient
	}
	c, err := newClient(auth.Token, gitlab.WithBaseURL(baseURL(auth)+"/api/v4"))
	if err != nil {
		return nil, err
	}
	restClient = c
	return restClient, nil
}

func baseURL(auth AuthConfig) string {
	return auth.APIProtocol + "://" + auth.Host
}

type tokenTransport struct {
	token      string
	isJobToken bool
}

func (t *tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	header := gitlab.AccessTokenHeaderName
	if t.isJobToken {
		header = gitlab.JobTokenHeaderName
	}
	req.Header.Set(header, t.token)
	return http.DefaultTransport.RoundTrip(req)
}

func GraphQLClient() (*graphql.Client, error) {
	graphqlClientMu.Lock()
	defer graphqlClientMu.Unlock()
	if graphqlClient != nil {
		return graphqlClient, nil
	}
	auth, err := LoadAuthConfig()
	if err != nil {
		return nil, err
	}
	hc := &http.Client{Transport: &tokenTransport{token: auth.Token, isJobToken: auth.IsJobToken}}
	graphqlClient = graphql.NewClient(baseURL(auth)+gitlab.GraphQLAPIEndpoint, hc)
	return graphqlClient, nil
}
