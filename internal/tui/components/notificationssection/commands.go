package notificationssection

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/cli/browser"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/git"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/common"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
)

func init() {
	browser.Stdout = io.Discard
	browser.Stderr = io.Discard
}

var openURLFunc = browser.OpenURL

// markNotificationDoneFunc is the function used to mark a notification as done
// via the GitHub API. It is a variable so tests can override it.
var markNotificationDoneFunc = data.MarkNotificationDone

func (m *Model) markAsDone() tea.Cmd {
	notification := m.GetCurrNotification()
	if notification == nil {
		return nil
	}

	notificationId := notification.GetId()
	updatedAt := notification.Notification.UpdatedAt
	taskId := fmt.Sprintf("notification_done_%s", notificationId)
	task := context.Task{
		Id:           taskId,
		StartText:    "Marking notification as done",
		FinishedText: "Notification marked as done",
		State:        context.TaskStart,
		Error:        nil,
	}
	startCmd := m.Ctx.StartTask(task)
	return tea.Batch(startCmd, func() tea.Msg {
		err := markNotificationDoneFunc(notificationId)
		if err == nil {
			// Persist to done store so it stays hidden across sessions
			data.GetDoneStore().MarkDone(notificationId, updatedAt)
		}
		return constants.TaskFinishedMsg{
			SectionId:   m.Id,
			SectionType: SectionType,
			TaskId:      taskId,
			Err:         err,
			Msg: UpdateNotificationMsg{
				Id:        notificationId,
				IsRemoved: err == nil,
			},
		}
	})
}

// markAllAsDone marks ALL pending GitLab todos for the current account as
// done via a single batched call (data.MarkAllNotificationsRead, which calls
// Todos.MarkAllTodosAsDone) — not just the notifications currently loaded in
// m.Notifications. This is a deliberate behavior change from the previous
// GitHub-backed implementation, which only affected the visible notifications
// one at a time; GitLab's Todos API has no scoped "mark these N as done"
// batch endpoint, only a global one.
func (m *Model) markAllAsDone() tea.Cmd {
	if len(m.Notifications) == 0 {
		return nil
	}

	count := len(m.Notifications)
	taskId := "notification_done_all"
	task := context.Task{
		Id: taskId,
		StartText: fmt.Sprintf(
			"Marking all pending GitLab todos as done (not just the %d shown here)",
			count,
		),
		FinishedText: "All pending GitLab todos marked as done",
		State:        context.TaskStart,
		Error:        nil,
	}

	type doneEntry struct {
		id        string
		updatedAt time.Time
	}
	entries := make([]doneEntry, 0, count)
	for _, n := range m.Notifications {
		entries = append(entries, doneEntry{n.GetId(), n.Notification.UpdatedAt})
	}

	startCmd := m.Ctx.StartTask(task)
	return tea.Batch(startCmd, func() tea.Msg {
		if err := data.MarkAllNotificationsRead(); err != nil {
			return constants.TaskFinishedMsg{
				SectionId:   m.Id,
				SectionType: SectionType,
				TaskId:      taskId,
				Err:         err,
			}
		}

		// Persist the currently visible notifications to the done store so
		// they stay hidden across sessions even though the server-side call
		// above affected the whole account, not just these.
		doneStore := data.GetDoneStore()
		for _, e := range entries {
			doneStore.MarkDone(e.id, e.updatedAt)
		}

		// Clear all notifications after marking as done
		return constants.TaskFinishedMsg{
			SectionId:   m.Id,
			SectionType: SectionType,
			TaskId:      taskId,
			Err:         nil,
			Msg:         ClearAllNotificationsMsg{},
		}
	})
}

func (m *Model) markAllAsRead() tea.Cmd {
	taskId := "notification_read_all"
	task := context.Task{
		Id:           taskId,
		StartText:    "Marking all notifications as read",
		FinishedText: "All notifications marked as read",
		State:        context.TaskStart,
		Error:        nil,
	}
	startCmd := m.Ctx.StartTask(task)
	return tea.Batch(startCmd, func() tea.Msg {
		err := data.MarkAllNotificationsRead()
		if err != nil {
			return constants.TaskFinishedMsg{
				SectionId:   m.Id,
				SectionType: SectionType,
				TaskId:      taskId,
				Err:         err,
			}
		}

		// Update all notifications to read state
		return constants.TaskFinishedMsg{
			SectionId:   m.Id,
			SectionType: SectionType,
			TaskId:      taskId,
			Err:         nil,
			Msg:         MarkAllAsReadMsg{},
		}
	})
}

type (
	// RefetchNotificationsMsg signals that notifications should be refetched from the API
	RefetchNotificationsMsg struct{}
	// ClearAllNotificationsMsg signals that all notifications should be removed from the local list
	// This is sent after successfully marking all notifications as done
	ClearAllNotificationsMsg struct{}
	// MarkAllAsReadMsg signals that all notifications should be updated to read state in the UI
	// This is sent after successfully calling the mark-all-read API
	MarkAllAsReadMsg struct{}
)

