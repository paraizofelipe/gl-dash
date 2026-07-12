package data

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"charm.land/log/v2"
	graphql "github.com/cli/shurcooL-graphql"
	checks "github.com/dlvhdr/x/gh-checks"
	gitlabapi "gitlab.com/gitlab-org/api/client-go"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/gitlab"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

type SuggestedReviewer struct {
	IsAuthor    bool
	IsCommenter bool
	Reviewer    struct {
		Login string
	}
}

type EnrichedPullRequestData struct {
	Url     string
	Number  int
	Title   string
	Body    string
	State   string
	IsDraft bool
	Author  struct {
		Login string
	}
	AuthorAssociation string
	UpdatedAt         time.Time
	CreatedAt         time.Time
	Mergeable         string
	ReviewDecision    string
	Additions         int
	Deletions         int
	HeadRefName       string
	BaseRefName       string
	HeadRepository    struct {
		Name string
	}
	HeadRef struct {
		Name string
	}
	Labels             PRLabels  `graphql:"labels(first: 6)"`
	Assignees          Assignees `graphql:"assignees(first: 3)"`
	Repository         Repository
	Commits            LastCommitWithStatusChecks `graphql:"commits(last: 1)"`
	AllCommits         AllCommits                 `graphql:"allCommits: commits(last: 100)"`
	Comments           CommentsWithBody           `graphql:"comments(last: 50, orderBy: { field: UPDATED_AT, direction: DESC })"`
	ReviewThreads      ReviewThreadsWithComments  `graphql:"reviewThreads(last: 50)"`
	ReviewRequests     ReviewRequests             `graphql:"reviewRequests(last: 100)"`
	Reviews            Reviews                    `graphql:"reviews(last: 100)"`
	SuggestedReviewers []SuggestedReviewer
	Files              ChangedFiles `graphql:"files(first: 20)"`
	Pipeline           MergeRequestPipeline
}

type PullRequestData struct {
	Number int
	Title  string
	Author struct {
		Login string
	}
	AuthorAssociation string
	UpdatedAt         time.Time
	CreatedAt         time.Time
	Url               string
	State             string
	Mergeable         string
	ReviewDecision    string
	Additions         int
	Deletions         int
	HeadRefName       string
	BaseRefName       string
	HeadRepository    struct {
		Name string
	}
	HeadRef struct {
		Name string
	}
	Repository       Repository
	Assignees        Assignees            `graphql:"assignees(first: 3)"`
	Comments         Comments             `graphql:"comments"`
	ReviewThreads    ReviewThreads        `graphql:"reviewThreads"`
	Reviews          ReviewsNumber        `graphql:"reviews"`
	ReviewRequests   ReviewRequestsNumber `graphql:"reviewRequests"`
	IsDraft          bool
	IsInMergeQueue   bool
	Commits          LastCommitStatus `graphql:"commits(last: 1)"`
	Labels           PRLabels         `graphql:"labels(first: 6)"`
	MergeStateStatus MergeStateStatus `graphql:"mergeStateStatus"`
}

type LastCommitStatus struct {
	Nodes []struct {
		Commit struct {
			StatusCheckRollup struct {
				State graphql.String
			}
		}
	}
}

type CheckRun struct {
	Name       graphql.String
	Status     graphql.String
	Conclusion checks.CheckRunState
	CheckSuite struct {
		Creator struct {
			Login graphql.String
		}
		WorkflowRun struct {
			Workflow struct {
				Name graphql.String
			}
		}
	}
}

type StatusContext struct {
	Context graphql.String
	State   graphql.String
	Creator struct {
		Login graphql.String
	}
}

type CheckSuiteNode struct {
	Status     graphql.String
	Conclusion graphql.String

	App struct {
		Name graphql.String
	}

	WorkflowRun struct {
		Workflow struct {
			Name graphql.String
		}
	}
}

type CheckSuites struct {
	TotalCount graphql.Int
	Nodes      []CheckSuiteNode
}

type StatusCheckRollupStats struct {
	State    checks.CommitState
	Contexts struct {
		TotalCount                 graphql.Int
		CheckRunCount              graphql.Int
		CheckRunCountsByState      []ContextCountByState
		StatusContextCount         graphql.Int
		StatusContextCountsByState []ContextCountByState
	} `graphql:"contexts(last: 1)"`
}

