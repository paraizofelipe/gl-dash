package data

import (
	"sync"
	"testing"

	gh "github.com/cli/go-gh/v2/pkg/api"
	"github.com/stretchr/testify/require"
)

func TestResolveGithubClient_ConcurrentAccess(t *testing.T) {
	original := githubClient
	defer func() { githubClient = original }()
	githubClient = nil

	isolateGitHubAuthEnv(t)

	const n = 50
	var wg sync.WaitGroup
	results := make([]*gh.GraphQLClient, n)
	errs := make([]error, n)
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = resolveGithubClient()
		}(i)
	}
	close(start)
	wg.Wait()

	for i := range n {
		require.Equal(t, errs[0] == nil, errs[i] == nil)
		if errs[0] == nil {
			require.Same(t, results[0], results[i])
		}
	}
}