func (m *Model) markAsRead() tea.Cmd {
	notification := m.GetCurrNotification()
	if notification == nil {
		return nil
	}

	notificationId := notification.GetId()
	taskId := fmt.Sprintf("notification_read_%s", notificationId)
	task := context.Task{
		Id:           taskId,
		StartText:    "Marking notification as read",
		FinishedText: "Notification marked as read",
		State:        context.TaskStart,
		Error:        nil,
	}
	startCmd := m.Ctx.StartTask(task)
	return tea.Batch(startCmd, func() tea.Msg {
		err := data.MarkNotificationRead(notificationId)
		return constants.TaskFinishedMsg{
			SectionId:   m.Id,
			SectionType: SectionType,
			TaskId:      taskId,
			Err:         err,
			Msg: UpdateNotificationReadStateMsg{
				Id:     notificationId,
				Unread: false,
			},
		}
	})
}

func (m *Model) unsubscribe() tea.Cmd {
	notification := m.GetCurrNotification()
	if notification == nil {
		return nil
	}

	notificationId := notification.GetId()
	taskId := fmt.Sprintf("notification_unsubscribe_%s", notificationId)
	task := context.Task{
		Id:           taskId,
		StartText:    "Unsubscribing from thread",
		FinishedText: "Unsubscribed from thread",
		State:        context.TaskStart,
		Error:        nil,
	}
	startCmd := m.Ctx.StartTask(task)
	return tea.Batch(startCmd, func() tea.Msg {
		err := data.UnsubscribeFromThread(notificationId)
		return constants.TaskFinishedMsg{
			SectionId:   m.Id,
			SectionType: SectionType,
			TaskId:      taskId,
			Err:         err,
			Msg: UnsubscribedMsg{
				Id: notificationId,
			},
		}
	})
}

// UnsubscribedMsg is sent when a notification thread is unsubscribed
type UnsubscribedMsg struct {
	Id string
}

// UpdateNotificationReadStateMsg is sent when a notification's read state changes
type UpdateNotificationReadStateMsg struct {
	Id     string
	Unread bool
}

func isOpenableURL(rawUrl string) bool {
	u, err := url.Parse(rawUrl)
	if err != nil {
		return false
	}
	// Only hand http/https URLs to the browser opener; other schemes
	// (file://, javascript:, ftp://, custom app handlers) should never be
	// auto-launched from a notification. url.Parse lowercases the scheme.
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// openInBrowser marks the current notification as read and opens it in the browser
func (m *Model) openInBrowser() tea.Cmd {
	notification := m.GetCurrNotification()
	if notification == nil {
		return nil
	}

	notificationId := notification.GetId()
	notificationUrl := notification.GetUrl()

	if !isOpenableURL(notificationUrl) {
		return func() tea.Msg {
			return constants.ErrMsg{Err: errors.New("notification has no openable url")}
		}
	}

	return tea.Batch(
		func() tea.Msg {
			_ = data.MarkNotificationRead(notificationId)
			return UpdateNotificationReadStateMsg{
				Id:     notificationId,
				Unread: false,
			}
		},
		func() tea.Msg {
			err := openURLFunc(notificationUrl)
			if err != nil {
				return constants.ErrMsg{Err: err}
			}
			return nil
		},
	)
}

// CheckoutPR checks out a PR. This is a standalone function that can be called
// from ui.go with the PR details from the notification view.
func CheckoutPR(ctx *context.ProgramContext, prNumber int, repoName string) (tea.Cmd, error) {
	repoPath, ok := common.GetRepoLocalPath(repoName, ctx.Config.RepoPaths)
	if !ok {
		return nil, errors.New(
			"local path to repo not specified, set one in your config.yml under repoPaths",
		)
	}

	taskId := fmt.Sprintf("checkout_%d", prNumber)
	task := context.Task{
		Id:           taskId,
		StartText:    fmt.Sprintf("Checking out PR #%d", prNumber),
		FinishedText: fmt.Sprintf("PR #%d has been checked out at %s", prNumber, repoPath),
		State:        context.TaskStart,
		Error:        nil,
	}
	startCmd := ctx.StartTask(task)
	return tea.Batch(startCmd, func() tea.Msg {
		userHomeDir, _ := os.UserHomeDir()
		if strings.HasPrefix(repoPath, "~") {
			repoPath = strings.Replace(repoPath, "~", userHomeDir, 1)
		}

		err := git.CheckoutMergeRequest(repoPath, prNumber)
		return constants.TaskFinishedMsg{TaskId: taskId, Err: err}
	}), nil
}
