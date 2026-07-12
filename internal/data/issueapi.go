package data

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"charm.land/log/v2"
	gh "github.com/cli/go-gh/v2/pkg/api"
	graphql "github.com/cli/shurcooL-graphql"
	"github.com/shurcooL/githubv4"

	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

type IssueData struct {
	Number int
	Title  string
	Body   string
	State  string
	Author struct {
		Login string
	}
	AuthorAssociation string
	UpdatedAt         time.Time
	CreatedAt         time.Time
	Url               string
	Repository        Repository
	Assignees         Assignees      `graphql:"assignees(first: 3)"`
	Comments          IssueComments  `graphql:"comments(last: 15)"`
	Reactions         IssueReactions `graphql:"reactions(first: 1)"`
	Labels            IssueLabels    `graphql:"labels(first: 20)"`
}

type IssueComments struct {
	Nodes      []IssueComment
	TotalCount int
}

type IssueComment struct {
	Author struct {
		Login string
	}
	Body      string
	UpdatedAt time.Time
}

type IssueReactions struct {
	TotalCount int
}

type Label struct {
	// Color is the label color as a hex string. GitLab returns it with a
	// leading "#" (e.g. "#d73a4a"); rendering normalizes the prefix so values
	// with or without "#" both work.
	Color       string
	Name        string
	Description string
}

type IssueLabels struct {
	Nodes []Label
}

func (data IssueData) GetAuthor(theme theme.Theme, showAuthorIcons bool) string {
	author := data.Author.Login
	if showAuthorIcons {
		author += fmt.Sprintf(" %s", GetAuthorRoleIcon(data.AuthorAssociation, theme))
	}
	return author
}

func (data IssueData) GetTitle() string {
	return data.Title
}

func (data IssueData) GetRepoNameWithOwner() string {
	return data.Repository.NameWithOwner
}

func (data IssueData) GetRepoNameAndOwner() (owner, repoName string) {
	return data.Repository.Owner.Login, data.Repository.Name
}

func (data IssueData) GetNumber() int {
	return data.Number
}

func (data IssueData) GetUrl() string {
	return data.Url
}

func (data IssueData) GetUpdatedAt() time.Time {
	return data.UpdatedAt
}

func (data IssueData) GetCreatedAt() time.Time {
	return data.CreatedAt
}

type issueNode struct {
	Iid         string
	Title       string
	Description string
	State       string
	Author      struct{ Username string }
	CreatedAt   time.Time
	UpdatedAt   time.Time
	WebUrl      string
	Labels      struct {
		Nodes []gitlabLabelNode
	} `graphql:"labels(first: 20)"`
	Assignees struct {
		Nodes []gitlabUserNode
	} `graphql:"assignees(first: 3)"`
}

func (n issueNode) toIssueData(projectPath string) IssueData {
	number, _ := strconv.Atoi(n.Iid)
	return IssueData{
		Number:     number,
		Title:      n.Title,
		Body:       n.Description,
		State:      n.State,
		Author:     struct{ Login string }{Login: n.Author.Username},
		CreatedAt:  n.CreatedAt,
		UpdatedAt:  n.UpdatedAt,
		Url:        n.WebUrl,
		Labels:     issueLabelsFromNodes(n.Labels.Nodes),
		Assignees:  assigneesFromNodes(n.Assignees.Nodes),
		Repository: repositoryFromProjectPath(projectPath),
	}
}