type AllCommits struct {
	Nodes []struct {
		Commit struct {
			AbbreviatedOid  string
			CommittedDate   time.Time
			MessageHeadline string
			Author          struct {
				Name string
				User struct {
					Login string
				}
			}
			StatusCheckRollup StatusCheckRollupStats
		}
	}
}

type LastCommitWithStatusChecks struct {
	Nodes []struct {
		Commit struct {
			Deployments struct {
				Nodes []struct {
					Task        graphql.String
					Description graphql.String
				}
			} `graphql:"deployments(last: 10)"`
			CommitUrl         graphql.String
			StatusCheckRollup struct {
				State    graphql.String
				Contexts struct {
					TotalCount                 graphql.Int
					CheckRunCount              graphql.Int
					CheckRunCountsByState      []ContextCountByState
					StatusContextCount         graphql.Int
					StatusContextCountsByState []ContextCountByState
					Nodes                      []struct {
						Typename      graphql.String `graphql:"__typename"`
						CheckRun      CheckRun       `graphql:"... on CheckRun"`
						StatusContext StatusContext  `graphql:"... on StatusContext"`
					}
				} `graphql:"contexts(last: 100)"`
			}
			// CheckSuites are fetched separately from StatusCheckRollup because
			// workflows awaiting approval (conclusion ACTION_REQUIRED) and workflows
			// still queued have no CheckRun objects yet, so they don’t appear in
			// StatusCheckRollup.contexts.
			CheckSuites CheckSuites `graphql:"checkSuites(last: 20)"`
		}
	}
	TotalCount int
}

// GitLab has no server-side "counts by state" for jobs (unlike GitHub's
// StatusCheckRollup.Contexts.*CountsByState) — PipelineJob/MergeRequestPipeline
// and CountJobsByState replace CheckRun/StatusContext/ContextCountByState for
// the GitLab side. The GitHub-specific types above are kept untouched until
// their last production consumer is migrated off them.
type PipelineJob struct {
	ID           int64
	Name         string
	Stage        string
	Status       PipelineStatus
	WebURL       string
	AllowFailure bool
}

type MergeRequestPipeline struct {
	ID     int64
	Status PipelineStatus
	WebURL string
	Jobs   []PipelineJob
}

type JobCountByState struct {
	State PipelineStatus
	Count int
}

// CountJobsByState aggregates jobs by state client-side. Preserves first-seen
// order so UI rendering is deterministic across calls with the same input.
func CountJobsByState(jobs []PipelineJob) []JobCountByState {
	counts := make(map[PipelineStatus]int)
	order := make([]PipelineStatus, 0, len(jobs))
	for _, j := range jobs {
		if _, seen := counts[j.Status]; !seen {
			order = append(order, j.Status)
		}
		counts[j.Status]++
	}
	result := make([]JobCountByState, 0, len(order))
	for _, s := range order {
		result = append(result, JobCountByState{State: s, Count: counts[s]})
	}
	return result
}

type CommentsWithBody struct {
	TotalCount graphql.Int
	Nodes      []Comment
}

type ContextCountByState = struct {
	Count graphql.Int
	State checks.CheckRunState
}

type Commits struct {
	Nodes []struct {
		Commit struct {
			Deployments struct {
				Nodes []struct {
					Task        graphql.String
					Description graphql.String
				}
			} `graphql:"deployments(last: 10)"`
			CommitUrl         graphql.String
			StatusCheckRollup struct {
				State graphql.String
			}
		}
	}
	TotalCount int
}

type Comment struct {
	Author struct {
		Login string
	}
	Body      string
	UpdatedAt time.Time
}

type ReviewComment struct {
	Author struct {
		Login string
	}
	Body      string
	UpdatedAt time.Time
	StartLine int
	Line      int
}

type ReviewComments struct {
	Nodes      []ReviewComment
	TotalCount int
}

type Comments struct {
	TotalCount int
}

type ReviewThreads struct {
	TotalCount int
}

type Review struct {
	Author struct {
		Login string
	}
	Body      string
	State     string
	UpdatedAt time.Time
}

type ReviewsNumber struct {
	TotalCount int
}

type Reviews struct {
	TotalCount int
	Nodes      []Review
}

type ReviewThreadsWithComments struct {
	Nodes []struct {
		Id           string
		IsOutdated   bool
		OriginalLine int
		StartLine    int
		Line         int
		Path         string
		Comments     ReviewComments `graphql:"comments(first: 20)"`
	}
}

type ChangedFile struct {
	Additions  int
	Deletions  int
	Path       string
	ChangeType string
}

