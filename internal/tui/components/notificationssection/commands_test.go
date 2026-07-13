package notificationssection

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	gitlabapi "gitlab.com/gitlab-org/api/client-go"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/notificationrow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

// noopStartTask is a stub that returns nil for testing
func noopStartTask(task context.Task) tea.Cmd {
	return nil
}

func TestCheckoutPR(t *testing.T) {
	tests := []struct {
		name      string
		prNumber  int
		repoName  string
		repoPaths map[string]string
		wantErr   bool
		wantNil   bool
	}{
		{
			name:      "returns error when repo path not configured",
			prNumber:  123,
			repoName:  "owner/repo",
			repoPaths: map[string]string{},
			wantErr:   true,
			wantNil:   true,
		},
		{
			name:     "returns command when repo path is configured",
			prNumber: 123,
			repoName: "owner/repo",
			repoPaths: map[string]string{
				"owner/repo": "/path/to/repo",
			},
			wantErr: false,
			wantNil: false,
		},
		{
			name:     "returns command with tilde path",
			prNumber: 456,
			repoName: "my-org/my-repo",
			repoPaths: map[string]string{
				"my-org/my-repo": "~/projects/my-repo",
			},
			wantErr: false,
			wantNil: false,
		},
		{
			name:      "returns error for unconfigured repo even with other repos configured",
			prNumber:  789,
			repoName:  "other/repo",
			repoPaths: map[string]string{"owner/repo": "/path/to/repo"},
			wantErr:   true,
			wantNil:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &context.ProgramContext{
				Config: &config.Config{
					RepoPaths: tt.repoPaths,
				},
				StartTask: noopStartTask,
			}

			cmd, err := CheckoutPR(ctx, tt.prNumber, tt.repoName)

			if tt.wantErr && err == nil {
				t.Errorf("CheckoutPR() error = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("CheckoutPR() error = %v, want nil", err)
			}
			if tt.wantNil && cmd != nil {
				t.Errorf("CheckoutPR() returned non-nil cmd, want nil")
			}
			if !tt.wantNil && cmd == nil {
				t.Errorf("CheckoutPR() returned nil cmd, want non-nil")
			}
		})
	}
}

func TestCheckoutPRErrorMessage(t *testing.T) {
	ctx := &context.ProgramContext{
		Config: &config.Config{
			RepoPaths: map[string]string{},
		},
		StartTask: noopStartTask,
	}

	_, err := CheckoutPR(ctx, 123, "owner/repo")

	if err == nil {
		t.Fatal("CheckoutPR() expected error, got nil")
	}

	expectedMsg := "local path to repo not specified, set one in your config.yml under repoPaths"
	if err.Error() != expectedMsg {
		t.Errorf("CheckoutPR() error = %q, want %q", err.Error(), expectedMsg)
	}
}

// TestMarkAsDoneStoresCorrectTimestamp is a regression test for a
// pointer-aliasing bug that occurred in markAsDone().
//
// GetCurrNotification() returns &m.Notifications[idx], a pointer into the
// Notifications slice. When the closure later dereferences this pointer to
// read UpdatedAt, the slice may have been modified (element removed via
// append), causing the pointer to reference a different notification's data.
//
// The fix captures UpdatedAt by value before the closure. This test verifies
// that the correct timestamp reaches the DoneStore even when the slice is
// modified between command creation and execution.
func TestMarkAsDoneStoresCorrectTimestamp(t *testing.T) {
	// Mock the API call to succeed without network access.
	origFunc := markNotificationDoneFunc
	markNotificationDoneFunc = func(string) error { return nil }
	defer func() { markNotificationDoneFunc = origFunc }()

	// Set up a DoneStore backed by a temp file so we don't touch real state.
	tempDir, err := os.MkdirTemp("", "gh-dash-markdone-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	store := data.NewDoneStoreForTesting(filepath.Join(tempDir, "done.json"))
	restoreStore := data.OverrideDoneStoreForTesting(store)
	defer restoreStore()

	t1 := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 2, 20, 15, 30, 0, 0, time.UTC)
	t3 := time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)

	// Build a minimal Model. A zero-value Table has cursor at index 0,
	// so GetCurrNotification() returns &m.Notifications[0].
	m := Model{
		Notifications: []notificationrow.Data{
			{Notification: data.NotificationData{Id: "notif-A", UpdatedAt: t1}},
			{Notification: data.NotificationData{Id: "notif-B", UpdatedAt: t2}},
			{Notification: data.NotificationData{Id: "notif-C", UpdatedAt: t3}},
		},
		sessionMarkedDone: make(map[string]bool),
		sessionMarkedRead: make(map[string]bool),
	}
	m.Ctx = &context.ProgramContext{
		StartTask: noopStartTask,
	}

	// Step 1: Call markAsDone(). This captures notif-A's ID and UpdatedAt
	// by value, before the closure.
	cmd := m.markAsDone()
	if cmd == nil {
		t.Fatal("markAsDone() returned nil cmd")
	}

	// Step 2: Simulate the race — remove notif-A from the slice.
	// This shifts notif-B into position 0 and notif-C into position 1.
	// If the closure had captured a pointer instead of a value, it would
	// now read notif-B's UpdatedAt (t2) instead of notif-A's (t1).
	m.Notifications = append(m.Notifications[:0], m.Notifications[1:]...)

	// Step 3: Execute the command. tea.Batch returns a BatchMsg containing
	// the inner cmds; execute each one.
	batchMsg := cmd()
	if cmds, ok := batchMsg.(tea.BatchMsg); ok {
		for _, c := range cmds {
			if c != nil {
				c()
			}
		}
	}

	// Step 4: Verify the DoneStore received notif-A's original timestamp (t1),
	// not notif-B's (t2), which is what the shifted pointer would have read.
	if !store.IsDone("notif-A", t1) {
		t.Error("DoneStore should have notif-A marked done at t1")
	}
	// If the bug were present, t2 would have been stored instead of t1.
	// In that case, IsDone("notif-A", t1) would still return true (t1 <= t2),
	// but IsDone with a time between t1 and t2 would incorrectly return true.
	// Use a more precise check: mark done at t1 means t1+1s should resurface
	// only if the stored timestamp is exactly t1.
	justAfterT1 := t1.Add(1 * time.Second)
	if store.IsDone("notif-A", justAfterT1) {
		t.Error(
			"notif-A should resurface for activity after t1 (stored timestamp should be exactly t1)",
		)
	}
	// That is the critical assertion: if the pointer-aliasing bug were present,
	// t2 would be stored, and activity at justAfterT1 would _not_ resurface
	// (because justAfterT1 < t2). The test would fail here.
}

