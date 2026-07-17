package data

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"charm.land/log/v2"
	graphql "github.com/cli/shurcooL-graphql"
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
	Comments           CommentsWithBody          `graphql:"comments(last: 50, orderBy: { field: UPDATED_AT, direction: DESC })"`
	ReviewThreads      ReviewThreadsWithComments `graphql:"reviewThreads(last: 50)"`
	ReviewRequests     ReviewRequests            `graphql:"reviewRequests(last: 100)"`
	Reviews            Reviews                   `graphql:"reviews(last: 100)"`
	SuggestedReviewers []SuggestedReviewer
	Files              ChangedFiles `graphql:"files(first: 20)"`
	Commits            []PullRequestCommit
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

// GitLab has no server-side "counts by state" for jobs (unlike GitHub's
// StatusCheckRollup.Contexts.*CountsByState) — PipelineJob/MergeRequestPipeline
// and CountJobsByState are the GitLab-native replacement for the GitHub-only
// CheckRun/StatusContext/StatusCheckRollupStats/ContextCountByState types
// (removed along with the GitHub checks adapter dependency they relied on).
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

// PullRequestCommit is a single commit shown in the MR Commits tab. GitLab
// merge requests expose commits via GraphQL, unlike the GitHub-only Commits
// type above which the migration left unused.
type PullRequestCommit struct {
	Sha       string
	Title     string
	Author    string
	CreatedAt time.Time
	Url       string
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
	clientMu     sync.Mutex
)

func SetClient(c *graphql.Client) {
	clientMu.Lock()
	client = c
	cachedClient = c
	clientMu.Unlock()
}

// ClearEnrichmentCache clears the cached GraphQL client used for fetching
// enriched PR/Issue data. Call this when refreshing to ensure fresh data.
func ClearEnrichmentCache() {
	clientMu.Lock()
	cachedClient = nil
	clientMu.Unlock()
}

// IsEnrichmentCacheCleared returns true if the enrichment cache is cleared.
// This is primarily for testing purposes.
func IsEnrichmentCacheCleared() bool {
	clientMu.Lock()
	defer clientMu.Unlock()
	return cachedClient == nil
}