type ChangedFiles struct {
	TotalCount int
	Nodes      []ChangedFile
}

type RequestedReviewerUser struct {
	Login string `graphql:"login"`
}

type RequestedReviewerTeam struct {
	Slug string `graphql:"slug"`
	Name string `graphql:"name"`
}

type RequestedReviewerBot struct {
	Login string `graphql:"login"`
}

type RequestedReviewerMannequin struct {
	Login string `graphql:"login"`
}

type ReviewRequestNode struct {
	AsCodeOwner       bool `graphql:"asCodeOwner"`
	RequestedReviewer struct {
		User      RequestedReviewerUser      `graphql:"... on User"`
		Team      RequestedReviewerTeam      `graphql:"... on Team"`
		Bot       RequestedReviewerBot       `graphql:"... on Bot"`
		Mannequin RequestedReviewerMannequin `graphql:"... on Mannequin"`
	} `graphql:"requestedReviewer"`
}

type ReviewRequestsNumber struct {
	TotalCount int
}

type ReviewRequests struct {
	TotalCount int
	Nodes      []ReviewRequestNode
}

func (r ReviewRequestNode) GetReviewerDisplayName() string {
	if r.RequestedReviewer.User.Login != "" {
		return r.RequestedReviewer.User.Login
	}
	if r.RequestedReviewer.Team.Slug != "" {
		return r.RequestedReviewer.Team.Slug
	}
	if r.RequestedReviewer.Bot.Login != "" {
		return r.RequestedReviewer.Bot.Login
	}
	if r.RequestedReviewer.Mannequin.Login != "" {
		return r.RequestedReviewer.Mannequin.Login
	}
	return ""
}

func (r ReviewRequestNode) GetReviewerType() string {
	if r.RequestedReviewer.User.Login != "" {
		return "User"
	}
	if r.RequestedReviewer.Team.Slug != "" {
		return "Team"
	}
	if r.RequestedReviewer.Bot.Login != "" {
		return "Bot"
	}
	if r.RequestedReviewer.Mannequin.Login != "" {
		return "Mannequin"
	}
	return ""
}

func (r ReviewRequestNode) IsTeam() bool {
	return r.RequestedReviewer.Team.Slug != ""
}

type PRLabels struct {
	Nodes []Label
}

type MergeStateStatus string

type PageInfo struct {
	HasNextPage bool
	StartCursor string
	EndCursor   string
}

func (data PullRequestData) GetAuthor(theme theme.Theme, showAuthorIcon bool) string {
	author := data.Author.Login
	if showAuthorIcon {
		author += fmt.Sprintf(" %s", GetAuthorRoleIcon(data.AuthorAssociation, theme))
	}
	return author
}

func (data PullRequestData) GetTitle() string {
	return data.Title
}

func (data PullRequestData) GetRepoNameWithOwner() string {
	return data.Repository.NameWithOwner
}

func (data PullRequestData) GetRepoNameAndOwner() (owner, repoName string) {
	return data.Repository.Owner.Login, data.Repository.Name
}

func (data PullRequestData) GetNumber() int {
	return data.Number
}

func (data PullRequestData) GetUrl() string {
	return data.Url
}

func (data PullRequestData) GetUpdatedAt() time.Time {
	return data.UpdatedAt
}

func (data PullRequestData) GetCreatedAt() time.Time {
	return data.CreatedAt
}

// ToPullRequestData converts EnrichedPullRequestData to PullRequestData
// This is useful when we fetch a single PR and need basic PR fields
func (e EnrichedPullRequestData) ToPullRequestData() PullRequestData {
	return PullRequestData{
		Number:            e.Number,
		Title:             e.Title,
		Author:            e.Author,
		AuthorAssociation: e.AuthorAssociation,
		UpdatedAt:         e.UpdatedAt,
		CreatedAt:         e.CreatedAt,
		Url:               e.Url,
		State:             e.State,
		Mergeable:         e.Mergeable,
		ReviewDecision:    e.ReviewDecision,
		Additions:         e.Additions,
		Deletions:         e.Deletions,
		HeadRefName:       e.HeadRefName,
		BaseRefName:       e.BaseRefName,
		HeadRepository:    e.HeadRepository,
		HeadRef:           e.HeadRef,
		Repository:        e.Repository,
		Assignees:         e.Assignees,
		IsDraft:           e.IsDraft,
		Labels:            e.Labels,
		// Note: Comments, ReviewThreads, Reviews, ReviewRequests, Commits
		// have different types in EnrichedPullRequestData vs PullRequestData
		// We leave them as zero values since the enriched data will be used instead
	}
}

