package data

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/log/v2"
	gitlabapi "gitlab.com/gitlab-org/api/client-go"
)

// Notification subject types. PullRequest/Issue/Discussion/Release/Commit/CheckSuite
// come from the GitHub notifications API; MergeRequest/Issue come from GitLab's
// Todo.TargetType. Issue is shared by both vocabularies.
const (
	SubjectTypePullRequest  = "PullRequest"
	SubjectTypeIssue        = "Issue"
	SubjectTypeDiscussion   = "Discussion"
	SubjectTypeRelease      = "Release"
	SubjectTypeCommit       = "Commit"
	SubjectTypeCheckSuite   = "CheckSuite"
	SubjectTypeMergeRequest = "MergeRequest"
)

// Notification reasons. The first block comes from the GitHub notifications
// API; the second block comes from GitLab's Todo.ActionName (gitlab.TodoAction).
const (
	ReasonSubscribed      = "subscribed"
	ReasonReviewRequested = "review_requested"
	ReasonMention         = "mention"
	ReasonAuthor          = "author"
	ReasonComment         = "comment"
	ReasonAssign          = "assign"
	ReasonStateChange     = "state_change"
	ReasonCIActivity      = "ci_activity"
	ReasonTeamMention     = "team_mention"
	ReasonSecurityAlert   = "security_alert"

	ReasonAssigned          = "assigned"
	ReasonMentioned         = "mentioned"
	ReasonBuildFailed       = "build_failed"
	ReasonMarked            = "marked"
	ReasonApprovalRequired  = "approval_required"
	ReasonDirectlyAddressed = "directly_addressed"
)

type NotificationSubject struct {
	Title            string `json:"title"`
	Url              string `json:"url"`
	LatestCommentUrl string `json:"latest_comment_url"`
	Type             string `json:"type"`
}

type NotificationRepository struct {
	Id       int    `json:"id"`
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	Private  bool   `json:"private"`
	Owner    struct {
		Login string `json:"login"`
	} `json:"owner"`
	HtmlUrl string `json:"html_url"`
}

type NotificationData struct {
	Id           string                 `json:"id"`
	Unread       bool                   `json:"unread"`
	Reason       string                 `json:"reason"`
	UpdatedAt    time.Time              `json:"updated_at"`
	LastReadAt   *time.Time             `json:"last_read_at"`
	Subject      NotificationSubject    `json:"subject"`
	Repository   NotificationRepository `json:"repository"`
	Url          string                 `json:"url"`
	Subscription string                 `json:"subscription_url"`
	// Actor is the username of the user who triggered this todo
	// (Todo.Author.Username on GitLab). Empty for data not sourced from a Todo.
	Actor string `json:"-"`
}

func (n NotificationData) GetTitle() string {
	return n.Subject.Title
}

func (n NotificationData) GetRepoNameWithOwner() string {
	return n.Repository.FullName
}

func (n NotificationData) GetNumber() int {
	// Notifications don't have a number, return 0
	return 0
}

func (n NotificationData) GetUrl() string {
	return strings.TrimRight(n.Repository.HtmlUrl, "/")
}

func (n NotificationData) GetUpdatedAt() time.Time {
	return n.UpdatedAt
}

func (n NotificationData) GetCreatedAt() time.Time {
	// Notifications don't have a created_at, use updated_at
	return n.UpdatedAt
}

type NotificationsResponse struct {
	Notifications []NotificationData
	TotalCount    int
	PageInfo      PageInfo
}

// getTodosService returns the GitLab Todos REST service, reusing the cached
// REST client already maintained by resolveRESTClient (internal/data/prapi.go,
// same package) instead of keeping a second, redundant client cache.
func getTodosService() (gitlabapi.TodosServiceInterface, error) {
	c, err := resolveRESTClient()
	if err != nil {
		return nil, err
	}
	return c.Todos, nil
}

// NotificationReadState represents the read state filter for notifications
type NotificationReadState string

const (
	NotificationStateUnread NotificationReadState = "unread" // Only unread (default)
	NotificationStateRead   NotificationReadState = "read"   // Only read
	NotificationStateAll    NotificationReadState = "all"    // Both read and unread
)

// notificationThreadScanMaxPages bounds the best-effort scan performed by
// FetchNotificationByThreadId. Not a contractual limit, just a sane cap on
// how many pages (of 100) we're willing to walk per state.
const notificationThreadScanMaxPages = 10

