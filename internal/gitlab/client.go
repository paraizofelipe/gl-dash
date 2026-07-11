package gitlab

import (
	"net/http"

	graphql "github.com/cli/shurcooL-graphql"
	gitlab "gitlab.com/gitlab-org/api/client-go"
)

var (
	restClient    *gitlab.Client
	graphqlClient *graphql.Client
)

func SetClients(rest *gitlab.Client, gql *graphql.Client) {
	restClient, graphqlClient = rest, gql
}

func RESTClient() (*gitlab.Client, error) {
	if restClient != nil {
		return restClient, nil
	}
	auth, err := LoadAuthConfig()
	if err != nil {
		return nil, err
	}
	c, err := gitlab.NewClient(auth.Token, gitlab.WithBaseURL(baseURL(auth)+"/api/v4"))
	if err != nil {
		return nil, err
	}
	restClient = c
	return restClient, nil
}

func baseURL(auth AuthConfig) string {
	return auth.APIProtocol + "://" + auth.Host
}

type tokenTransport struct{ token string }

func (t *tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("PRIVATE-TOKEN", t.token)
	return http.DefaultTransport.RoundTrip(req)
}

func GraphQLClient() (*graphql.Client, error) {
	if graphqlClient != nil {
		return graphqlClient, nil
	}
	auth, err := LoadAuthConfig()
	if err != nil {
		return nil, err
	}
	hc := &http.Client{Transport: &tokenTransport{token: auth.Token}}
	graphqlClient = graphql.NewClient(baseURL(auth)+gitlab.GraphQLAPIEndpoint, hc)
	return graphqlClient, nil
}