type PullRequestsResponse struct {
	Prs        []PullRequestData
	TotalCount int
	PageInfo   PageInfo
}

var (
	client       *graphql.Client
	cachedClient *graphql.Client
)

func SetClient(c *graphql.Client) {
	client = c
	cachedClient = c
}

// ClearEnrichmentCache clears the cached GraphQL client used for fetching
// enriched PR/Issue data. Call this when refreshing to ensure fresh data.
func ClearEnrichmentCache() {
	cachedClient = nil
}

// IsEnrichmentCacheCleared returns true if the enrichment cache is cleared.
// This is primarily for testing purposes.
func IsEnrichmentCacheCleared() bool {
	return cachedClient == nil
}

func resolveGraphQLClient() (*graphql.Client, error) {
	if client != nil {
		return client, nil
	}
	if config.IsFeatureEnabled(config.FF_MOCK_DATA) {
		log.Info("using mock data", "server", "https://localhost:3000")
		if transport, ok := http.DefaultTransport.(*http.Transport); ok {
			transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		}
		client = graphql.NewClient(
			"https://localhost:3000/api/graphql",
			&http.Client{Transport: http.DefaultTransport},
		)
		return client, nil
	}
	c, err := gitlab.GraphQLClient()
	if err != nil {
		return nil, err
	}
	client = c
	return client, nil
}

var gitlabRESTClient *gitlabapi.Client

// SetRESTClient overrides the cached REST client used to fetch pipeline/job
// data. Used by tests.
func SetRESTClient(c *gitlabapi.Client) {
	gitlabRESTClient = c
}

func resolveRESTClient() (*gitlabapi.Client, error) {
	if gitlabRESTClient != nil {
		return gitlabRESTClient, nil
	}
	c, err := gitlab.RESTClient()
	if err != nil {
		return nil, err
	}
	gitlabRESTClient = c
	return gitlabRESTClient, nil
}

// FindPipelineForMR returns the merge request's most recent pipeline (the
// GitLab REST API has no single "head pipeline" endpoint for merge requests,
// so this lists all pipelines tied to the MR and picks the one with the
// highest ID). Returns a zero-value MergeRequestPipeline with a nil error
// when the merge request has no pipeline yet — that's a legitimate state,
// not a failure. Jobs are left empty; call ListPipelineJobs separately.
func FindPipelineForMR(projectPath string, mrIID int) (MergeRequestPipeline, error) {
	c, err := resolveRESTClient()
	if err != nil {
		return MergeRequestPipeline{}, err
	}
	pipelines, _, err := c.MergeRequests.ListMergeRequestPipelines(projectPath, int64(mrIID))
	if err != nil {
		return MergeRequestPipeline{}, err
	}
	if len(pipelines) == 0 {
		return MergeRequestPipeline{}, nil
	}
	latest := pipelines[0]
	for _, p := range pipelines[1:] {
		if p.ID > latest.ID {
			latest = p
		}
	}
	return MergeRequestPipeline{
		ID:     latest.ID,
		Status: PipelineStatus(strings.ToLower(latest.Status)),
		WebURL: latest.WebURL,
	}, nil
}

// ListPipelineJobs lists the jobs of a pipeline, optionally filtered
// server-side by scope (e.g. "manual"). An empty scope lists all jobs.
func ListPipelineJobs(projectPath string, pipelineID int64, scope string) ([]PipelineJob, error) {
	c, err := resolveRESTClient()
	if err != nil {
		return nil, err
	}
	opts := &gitlabapi.ListJobsOptions{}
	if scope != "" {
		opts.Scope = &[]gitlabapi.BuildStateValue{gitlabapi.BuildStateValue(scope)}
	}
	jobs, _, err := c.Jobs.ListPipelineJobs(projectPath, pipelineID, opts)
	if err != nil {
		return nil, err
	}
	result := make([]PipelineJob, 0, len(jobs))
	for _, j := range jobs {
		result = append(result, PipelineJob{
			ID:           j.ID,
			Name:         j.Name,
			Stage:        j.Stage,
			Status:       PipelineStatus(strings.ToLower(j.Status)),
			WebURL:       j.WebURL,
			AllowFailure: j.AllowFailure,
		})
	}
	return result, nil
}