func TestUpdateNotificationKeepsCursorOnNewLastItem(t *testing.T) {
	cfg, err := config.ParseConfig(config.Location{
		ConfigFlag:       "../../../config/testdata/test-config.yml",
		SkipGlobalConfig: true,
	})
	if err != nil {
		t.Fatalf("Failed to parse config: %v", err)
	}

	ctx := &context.ProgramContext{
		Config: &cfg,
	}
	ctx.Theme = theme.ParseTheme(ctx.Config)
	ctx.Styles = context.InitStyles(ctx.Theme)

	m := NewModel(0, ctx, config.NotificationsSectionConfig{}, time.Now())
	m.Notifications = []notificationrow.Data{
		{Notification: data.NotificationData{Id: "notif-A"}},
		{Notification: data.NotificationData{Id: "notif-B"}},
		{Notification: data.NotificationData{Id: "notif-C"}},
	}
	m.TotalCount = len(m.Notifications)
	m.Table.SetRows(m.BuildRows())

	m.LastItem()
	if got := m.CurrRow(); got != 2 {
		t.Fatalf("CurrRow() = %d, want 2 before removal", got)
	}

	m.Update(UpdateNotificationMsg{
		Id:        "notif-C",
		IsRemoved: true,
	})

	if got := m.CurrRow(); got != 1 {
		t.Fatalf("CurrRow() = %d, want 1 after removing the last notification", got)
	}

	current := m.GetCurrNotification()
	if current == nil {
		t.Fatal("GetCurrNotification() returned nil")
	}

	if got := current.GetId(); got != "notif-B" {
		t.Fatalf("GetCurrNotification().GetId() = %q, want %q", got, "notif-B")
	}
}

func newModelWithCurrentPRNotification(id, url string) Model {
	return Model{
		Notifications: []notificationrow.Data{
			{
				Notification: data.NotificationData{
					Id: id,
					Subject: data.NotificationSubject{
						Title: "Test PR",
						Url:   url,
						Type:  "PullRequest",
					},
					Repository: data.NotificationRepository{
						FullName: "owner/repo",
						HtmlUrl:  "https://github.com/owner/repo",
					},
				},
			},
		},
	}
}

func openURLSubCmd(t *testing.T, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	if cmd == nil {
		t.Fatal("openInBrowser() returned a nil cmd")
	}

	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected tea.BatchMsg from openInBrowser(), got %T", msg)
	}
	if len(batch) != 2 {
		t.Fatalf(
			"expected openInBrowser() batch to contain exactly 2 commands (mark-as-read, open-in-browser), got %d",
			len(batch),
		)
	}
	return batch[1]
}

