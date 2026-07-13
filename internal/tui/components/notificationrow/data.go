package notificationrow

import (
	"fmt"
	"strings"
	"time"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
)

// PR/Issue state constants from GitHub API
const (
	StateOpen   = "OPEN"
	StateClosed = "CLOSED"
	StateMerged = "MERGED"
)

type Data struct {
	Notification        data.NotificationData
	NewCommentsCount    int    // Number of new comments since last read
	SubjectState        string // State of the PR/Issue (OPEN, CLOSED, MERGED)
	IsDraft             bool   // Whether PR is a draft
	Actor               string // Username of the user who triggered the notification
	ActivityDescription string // Human-readable description of the activity (e.g., "@user commented on this PR")
	ResolvedUrl         string // Async-resolved URL (e.g., for CheckSuite -> specific workflow run)
}

func (d Data) GetTitle() string {
	// Sanitize title: remove carriage returns and other control characters
	// that can corrupt terminal rendering (e.g., GitHub sometimes returns
	// titles with trailing \r characters)
	title := d.Notification.Subject.Title
	title = strings.ReplaceAll(title, "\r", "")
	title = strings.ReplaceAll(title, "\n", " ")
	return strings.TrimSpace(title)
}

func (d Data) GetRepoNameWithOwner() string {
	return d.Notification.Repository.FullName
}

func (d Data) GetNumber() int {
	return int(d.Notification.Subject.IID)
}

// isApiFollowUpUrl reports whether rawUrl is a REST API URL rather than a
// ready-to-use web URL. GitLab's Todo.Target.WebURL is always a web URL and
// never contains "/repos/"; the check exists for legacy notification data
// shaped like GitHub's API responses, which do use that segment.
func isApiFollowUpUrl(rawUrl string) bool {
	return strings.Contains(rawUrl, "/repos/")
}

func (d Data) GetUrl() string {
	subject := d.Notification.Subject
	baseUrl := repoBaseUrl(d.Notification.Repository)

	if subject.Url != "" && !isApiFollowUpUrl(subject.Url) {
		return subject.Url
	}

	switch subject.Type {
	case data.SubjectTypePullRequest:
		return fmt.Sprintf("%s/pull/%s", baseUrl, extractNumberFromUrl(subject.Url))
	case data.SubjectTypeIssue:
		return fmt.Sprintf("%s/issues/%s", baseUrl, extractNumberFromUrl(subject.Url))
	case data.SubjectTypeDiscussion:
		num := extractNumberFromUrl(subject.Url)
		if num != "" {
			return fmt.Sprintf("%s/discussions/%s", baseUrl, num)
		}
		return fmt.Sprintf("%s/discussions", baseUrl)
	case data.SubjectTypeRelease:
		return fmt.Sprintf("%s/releases", baseUrl)
	case data.SubjectTypeCommit:
		return fmt.Sprintf("%s/commits", baseUrl)
	case data.SubjectTypeCheckSuite:
		if d.ResolvedUrl != "" {
			return d.ResolvedUrl
		}
		return fmt.Sprintf("%s/actions", baseUrl)
	case data.SubjectTypeMergeRequest:
		return baseUrl
	default:
		return baseUrl
	}
}

// repoBaseUrl returns the base web URL for a repository/project, taken
// directly from Repository.HtmlUrl (populated from the GitLab project's
// namespace path, or a GitHub Enterprise host for legacy data). Returns ""
// when HtmlUrl is empty rather than guessing a host.
func repoBaseUrl(repo data.NotificationRepository) string {
	return strings.TrimRight(repo.HtmlUrl, "/")
}

func (d Data) GetUpdatedAt() time.Time {
	return d.Notification.UpdatedAt
}

func (d Data) GetCreatedAt() time.Time {
	return d.Notification.UpdatedAt
}

func (d Data) GetId() string {
	return d.Notification.Id
}

func (d Data) GetSubjectType() string {
	return d.Notification.Subject.Type
}

func (d Data) GetReason() string {
	return d.Notification.Reason
}

func (d Data) IsUnread() bool {
	return d.Notification.Unread
}

func (d Data) GetLatestCommentUrl() string {
	return d.Notification.Subject.LatestCommentUrl
}

// extractNumberFromUrl extracts the last path segment (typically a number) from an API URL
func extractNumberFromUrl(apiUrl string) string {
	if apiUrl == "" {
		return ""
	}
	for i := len(apiUrl) - 1; i >= 0; i-- {
		if apiUrl[i] == '/' {
			return apiUrl[i+1:]
		}
	}
	return ""
}

// GenerateActivityDescription creates a human-readable description of the notification activity
func GenerateActivityDescription(reason, subjectType, actor string) string {
	switch reason {
	case data.ReasonComment:
		if actor != "" {
			switch subjectType {
			case data.SubjectTypePullRequest:
				return fmt.Sprintf("@%s commented on this pull request", actor)
			case data.SubjectTypeIssue:
				return fmt.Sprintf("@%s commented on this issue", actor)
			default:
				return fmt.Sprintf("@%s commented", actor)
			}
		}
		return "New comment"
	case data.ReasonReviewRequested:
		if actor != "" {
			return fmt.Sprintf("@%s requested your review", actor)
		}
		return "Review requested"
	case data.ReasonMention:
		if actor != "" {
			return fmt.Sprintf("@%s mentioned you", actor)
		}
		return "You were mentioned"
	case data.ReasonAuthor:
		return "Activity on your thread"
	case data.ReasonAssign:
		return "You were assigned"
	case data.ReasonStateChange:
		switch subjectType {
		case data.SubjectTypePullRequest:
			return "Pull request state changed"
		case data.SubjectTypeIssue:
			return "Issue state changed"
		default:
			return "State changed"
		}
	case data.ReasonCIActivity:
		return "CI activity"
	case data.ReasonSubscribed:
		if actor != "" {
			switch subjectType {
			case data.SubjectTypePullRequest:
				return fmt.Sprintf("@%s commented on this pull request", actor)
			case data.SubjectTypeIssue:
				return fmt.Sprintf("@%s commented on this issue", actor)
			default:
				return "Activity on subscribed thread"
			}
		}
		return "Activity on subscribed thread"
	case data.ReasonTeamMention:
		return "Your team was mentioned"
	case data.ReasonSecurityAlert:
		return "Security vulnerability detected"
	case data.ReasonAssigned:
		return "You were assigned"
	case data.ReasonMentioned:
		if actor != "" {
			return fmt.Sprintf("@%s mentioned you", actor)
		}
		return "You were mentioned"
	case data.ReasonBuildFailed:
		return "Pipeline failed"
	case data.ReasonMarked:
		return "Manually marked as a to-do"
	case data.ReasonApprovalRequired:
		return "Your approval is required"
	case data.ReasonDirectlyAddressed:
		if actor != "" {
			return fmt.Sprintf("@%s addressed you directly", actor)
		}
		return "You were directly addressed"
	default:
		if actor != "" {
			return fmt.Sprintf("@%s triggered this notification", actor)
		}
		return ""
	}
}