// PlayJob triggers a manual job to run (POST .../jobs/:job_id/play).
func PlayJob(projectPath string, jobID int64) error {
	c, err := resolveRESTClient()
	if err != nil {
		return err
	}
	_, _, err = c.Jobs.PlayJob(projectPath, jobID, nil)
	return err
}

type gitlabLabelNode struct {
	Title       string
	Color       string
	Description string
}

type gitlabUserNode struct {
	Username string
}

func convertLabelNodes(nodes []gitlabLabelNode) []Label {
	converted := make([]Label, len(nodes))
	for i, n := range nodes {
		converted[i] = Label{Name: n.Title, Color: n.Color, Description: n.Description}
	}
	return converted
}

func labelsFromNodes(nodes []gitlabLabelNode) PRLabels {
	return PRLabels{Nodes: convertLabelNodes(nodes)}
}

func issueLabelsFromNodes(nodes []gitlabLabelNode) IssueLabels {
	return IssueLabels{Nodes: convertLabelNodes(nodes)}
}

func assigneesFromNodes(nodes []gitlabUserNode) Assignees {
	converted := make([]Assignee, len(nodes))
	for i, n := range nodes {
		converted[i] = Assignee{Login: n.Username}
	}
	return Assignees{Nodes: converted}
}

func optionalGraphQLValue[T ~string](s string) *T {
	if s == "" {
		return nil
	}
	v := T(s)
	return &v
}

func optionalGraphQLStringList[T ~string](values []string) *[]T {
	if len(values) == 0 {
		return nil
	}
	list := make([]T, len(values))
	for i, v := range values {
		list[i] = T(v)
	}
	return &list
}

type IssuableState string

type MergeRequestState string

func repositoryFromProjectPath(projectPath string) Repository {
	if projectPath == "" {
		return Repository{}
	}
	idx := strings.LastIndex(projectPath, "/")
	if idx < 0 {
		return Repository{Name: projectPath, NameWithOwner: projectPath}
	}
	return Repository{
		Name:          projectPath[idx+1:],
		Owner:         Owner{Login: projectPath[:idx]},
		NameWithOwner: projectPath,
	}
}

func mergeableFromDetailedStatus(status string) string {
	switch status {
	case "CONFLICT":
		return "CONFLICTING"
	case "MERGEABLE":
		return "MERGEABLE"
	default:
		return "UNKNOWN"
	}
}

func reviewDecisionFromApproved(approved bool) string {
	if approved {
		return "APPROVED"
	}
	return "REVIEW_REQUIRED"
}

func mergeStateStatusFromDetailedStatus(status string) MergeStateStatus {
	switch status {
	case "MERGEABLE":
		return "CLEAN"
	case "CI_STILL_RUNNING":
		return "UNSTABLE"
	case "DISCUSSIONS_NOT_RESOLVED", "NOT_APPROVED", "DRAFT_STATUS":
		return "BLOCKED"
	default:
		return ""
	}
}

type headPipelineNode struct {
	Status string
}

type mergeRequestNode struct {
	Iid                 string
	Title               string
	Description         string
	State               string
	Draft               bool
	Author              struct{ Username string }
	CreatedAt           time.Time
	UpdatedAt           time.Time
	WebUrl              string
	SourceBranch        string
	TargetBranch        string
	DetailedMergeStatus string
	Approved            bool
	DiffStatsSummary    struct {
		Additions int
		Deletions int
	}
	Labels struct {
		Nodes []gitlabLabelNode
	} `graphql:"labels(first: 6)"`
	Assignees struct {
		Nodes []gitlabUserNode
	} `graphql:"assignees(first: 10)"`
	HeadPipeline *headPipelineNode `graphql:"headPipeline"`
}

// lastCommitStatusFromHeadPipeline adapts the GitLab merge request's head
// pipeline status into the LastCommitStatus shape that prrow.go/branch.go
// already know how to read. GitLab's GraphQL enum values come back
// UPPERCASE (e.g. "SUCCESS"); the data.PipelineStatus adapter (T14) expects
// lowercase, so this is the single normalization point for the list/detail
// fetch path.
func lastCommitStatusFromHeadPipeline(hp *headPipelineNode) LastCommitStatus {
	if hp == nil {
		return LastCommitStatus{}
	}
	var result LastCommitStatus
	result.Nodes = make([]struct {
		Commit struct {
			StatusCheckRollup struct {
				State graphql.String
			}
		}
	}, 1)
	result.Nodes[0].Commit.StatusCheckRollup.State = graphql.String(
		strings.ToLower(hp.Status),
	)
	return result
}