func FetchIssues(query string, limit int, pageInfo *PageInfo) (IssuesResponse, error) {
	c, err := resolveGraphQLClient()
	if err != nil {
		return IssuesResponse{}, err
	}

	currentUsername, err := CurrentLoginName()
	if err != nil {
		log.Warn("failed to resolve current username for @me", "err", err)
	}
	translated := TranslateSearchQuery(query, currentUsername)
	for _, u := range translated.Unsupported {
		log.Warn("search qualifier has no GitLab equivalent, ignoring", "qualifier", u)
	}
	if translated.NotAuthorUsername != "" {
		log.Warn(
			"search qualifier has no GitLab equivalent, ignoring",
			"qualifier", "-author:"+translated.NotAuthorUsername,
		)
	}

	var endCursor *string
	if pageInfo != nil {
		endCursor = &pageInfo.EndCursor
	}
	log.Debug("Fetching issues", "query", query, "limit", limit, "endCursor", endCursor)

	var nodes []issueNode
	var totalCount int
	var respPageInfo PageInfo

	labelName := optionalGraphQLStringList[graphql.String](translated.Labels)
	issueState := translated.State
	if issueState == "merged" {
		log.Warn(
			"search qualifier has no GitLab equivalent for issues, ignoring",
			"qualifier", "is:merged",
		)
		issueState = ""
	}
	state := optionalGraphQLValue[IssuableState](issueState)

	if translated.ProjectPath != "" {
		var queryResult struct {
			Project struct {
				Issues struct {
					Nodes    []issueNode
					Count    int
					PageInfo PageInfo
				} `graphql:"issues(first: $limit, after: $endCursor, sort: UPDATED_DESC, state: $state, authorUsername: $authorUsername, assigneeUsername: $assigneeUsername, labelName: $labelName)"`
			} `graphql:"project(fullPath: $fullPath)"`
		}
		variables := map[string]any{
			"fullPath":         graphql.ID(translated.ProjectPath),
			"limit":            graphql.Int(limit),
			"endCursor":        (*graphql.String)(endCursor),
			"state":            state,
			"authorUsername":   optionalGraphQLValue[graphql.String](translated.AuthorUsername),
			"assigneeUsername": optionalGraphQLValue[graphql.String](translated.AssigneeUsername),
			"labelName":        labelName,
		}
		err = c.QueryNamed(context.Background(), "ProjectIssues", &queryResult, variables)
		if err != nil {
			return IssuesResponse{}, err
		}
		nodes = queryResult.Project.Issues.Nodes
		totalCount = queryResult.Project.Issues.Count
		respPageInfo = queryResult.Project.Issues.PageInfo
	} else {
		authorUsername := translated.AuthorUsername
		if authorUsername == "" && translated.AssigneeUsername == "" {
			if currentUsername == "" {
				if err != nil {
					return IssuesResponse{}, fmt.Errorf(
						"cannot resolve current user for an unscoped issue search: %w", err,
					)
				}
				return IssuesResponse{}, fmt.Errorf(
					"cannot resolve current user for an unscoped issue search",
				)
			}
			authorUsername = currentUsername
		}

		var queryResult struct {
			Issues struct {
				Nodes    []issueNode
				Count    int
				PageInfo PageInfo
			} `graphql:"issues(first: $limit, after: $endCursor, sort: UPDATED_DESC, state: $state, authorUsername: $authorUsername, assigneeUsername: $assigneeUsername, labelName: $labelName)"`
		}
		variables := map[string]any{
			"limit":            graphql.Int(limit),
			"endCursor":        (*graphql.String)(endCursor),
			"state":            state,
			"authorUsername":   optionalGraphQLValue[graphql.String](authorUsername),
			"assigneeUsername": optionalGraphQLValue[graphql.String](translated.AssigneeUsername),
			"labelName":        labelName,
		}
		err = c.QueryNamed(context.Background(), "MyIssues", &queryResult, variables)
		if err != nil {
			return IssuesResponse{}, err
		}
		nodes = queryResult.Issues.Nodes
		totalCount = queryResult.Issues.Count
		respPageInfo = queryResult.Issues.PageInfo
	}

	log.Info("Successfully fetched issues", "query", query, "count", totalCount)

	issues := make([]IssueData, 0, len(nodes))
	for _, n := range nodes {
		issues = append(issues, n.toIssueData(translated.ProjectPath))
	}

	return IssuesResponse{
		Issues:     issues,
		TotalCount: totalCount,
		PageInfo:   respPageInfo,
	}, nil
}

type IssuesResponse struct {
	Issues     []IssueData
	TotalCount int
	PageInfo   PageInfo
}

// FetchIssue fetches a single issue by its GitHub URL
func FetchIssue(issueUrl string) (IssueData, error) {
	var err error
	if githubClient == nil {
		githubClient, err = gh.DefaultGraphQLClient()
		if err != nil {
			return IssueData{}, err
		}
	}

	var queryResult struct {
		Resource struct {
			Issue IssueData `graphql:"... on Issue"`
		} `graphql:"resource(url: $url)"`
	}
	parsedUrl, err := url.Parse(issueUrl)
	if err != nil {
		return IssueData{}, err
	}
	variables := map[string]any{
		"url": githubv4.URI{URL: parsedUrl},
	}
	log.Debug("Fetching Issue", "url", issueUrl)
	err = githubClient.Query("FetchIssue", &queryResult, variables)
	if err != nil {
		return IssueData{}, err
	}
	log.Info("Successfully fetched Issue", "url", issueUrl)

	return queryResult.Resource.Issue, nil
}
