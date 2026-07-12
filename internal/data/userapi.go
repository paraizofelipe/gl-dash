package data

import (
	"context"
	"sync"

	"charm.land/log/v2"
	graphql "github.com/cli/shurcooL-graphql"
)

var (
	repoUserCache = make(map[string][]User)
	userCacheMu   sync.RWMutex
)

type User struct {
	Login string `json:"login"`
	Name  string `json:"name"`
}

type ProjectMembersResponse struct {
	Project struct {
		ProjectMembers struct {
			Nodes []struct {
				User struct {
					Username string
					Name     string
				} `graphql:"user"`
			}
		} `graphql:"projectMembers(first: $limit)"`
	} `graphql:"project(fullPath: $fullPath)"`
}

func CachedRepoUsers(repoNameWithOwner string) ([]User, bool) {
	userCacheMu.RLock()
	defer userCacheMu.RUnlock()
	users, ok := repoUserCache[repoNameWithOwner]
	return users, ok
}

// FetchRepoUsers fetches a GitLab project's members via projectMembers.
// Unlike the previous GitHub-based mentionableUsers, this only returns users
// with an explicit membership/role on the project — commenters or issue/MR
// authors without membership will not appear in the results.
func FetchRepoUsers(owner, repoName string) ([]User, error) {
	// Check cache first
	repo := owner + "/" + repoName
	if cachedUsers, ok := CachedRepoUsers(repo); ok {
		log.Debug(
			"Using cached repo users",
			"owner",
			owner,
			"repoName",
			repoName,
			"len(cachedUsers)",
			len(cachedUsers),
		)
		return cachedUsers, nil
	}

	log.Debug("Fetching repo users", "owner", owner, "repoName", repoName)

	client, err := resolveGraphQLClient()
	if err != nil {
		return nil, err
	}

	var result ProjectMembersResponse
	variables := map[string]any{
		"fullPath": graphql.ID(repo),
		"limit":    graphql.Int(100),
	}

	err = client.QueryNamed(context.Background(), "GetProjectMembers", &result, variables)
	if err != nil {
		return nil, err
	}

	users := make([]User, 0)
	for _, node := range result.Project.ProjectMembers.Nodes {
		if node.User.Username != "" {
			users = append(users, User{Login: node.User.Username, Name: node.User.Name})
		}
	}

	userCacheMu.Lock()
	defer userCacheMu.Unlock()

	repoUserCache[repo] = users
	log.Debug(
		"Successfully fetched repo users",
		"owner",
		owner,
		"repoName",
		repoName,
		"len",
		len(users),
	)
	return users, nil
}

func ClearUserCache() {
	userCacheMu.Lock()
	defer userCacheMu.Unlock()
	repoUserCache = make(map[string][]User)
}

func ClearRepoUserCache(repoNameWithOwner string) {
	userCacheMu.Lock()
	defer userCacheMu.Unlock()
	delete(repoUserCache, repoNameWithOwner)
}

func UserLogins(users []User) []string {
	logins := make([]string, len(users))
	for i, user := range users {
		logins[i] = user.Login
	}
	return logins
}