func (n mergeRequestNode) toPullRequestData(projectPath string) PullRequestData {
	number, _ := strconv.Atoi(n.Iid)
	return PullRequestData{
		Number:           number,
		Title:            n.Title,
		Author:           struct{ Login string }{Login: n.Author.Username},
		CreatedAt:        n.CreatedAt,
		UpdatedAt:        n.UpdatedAt,
		Url:              n.WebUrl,
		State:            n.State,
		IsDraft:          n.Draft,
		HeadRefName:      n.SourceBranch,
		BaseRefName:      n.TargetBranch,
		Additions:        n.DiffStatsSummary.Additions,
		Deletions:        n.DiffStatsSummary.Deletions,
		Mergeable:        mergeableFromDetailedStatus(n.DetailedMergeStatus),
		ReviewDecision:   reviewDecisionFromApproved(n.Approved),
		MergeStateStatus: mergeStateStatusFromDetailedStatus(n.DetailedMergeStatus),
		IsInMergeQueue:   false,
		Labels:           labelsFromNodes(n.Labels.Nodes),
		Assignees:        assigneesFromNodes(n.Assignees.Nodes),
		Repository:       repositoryFromProjectPath(projectPath),
		Commits:          lastCommitStatusFromHeadPipeline(n.HeadPipeline),
	}
}

func (n mergeRequestNode) toEnrichedPullRequestData(projectPath string) EnrichedPullRequestData {
	number, _ := strconv.Atoi(n.Iid)
	return EnrichedPullRequestData{
		Number:         number,
		Title:          n.Title,
		Body:           n.Description,
		Author:         struct{ Login string }{Login: n.Author.Username},
		CreatedAt:      n.CreatedAt,
		UpdatedAt:      n.UpdatedAt,
		Url:            n.WebUrl,
		State:          n.State,
		IsDraft:        n.Draft,
		HeadRefName:    n.SourceBranch,
		BaseRefName:    n.TargetBranch,
		Additions:      n.DiffStatsSummary.Additions,
		Deletions:      n.DiffStatsSummary.Deletions,
		Mergeable:      mergeableFromDetailedStatus(n.DetailedMergeStatus),
		ReviewDecision: reviewDecisionFromApproved(n.Approved),
		Labels:         labelsFromNodes(n.Labels.Nodes),
		Assignees:      assigneesFromNodes(n.Assignees.Nodes),
		Repository:     repositoryFromProjectPath(projectPath),
	}
}