func resolveGraphQLClient() (*graphql.Client, error) {
	clientMu.Lock()
	defer clientMu.Unlock()
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

var (
	gitlabRESTClient   *gitlabapi.Client
	gitlabRESTClientMu sync.Mutex
)

// SetRESTClient overrides the cached REST client used to fetch pipeline/job
// data. Used by tests.
func SetRESTClient(c *gitlabapi.Client) {
	gitlabRESTClientMu.Lock()
	gitlabRESTClient = c
	gitlabRESTClientMu.Unlock()
}

func resolveRESTClient() (*gitlabapi.Client, error) {
	gitlabRESTClientMu.Lock()
	defer gitlabRESTClientMu.Unlock()
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

func hasCachedRESTClient() bool {
	gitlabRESTClientMu.Lock()
	defer gitlabRESTClientMu.Unlock()
	return gitlabRESTClient != nil
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
	opts := &gitlabapi.ListJobsOptions{
		ListOptions: gitlabapi.ListOptions{PerPage: 100},
	}
	if scope != "" {
		opts.Scope = &[]gitlabapi.BuildStateValue{gitlabapi.BuildStateValue(scope)}
	}

	result := make([]PipelineJob, 0)
	// A pipeline can have far more jobs than a single page holds (the API
	// defaults to 20), so follow pagination to the end. Otherwise failing jobs
	// on later pages are silently dropped and the pipeline looks healthier than
	// it actually is.
	for {
		jobs, resp, err := c.Jobs.ListPipelineJobs(projectPath, pipelineID, opts)
		if err != nil {
			return nil, err
		}
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
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
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

type gitlabNotePositionNode struct {
	FilePath string
	NewLine  int
	OldLine  int
}

type gitlabNoteNode struct {
	Author struct {
		Username string
	}
	Body      string
	CreatedAt time.Time
	UpdatedAt time.Time
	System    bool
	Position  *gitlabNotePositionNode
}

type gitlabDiscussionNode struct {
	Notes struct {
		Nodes []gitlabNoteNode
	} `graphql:"notes(first: 100)"`
}

func diffPositionLineAndPath(p *gitlabNotePositionNode) (string, int) {
	if p == nil {
		return "", 0
	}
	line := p.NewLine
	if line == 0 {
		line = p.OldLine
	}
	return p.FilePath, line
}

func commentsAndReviewThreadsFromDiscussions(
	discussions []gitlabDiscussionNode,
) (CommentsWithBody, ReviewThreadsWithComments) {
	var comments []Comment
	var threads []struct {
		Id           string
		IsOutdated   bool
		OriginalLine int
		StartLine    int
		Line         int
		Path         string
		Comments     ReviewComments `graphql:"comments(first: 20)"`
	}

	for _, discussion := range discussions {
		// Group thread-vs-comment per discussion, not per note: in GitLab
		// only the first note of a diff discussion carries a position;
		// replies come with position: null. Detecting the position on any
		// non-system note marks the whole discussion as a review thread so
		// its replies stay attached instead of fragmenting into top-level
		// comments. System notes never make a discussion a review thread.
		var path string
		var line int
		hasPosition := false
		for _, note := range discussion.Notes.Nodes {
			if note.System || note.Position == nil {
				continue
			}
			path, line = diffPositionLineAndPath(note.Position)
			hasPosition = true
			break
		}

		var lineComments []ReviewComment
		for _, note := range discussion.Notes.Nodes {
			if note.System {
				continue
			}
			if hasPosition {
				lineComments = append(lineComments, ReviewComment{
					Author:    struct{ Login string }{Login: note.Author.Username},
					Body:      note.Body,
					UpdatedAt: note.UpdatedAt,
					StartLine: line,
					Line:      line,
				})
				continue
			}
			comments = append(comments, Comment{
				Author:    struct{ Login string }{Login: note.Author.Username},
				Body:      note.Body,
				UpdatedAt: note.UpdatedAt,
			})
		}

		if len(lineComments) > 0 {
			threads = append(threads, struct {
				Id           string
				IsOutdated   bool
				OriginalLine int
				StartLine    int
				Line         int
				Path         string
				Comments     ReviewComments `graphql:"comments(first: 20)"`
			}{
				Path:         path,
				OriginalLine: line,
				StartLine:    line,
				Line:         line,
				Comments:     ReviewComments{Nodes: lineComments, TotalCount: len(lineComments)},
			})
		}
	}

	return CommentsWithBody{TotalCount: graphql.Int(len(comments)), Nodes: comments},
		ReviewThreadsWithComments{Nodes: threads}
}

type gitlabDiffStatNode struct {
	Path      string
	Additions int
	Deletions int
}

type gitlabCommitNode struct {
	Sha          string
	Title        string
	WebUrl       string
	AuthoredDate time.Time
	Author       struct {
		Username string
	}
	AuthorName string
}

// commitsFromNodes maps GitLab's merge request commit nodes into the
// PullRequestCommit shape the Commits tab renders. It prefers the linked
// GitLab user's username and falls back to the raw authorName for commits
// authored by someone without a GitLab account.
func commitsFromNodes(nodes []gitlabCommitNode) []PullRequestCommit {
	commits := make([]PullRequestCommit, len(nodes))
	for i, n := range nodes {
		author := n.Author.Username
		if author == "" {
			author = n.AuthorName
		}
		commits[i] = PullRequestCommit{
			Sha:       n.Sha,
			Title:     n.Title,
			Author:    author,
			CreatedAt: n.AuthoredDate,
			Url:       n.WebUrl,
		}
	}
	return commits
}

// changedFilesFromDiffStats maps GitLab's per-file diffStats
// ({ path, additions, deletions }) into the ChangedFiles shape the Files
// Changed tab renders. GitLab's diffStats carries no change-type flag
// (added/deleted/renamed), so ChangeType is left empty and the row shows no
// type glyph.
func changedFilesFromDiffStats(nodes []gitlabDiffStatNode) ChangedFiles {
	files := make([]ChangedFile, len(nodes))
	for i, n := range nodes {
		files[i] = ChangedFile{
			Path:      n.Path,
			Additions: n.Additions,
			Deletions: n.Deletions,
		}
	}
	return ChangedFiles{TotalCount: len(files), Nodes: files}
}

func reviewsFromApprovedBy(nodes []gitlabUserNode) Reviews {
	result := make([]Review, len(nodes))
	for i, n := range nodes {
		result[i] = Review{
			Author: struct{ Login string }{Login: n.Username},
			State:  "APPROVED",
		}
	}
	return Reviews{TotalCount: len(result), Nodes: result}
}

func reviewRequestsFromReviewers(nodes []gitlabUserNode) ReviewRequests {
	result := make([]ReviewRequestNode, len(nodes))
	for i, n := range nodes {
		result[i] = ReviewRequestNode{
			RequestedReviewer: struct {
				User      RequestedReviewerUser      `graphql:"... on User"`
				Team      RequestedReviewerTeam      `graphql:"... on Team"`
				Bot       RequestedReviewerBot       `graphql:"... on Bot"`
				Mannequin RequestedReviewerMannequin `graphql:"... on Mannequin"`
			}{
				User: RequestedReviewerUser{Login: n.Username},
			},
		}
	}
	return ReviewRequests{TotalCount: len(result), Nodes: result}
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

// pullRequestStateFromGitLab normalizes GitLab's lowercase merge request state
// (opened/closed/locked/merged) into the GitHub-style uppercase form
// (OPEN/CLOSED/MERGED) the TUI was written against — the rendering and
// optimistic-update code all switch on OPEN/CLOSED/MERGED, so without this the
// state glyph column falls through to the "-" placeholder. A "locked" MR (merge
// in progress) is still shown as open.
func pullRequestStateFromGitLab(state string) string {
	switch strings.ToLower(state) {
	case "opened", "locked":
		return "OPEN"
	case "closed":
		return "CLOSED"
	case "merged":
		return "MERGED"
	default:
		return strings.ToUpper(state)
	}
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
	UserNotesCount      int
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
	// Cross-project searches (e.g. author:@me with no project:) don't carry a
	// project path, but the web URL does — derive it so Repository/RepoName is
	// populated for the row and for custom keybinding templates ({{.RepoName}}).
	if projectPath == "" {
		if fullPath, _, err := parseMergeRequestUrl(n.WebUrl); err == nil {
			projectPath = fullPath
		}
	}
	return PullRequestData{
		Number:           number,
		Title:            n.Title,
		Author:           struct{ Login string }{Login: n.Author.Username},
		CreatedAt:        n.CreatedAt,
		UpdatedAt:        n.UpdatedAt,
		Url:              n.WebUrl,
		State:            pullRequestStateFromGitLab(n.State),
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
		// GitLab's userNotesCount is the comment-bubble count (all user notes,
		// system notes excluded). The listing renders Comments.TotalCount +
		// ReviewThreads.TotalCount, so map it here and leave ReviewThreads at
		// zero to avoid double-counting the diff-discussion notes it already includes.
		Comments: Comments{TotalCount: n.UserNotesCount},
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
		State:          pullRequestStateFromGitLab(n.State),
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
	populateAuthorRoles(prs, translated.ProjectPath)

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

type mergeRequestActivityNode struct {
	mergeRequestNode
	Discussions struct {
		Nodes []gitlabDiscussionNode
	} `graphql:"discussions(first: 50)"`
	ApprovedBy struct {
		Nodes []gitlabUserNode
	} `graphql:"approvedBy(first: 50)"`
	Reviewers struct {
		Nodes []gitlabUserNode
	} `graphql:"reviewers(first: 50)"`
	DiffStats []gitlabDiffStatNode `graphql:"diffStats"`
	Commits   struct {
		Nodes []gitlabCommitNode
	} `graphql:"commits(first: 100)"`
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
			MergeRequest mergeRequestActivityNode `graphql:"mergeRequest(iid: $iid)"`
		} `graphql:"project(fullPath: $fullPath)"`
	}
	variables := map[string]any{
		"fullPath": graphql.ID(fullPath),
		"iid":      graphql.String(iid),
	}
	log.Debug("Fetching MR", "url", prUrl)
	err = c.QueryNamed(context.Background(), "FetchMergeRequest", &queryResult, variables)
	if err != nil {
		return EnrichedPullRequestData{}, err
	}
	log.Info("Successfully fetched MR", "url", prUrl)

	mr := queryResult.Project.MergeRequest
	enriched := mr.toEnrichedPullRequestData(fullPath)
	enriched.Comments, enriched.ReviewThreads = commentsAndReviewThreadsFromDiscussions(
		mr.Discussions.Nodes,
	)
	enriched.Reviews = reviewsFromApprovedBy(mr.ApprovedBy.Nodes)
	enriched.ReviewRequests = reviewRequestsFromReviewers(mr.Reviewers.Nodes)
	enriched.Files = changedFilesFromDiffStats(mr.DiffStats)
	enriched.Commits = commitsFromNodes(mr.Commits.Nodes)
	enriched.SuggestedReviewers = nil
	enriched.Pipeline = fetchPipelineBestEffort(fullPath, iid)
	enriched.AuthorAssociation = resolveCachedAuthorRole(fullPath, mr.Author.Username)
	return enriched, nil
}

// queryAuthorRole resolves the author's access level in the project
// (Owner/Maintainer/Developer/Reporter/Guest) via GraphQL. GitLab has no
// per-merge-request "author association" like GitHub, so the project membership
// access level is the closest equivalent for the author role badge. Returns
// ("", nil) when the author is not a project member (directly or through an
// inherited group membership); a non-nil error means the lookup itself failed.
func queryAuthorRole(fullPath, username string) (string, error) {
	if username == "" || fullPath == "" {
		return "", nil
	}
	c, err := resolveGraphQLClient()
	if err != nil {
		return "", err
	}
	var q struct {
		Project struct {
			ProjectMembers struct {
				Nodes []struct {
					User        struct{ Username string }
					AccessLevel struct{ StringValue string }
				}
			} `graphql:"projectMembers(search: $search, relations: [DIRECT, INHERITED])"`
		} `graphql:"project(fullPath: $fullPath)"`
	}
	variables := map[string]any{
		"fullPath": graphql.ID(fullPath),
		"search":   graphql.String(username),
	}
	if err := c.QueryNamed(context.Background(), "MergeRequestAuthorRole", &q, variables); err != nil {
		return "", err
	}
	// search is a fuzzy match, so pick the node whose username matches exactly.
	for _, n := range q.Project.ProjectMembers.Nodes {
		if strings.EqualFold(n.User.Username, username) {
			return n.AccessLevel.StringValue, nil
		}
	}
	return "", nil
}

// fetchAuthorRoleBestEffort resolves the author's project access level, swallowing
// any error: the badge is cosmetic and must never fail the surrounding fetch.
func fetchAuthorRoleBestEffort(fullPath, username string) string {
	role, err := queryAuthorRole(fullPath, username)
	if err != nil {
		log.Debug("failed to resolve merge request author role", "err", err)
		return ""
	}
	return role
}

var (
	authorRoleCache   = map[string]string{}
	authorRoleCacheMu sync.RWMutex
)

func authorRoleCacheKey(fullPath, username string) string {
	return fullPath + "\x00" + username
}

// resolveCachedAuthorRole returns the author's project access level, caching
// successful lookups for the session so repeated list refreshes (and the
// preview) don't re-query GitLab for the same author. Errors are not cached, so
// a transient failure is retried on the next refresh.
func resolveCachedAuthorRole(fullPath, username string) string {
	if fullPath == "" || username == "" {
		return ""
	}
	key := authorRoleCacheKey(fullPath, username)
	authorRoleCacheMu.RLock()
	cached, ok := authorRoleCache[key]
	authorRoleCacheMu.RUnlock()
	if ok {
		return cached
	}
	role, err := queryAuthorRole(fullPath, username)
	if err != nil {
		return ""
	}
	authorRoleCacheMu.Lock()
	authorRoleCache[key] = role
	authorRoleCacheMu.Unlock()
	return role
}

// clearAuthorRoleCache resets the session role cache. Used by tests for isolation.
func clearAuthorRoleCache() {
	authorRoleCacheMu.Lock()
	authorRoleCache = map[string]string{}
	authorRoleCacheMu.Unlock()
}

// populateAuthorRoles fills each merge request author's project role for the
// listing's author badge, resolving unique authors concurrently and caching the
// result. It is a no-op for cross-project searches (no project path); authors
// whose role can't be resolved keep an empty AuthorAssociation so the badge
// falls back to the neutral icon.
func populateAuthorRoles(prs []PullRequestData, fullPath string) {
	if fullPath == "" || len(prs) == 0 {
		return
	}

	authors := make(map[string]struct{})
	for _, pr := range prs {
		if pr.Author.Login != "" {
			authors[pr.Author.Login] = struct{}{}
		}
	}

	roles := make(map[string]string, len(authors))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for username := range authors {
		wg.Add(1)
		go func(username string) {
			defer wg.Done()
			role := resolveCachedAuthorRole(fullPath, username)
			if role == "" {
				return
			}
			mu.Lock()
			roles[username] = role
			mu.Unlock()
		}(username)
	}
	wg.Wait()

	for i := range prs {
		if role, ok := roles[prs[i].Author.Login]; ok {
			prs[i].AuthorAssociation = role
		}
	}
}

// fetchPipelineBestEffort resolves the merge request's pipeline and jobs via
// REST. Failures are logged and swallowed: pipeline/job data enriches the CI
// views but must never fail the surrounding merge request fetch. Skips the
// REST round-trip entirely under FF_MOCK_DATA, or when no REST client is
// already cached/injected AND no GitLab credential is configured, mirroring
// the guard resolveGraphQLClient/cmd/root.go already apply — otherwise every
// merge request fetch during local mock development or a GitHub-only setup
// mid-migration would fire an unsolicited request against the GitLab host
// and log a misleading warning. The hasCachedRESTClient() check (a
// synchronized read of gitlabRESTClient) keeps this from short-circuiting
// tests (or any future caller) that inject a client via SetRESTClient
// without going through LoadAuthConfig at all.
func fetchPipelineBestEffort(fullPath, iid string) MergeRequestPipeline {
	if config.IsFeatureEnabled(config.FF_MOCK_DATA) {
		return MergeRequestPipeline{}
	}
	if !hasCachedRESTClient() {
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