func FetchNotifications(
	limit int,
	repoFilters []string,
	readState NotificationReadState,
	pageInfo *PageInfo,
) (NotificationsResponse, error) {
	todos, err := getTodosService()
	if err != nil {
		return NotificationsResponse{}, err
	}

	// Determine page number from PageInfo (EndCursor stores the current page as string)
	page := 1
	if pageInfo != nil && pageInfo.EndCursor != "" {
		fmt.Sscanf(pageInfo.EndCursor, "%d", &page)
	}

	opt := &gitlabapi.ListTodosOptions{
		ListOptions: gitlabapi.ListOptions{
			PerPage: int64(limit),
			Page:    int64(page),
		},
	}
	switch readState {
	case NotificationStateUnread:
		opt.State = gitlabapi.Ptr("pending")
	case NotificationStateRead:
		opt.State = gitlabapi.Ptr("done")
	case NotificationStateAll:
		// No state filter. Note: GitLab's GET /todos returns only pending
		// todos when no state filter is applied (not both), a real
		// asymmetry versus the old GitHub all=true behavior.
	}

	log.Debug("Fetching notifications", "limit", limit, "page", page, "readState", readState)
	items, resp, err := todos.ListTodos(opt)
	if err != nil {
		return NotificationsResponse{}, err
	}

	// ListTodosOptions.ProjectID filters by numeric project ID, not by
	// "group/project" path, so repo filtering happens client-side here.
	repoFilterSet := make(map[string]bool, len(repoFilters))
	for _, r := range repoFilters {
		repoFilterSet[r] = true
	}

	notifications := make([]NotificationData, 0, len(items))
	for _, item := range items {
		n := notificationFromTodo(item)
		if len(repoFilterSet) > 0 && !repoFilterSet[n.Repository.FullName] {
			continue
		}
		notifications = append(notifications, n)
	}

	hasNextPage := resp != nil && resp.NextPage != 0
	nextPage := ""
	if hasNextPage {
		nextPage = fmt.Sprintf("%d", resp.NextPage)
	}

	log.Info(
		"Successfully fetched notifications",
		"count", len(notifications),
		"page", page,
		"hasNextPage", hasNextPage,
		"readState", readState,
	)

	return NotificationsResponse{
		Notifications: notifications,
		TotalCount:    len(notifications),
		PageInfo: PageInfo{
			HasNextPage: hasNextPage,
			EndCursor:   nextPage,
		},
	}, nil
}

// FetchNotificationByThreadId scans pending and done todos looking for a
// matching ID. This is useful for refreshing bookmarked or session-marked
// notifications that may not appear in the regular notifications list.
//
// The GitLab Todos API has no GET /todos/:id endpoint (confirmed against
// gitlab.com/gitlab-org/api/client-go's TodosServiceInterface, which only
// exposes ListTodos/MarkTodoAsDone/MarkAllTodosAsDone) — this is therefore a
// bounded, best-effort client-side scan rather than a direct lookup. Returns
// (nil, nil), not an error, when the todo isn't found within the scan limit.
func FetchNotificationByThreadId(threadId string) (*NotificationData, error) {
	id, err := strconv.ParseInt(threadId, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid todo id %q: %w", threadId, err)
	}

	todos, err := getTodosService()
	if err != nil {
		return nil, err
	}

	log.Debug("Scanning todos for thread ID", "threadId", threadId)

	for _, state := range []string{"pending", "done"} {
		for page := int64(1); page <= notificationThreadScanMaxPages; page++ {
			opt := &gitlabapi.ListTodosOptions{
				ListOptions: gitlabapi.ListOptions{PerPage: 100, Page: page},
				State:       gitlabapi.Ptr(state),
			}
			items, resp, err := todos.ListTodos(opt)
			if err != nil {
				return nil, err
			}
			for _, item := range items {
				if item.ID == id {
					n := notificationFromTodo(item)
					return &n, nil
				}
			}
			if resp == nil || resp.NextPage == 0 {
				break
			}
		}
	}

	return nil, nil
}