func TestOpenInBrowser_InvokesOpenURLFuncWithCurrentNotificationURL(t *testing.T) {
	originalOpenURLFunc := openURLFunc
	defer func() { openURLFunc = originalOpenURLFunc }()

	var callCount int
	var gotURL string
	openURLFunc = func(url string) error {
		callCount++
		gotURL = url
		return nil
	}

	m := newModelWithCurrentPRNotification(
		"notif-open-1",
		"https://api.github.com/repos/owner/repo/pulls/123",
	)

	browserCmd := openURLSubCmd(t, m.openInBrowser())
	msg := browserCmd()

	if callCount != 1 {
		t.Fatalf("openURLFunc call count = %d, want 1", callCount)
	}

	wantURL := "https://github.com/owner/repo/pull/123"
	if gotURL != wantURL {
		t.Fatalf("openURLFunc called with URL = %q, want %q", gotURL, wantURL)
	}

	if msg != nil {
		t.Fatalf("open-in-browser command message = %v, want nil on success", msg)
	}
}

func TestOpenInBrowser_PropagatesOpenURLFuncError(t *testing.T) {
	originalOpenURLFunc := openURLFunc
	defer func() { openURLFunc = originalOpenURLFunc }()

	wantErr := errors.New("xdg-open: command not found")
	var callCount int
	openURLFunc = func(url string) error {
		callCount++
		return wantErr
	}

	m := newModelWithCurrentPRNotification(
		"notif-open-2",
		"https://api.github.com/repos/owner/repo/pulls/456",
	)

	browserCmd := openURLSubCmd(t, m.openInBrowser())
	msg := browserCmd()

	if callCount != 1 {
		t.Fatalf("openURLFunc call count = %d, want 1", callCount)
	}

	errMsg, ok := msg.(constants.ErrMsg)
	if !ok {
		t.Fatalf("expected constants.ErrMsg, got %T (%v)", msg, msg)
	}
	if !errors.Is(errMsg.Err, wantErr) {
		t.Fatalf("ErrMsg.Err = %v, want %v", errMsg.Err, wantErr)
	}
}

func newModelWithCurrentNotification(
	id string,
	subject data.NotificationSubject,
	repo data.NotificationRepository,
) Model {
	return Model{
		Notifications: []notificationrow.Data{
			{
				Notification: data.NotificationData{
					Id:         id,
					Subject:    subject,
					Repository: repo,
				},
			},
		},
	}
}

func newCountingMockRESTClient(t *testing.T) (*gitlabapi.Client, *int) {
	t.Helper()
	callCount := new(int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*callCount++
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	client, err := gitlabapi.NewClient(
		"test-token",
		gitlabapi.WithBaseURL(server.URL),
		gitlabapi.WithoutRetries(),
	)
	if err != nil {
		t.Fatalf("failed to build mock gitlab client: %v", err)
	}
	return client, callCount
}

func TestOpenInBrowser_UnopenableURLDoesNotOpenOrMarkAsRead(t *testing.T) {
	tests := []struct {
		name    string
		subject data.NotificationSubject
		repo    data.NotificationRepository
		wantUrl string
	}{
		{
			name: "empty url",
			subject: data.NotificationSubject{
				Title: "Test",
				Type:  "MergeRequest",
			},
			repo:    data.NotificationRepository{FullName: "group/proj"},
			wantUrl: "",
		},
		{
			name: "relative url without scheme or host",
			subject: data.NotificationSubject{
				Title: "Test",
				Type:  "PullRequest",
				Url:   "https://api.github.com/repos/owner/repo/pulls/123",
			},
			repo:    data.NotificationRepository{FullName: "owner/repo"},
			wantUrl: "/pull/123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newModelWithCurrentNotification("notif-unopenable", tt.subject, tt.repo)

			if got := m.Notifications[0].GetUrl(); got != tt.wantUrl {
				t.Fatalf("test fixture invalid: GetUrl() = %q, want %q", got, tt.wantUrl)
			}

			originalOpenURLFunc := openURLFunc
			defer func() { openURLFunc = originalOpenURLFunc }()
			var openURLCallCount int
			openURLFunc = func(string) error {
				openURLCallCount++
				return nil
			}

			mockClient, restCallCount := newCountingMockRESTClient(t)
			data.SetRESTClient(mockClient)
			defer data.SetRESTClient(nil)

			cmd := m.openInBrowser()
			if cmd == nil {
				t.Fatal("openInBrowser() returned a nil cmd")
			}

			msg := cmd()

			if _, isBatch := msg.(tea.BatchMsg); isBatch {
				t.Fatalf(
					"openInBrowser() for an unopenable url should not run the mark-as-read/open batch, got tea.BatchMsg",
				)
			}

			errMsg, ok := msg.(constants.ErrMsg)
			if !ok {
				t.Fatalf("expected constants.ErrMsg, got %T (%v)", msg, msg)
			}
			if errMsg.Err == nil {
				t.Fatal("expected constants.ErrMsg.Err to be a non-nil error")
			}

			if openURLCallCount != 0 {
				t.Fatalf("openURLFunc call count = %d, want 0", openURLCallCount)
			}
			if *restCallCount != 0 {
				t.Fatalf("mark-as-read HTTP call count = %d, want 0", *restCallCount)
			}
		})
	}
}