func FetchPullRequests(query string, limit int, pageInfo *PageInfo) (PullRequestsResponse, error) {
	c, err := resolveGraphQLClient()
	if err != nil {
		return PullRequestsResponse{}, err
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

	log.Debug("Fetching MRs", "query", query, "limit", limit, "endCursor", endCursor)

	var nodes []mergeRequestNode
	var totalCount int
	var respPageInfo PageInfo

	labels := optionalGraphQLStringList[graphql.String](translated.Labels)

	if translated.ProjectPath != "" {
		var sourceBranch []string
		if translated.SourceBranch != "" {
			sourceBranch = []string{translated.SourceBranch}
		}
		sourceBranches := optionalGraphQLStringList[graphql.String](sourceBranch)

		var queryResult struct {
			Project struct {
				MergeRequests struct {
					Nodes    []mergeRequestNode
					Count    int
					PageInfo PageInfo
				} `graphql:"mergeRequests(first: $limit, after: $endCursor, sort: UPDATED_DESC, state: $state, authorUsername: $authorUsername, assigneeUsername: $assigneeUsername, reviewerUsername: $reviewerUsername, labels: $labels, sourceBranches: $sourceBranches)"`
			} `graphql:"project(fullPath: $fullPath)"`
		}
		variables := map[string]any{
			"fullPath":         graphql.ID(translated.ProjectPath),
			"limit":            graphql.Int(limit),
			"endCursor":        (*graphql.String)(endCursor),
			"state":            optionalGraphQLValue[MergeRequestState](translated.State),
			"authorUsername":   optionalGraphQLValue[graphql.String](translated.AuthorUsername),
			"assigneeUsername": optionalGraphQLValue[graphql.String](translated.AssigneeUsername),
			"reviewerUsername": optionalGraphQLValue[graphql.String](translated.ReviewerUsername),
			"labels":           labels,
			"sourceBranches":   sourceBranches,
		}
		err = c.QueryNamed(context.Background(), "ProjectMergeRequests", &queryResult, variables)
		if err != nil {
			return PullRequestsResponse{}, err
		}
		nodes = queryResult.Project.MergeRequests.Nodes
		totalCount = queryResult.Project.MergeRequests.Count
		respPageInfo = queryResult.Project.MergeRequests.PageInfo
	} else {
		state := optionalGraphQLValue[MergeRequestState](translated.State)

		reviewerUsername := translated.ReviewerUsername
		if reviewerUsername != "" && reviewerUsername != currentUsername {
			log.Warn(
				"search qualifier only supported for the current user without project:, ignoring",
				"qualifier", "review-requested:"+reviewerUsername,
			)
			reviewerUsername = ""
		}
		assigneeUsername := translated.AssigneeUsername
		if assigneeUsername != "" && assigneeUsername != currentUsername {
			log.Warn(
				"search qualifier only supported for the current user without project:, ignoring",
				"qualifier", "assignee:"+assigneeUsername,
			)
			assigneeUsername = ""
		}
		authorUsername := translated.AuthorUsername
		if authorUsername != "" && authorUsername != currentUsername {
			log.Warn(
				"search qualifier only supported for the current user without project:, ignoring",
				"qualifier", "author:"+authorUsername,
			)
			authorUsername = ""
		}

		switch {
		case reviewerUsername != "":
			var queryResult struct {
				CurrentUser struct {
					ReviewRequestedMergeRequests struct {
						Nodes    []mergeRequestNode
						Count    int
						PageInfo PageInfo
					} `graphql:"reviewRequestedMergeRequests(first: $limit, after: $endCursor, sort: UPDATED_DESC, state: $state, labels: $labels, authorUsername: $authorUsername, assigneeUsername: $assigneeUsername)"`
				}
			}
			variables := map[string]any{
				"limit":            graphql.Int(limit),
				"endCursor":        (*graphql.String)(endCursor),
				"state":            state,
				"labels":           labels,
				"authorUsername":   optionalGraphQLValue[graphql.String](authorUsername),
				"assigneeUsername": optionalGraphQLValue[graphql.String](assigneeUsername),
			}
			err = c.QueryNamed(
				context.Background(),
				"MyReviewRequestedMergeRequests",
				&queryResult,
				variables,
			)
			if err != nil {
				return PullRequestsResponse{}, err
			}
			nodes = queryResult.CurrentUser.ReviewRequestedMergeRequests.Nodes
			totalCount = queryResult.CurrentUser.ReviewRequestedMergeRequests.Count
			respPageInfo = queryResult.CurrentUser.ReviewRequestedMergeRequests.PageInfo
		case assigneeUsername != "" && authorUsername == "":
			var queryResult struct {
				CurrentUser struct {
					AssignedMergeRequests struct {
						Nodes    []mergeRequestNode
						Count    int
						PageInfo PageInfo
					} `graphql:"assignedMergeRequests(first: $limit, after: $endCursor, sort: UPDATED_DESC, state: $state, labels: $labels)"`
				}
			}
			variables := map[string]any{
				"limit":     graphql.Int(limit),
				"endCursor": (*graphql.String)(endCursor),
				"state":     state,
				"labels":    labels,
			}
			err = c.QueryNamed(
				context.Background(),
				"MyAssignedMergeRequests",
				&queryResult,
				variables,
			)
			if err != nil {
				return PullRequestsResponse{}, err
			}
			nodes = queryResult.CurrentUser.AssignedMergeRequests.Nodes
			totalCount = queryResult.CurrentUser.AssignedMergeRequests.Count
			respPageInfo = queryResult.CurrentUser.AssignedMergeRequests.PageInfo
		default:
			var queryResult struct {
				CurrentUser struct {
					AuthoredMergeRequests struct {
						Nodes    []mergeRequestNode
						Count    int
						PageInfo PageInfo
					} `graphql:"authoredMergeRequests(first: $limit, after: $endCursor, sort: UPDATED_DESC, state: $state, labels: $labels, assigneeUsername: $assigneeUsername)"`
				}
			}
			variables := map[string]any{
				"limit":            graphql.Int(limit),
				"endCursor":        (*graphql.String)(endCursor),
				"state":            state,
				"labels":           labels,
				"assigneeUsername": optionalGraphQLValue[graphql.String](assigneeUsername),
			}
			err = c.QueryNamed(context.Background(), "MyMergeRequests", &queryResult, variables)
			if err != nil {
				return PullRequestsResponse{}, err
			}
			nodes = queryResult.CurrentUser.AuthoredMergeRequests.Nodes
			totalCount = queryResult.CurrentUser.AuthoredMergeRequests.Count
			respPageInfo = queryResult.CurrentUser.AuthoredMergeRequests.PageInfo
		}
	}

	log.Info("Successfully fetched MRs", "count", totalCount)

	prs := make([]PullRequestData, 0, len(nodes))
	for _, n := range nodes {
		prs = append(prs, n.toPullRequestData(translated.ProjectPath))
	}

	return PullRequestsResponse{
		Prs:        prs,
		TotalCount: totalCount,
		PageInfo:   respPageInfo,
	}, nil
}

func parseMergeRequestUrl(mrUrl string) (fullPath string, iid string, err error) {
	parsedUrl, err := url.Parse(mrUrl)
	if err != nil {
		return "", "", err
	}
	const sep = "/-/merge_requests/"
	path := strings.TrimPrefix(parsedUrl.Path, "/")
	before, after, found := strings.Cut(path, sep)
	if !found || before == "" || after == "" {
		return "", "", fmt.Errorf("not a merge request URL: %s", mrUrl)
	}
	return before, after, nil
}

func FetchPullRequest(prUrl string) (EnrichedPullRequestData, error) {
	fullPath, iid, err := parseMergeRequestUrl(prUrl)
	if err != nil {
		return EnrichedPullRequestData{}, err
	}

	c, err := resolveGraphQLClient()
	if err != nil {
		return EnrichedPullRequestData{}, err
	}

	var queryResult struct {
		Project struct {
			MergeRequest mergeRequestNode `graphql:"mergeRequest(iid: $iid)"`
		} `graphql:"project(fullPath: $fullPath)"`
	}
	variables := map[string]any{
		"fullPath": graphql.ID(fullPath),
		"iid":      graphql.String(iid),
	}
	log.Debug("Fetching PR", "url", prUrl)
	err = c.QueryNamed(context.Background(), "FetchMergeRequest", &queryResult, variables)
	if err != nil {
		return EnrichedPullRequestData{}, err
	}
	log.Info("Successfully fetched PR", "url", prUrl)

	enriched := queryResult.Project.MergeRequest.toEnrichedPullRequestData(fullPath)
	enriched.Pipeline = fetchPipelineBestEffort(fullPath, iid)
	return enriched, nil
}

// fetchPipelineBestEffort resolves the merge request's pipeline and jobs via
// REST. Failures are logged and swallowed: pipeline/job data enriches the CI
// views but must never fail the surrounding merge request fetch. Skips the
// REST round-trip entirely under FF_MOCK_DATA, or when no REST client is
// already cached/injected AND no GitLab credential is configured, mirroring
// the guard resolveGraphQLClient/cmd/root.go already apply — otherwise every
// merge request fetch during local mock development or a GitHub-only setup
// mid-migration would fire an unsolicited request against the GitLab host
// and log a misleading warning. The gitlabRESTClient==nil check keeps this
// from short-circuiting tests (or any future caller) that inject a client
// via SetRESTClient without going through LoadAuthConfig at all.
func fetchPipelineBestEffort(fullPath, iid string) MergeRequestPipeline {
	if config.IsFeatureEnabled(config.FF_MOCK_DATA) {
		return MergeRequestPipeline{}
	}
	if gitlabRESTClient == nil {
		if auth, err := gitlab.LoadAuthConfig(); err != nil || auth.Token == "" {
			return MergeRequestPipeline{}
		}
	}

	mrIID, err := strconv.Atoi(iid)
	if err != nil {
		log.Warn("failed to parse merge request iid for pipeline lookup", "iid", iid, "err", err)
		return MergeRequestPipeline{}
	}
	pipeline, err := FindPipelineForMR(fullPath, mrIID)
	if err != nil {
		log.Warn("failed to fetch pipeline for merge request", "err", err)
		return MergeRequestPipeline{}
	}
	if pipeline.ID == 0 {
		return pipeline
	}
	jobs, err := ListPipelineJobs(fullPath, pipeline.ID, "")
	if err != nil {
		log.Warn("failed to fetch pipeline jobs for merge request", "err", err)
		return pipeline
	}
	pipeline.Jobs = jobs
	return pipeline
}