// notificationFromTodo maps a GitLab Todo onto the internal NotificationData
// model. Target/Project/Author/CreatedAt are all pointers on Todo and may be
// nil (e.g. AlertManagement::Alert todos carry little of the Issue/MR-shaped
// data) — every access here is nil-checked.
func notificationFromTodo(todo *gitlabapi.Todo) NotificationData {
	n := NotificationData{
		Id:     strconv.FormatInt(todo.ID, 10),
		Unread: todo.State == "pending",
		Reason: string(todo.ActionName),
		Subject: NotificationSubject{
			Type: string(todo.TargetType),
		},
	}

	if todo.CreatedAt != nil {
		n.UpdatedAt = *todo.CreatedAt
	}

	if todo.Author != nil {
		n.Actor = todo.Author.Username
	}

	if todo.Project != nil {
		n.Repository = NotificationRepository{
			Id:       int(todo.Project.ID),
			Name:     todo.Project.Name,
			FullName: todo.Project.PathWithNamespace,
			HtmlUrl:  projectWebUrlFromTodo(todo),
		}
	}

	if todo.Target != nil {
		n.Subject.Title = todo.Target.Title
		n.Subject.Url = todo.Target.WebURL
	}

	return n
}

// projectWebUrlFromTodo derives the project's web base URL by trimming the
// "/-/issues/N" or "/-/merge_requests/N" suffix off the target's web URL.
// BasicProject (unlike BasicUser) has no WebURL field of its own, so this is
// the most direct way to recover it without a second API call.
func projectWebUrlFromTodo(todo *gitlabapi.Todo) string {
	if todo.Target == nil || todo.Target.WebURL == "" {
		return ""
	}
	if idx := strings.Index(todo.Target.WebURL, "/-/"); idx > 0 {
		return todo.Target.WebURL[:idx]
	}
	return ""
}

func MarkNotificationDone(threadId string) error {
	todos, err := getTodosService()
	if err != nil {
		return err
	}

	id, err := strconv.ParseInt(threadId, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid todo id %q: %w", threadId, err)
	}

	log.Debug("Marking todo as done", "id", id)
	_, err = todos.MarkTodoAsDone(id)
	if err != nil {
		return err
	}
	log.Info("Successfully marked todo as done", "id", id)
	return nil
}

// MarkNotificationRead marks the todo as done. GitLab's Todos model has no
// state separate from pending/done, so "mark as read" and "mark as done"
// converge to the same server call — a deliberate, documented behavior
// change from the GitHub-backed implementation.
func MarkNotificationRead(threadId string) error {
	return MarkNotificationDone(threadId)
}

// UnsubscribeFromThread has no equivalent in the GitLab Todos API: there is
// no per-thread subscription endpoint under /todos (confirmed against
// TodosServiceInterface). Returns an explicit error rather than silently
// no-op'ing, so callers surface the limitation instead of hiding it.
func UnsubscribeFromThread(threadId string) error {
	return errors.New("unsubscribe is not supported for GitLab todos")
}

// MarkAllNotificationsRead marks ALL pending todos for the current GitLab
// account as done — not just the notifications visible in this section's
// current query. This is a deliberate, documented behavior change from the
// GitHub-backed implementation, which only affected the notifications
// returned by the active filter.
func MarkAllNotificationsRead() error {
	todos, err := getTodosService()
	if err != nil {
		return err
	}

	log.Debug("Marking all todos as done")
	_, err = todos.MarkAllTodosAsDone()
	if err != nil {
		return err
	}
	log.Info("Successfully marked all todos as done")
	return nil
}

// PipelineEnrichment carries the best-effort pipeline status/URL used to
// enrich a build_failed todo.
type PipelineEnrichment struct {
	Status PipelineStatus
	Url    string
}

// FetchPipelineForTodo resolves pipeline status/URL for a build_failed todo,
// reusing FindPipelineForMR (Phase 2, internal/data/prapi.go). Best-effort:
// any failure to resolve a pipeline is logged at Debug and returns (nil, nil)
// rather than a fatal error — a todo must still render even when pipeline
// enrichment isn't available.
func FetchPipelineForTodo(projectPath string, mrIID int) (*PipelineEnrichment, error) {
	pipeline, err := FindPipelineForMR(projectPath, mrIID)
	if err != nil {
		log.Debug("no pipeline data available for todo", "err", err)
		return nil, nil
	}
	if pipeline.ID == 0 {
		return nil, nil
	}
	return &PipelineEnrichment{Status: pipeline.Status, Url: pipeline.WebURL}, nil
}
