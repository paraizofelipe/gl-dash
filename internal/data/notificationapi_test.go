package data

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	gh "github.com/cli/go-gh/v2/pkg/api"
	"github.com/stretchr/testify/require"
)

func TestFindBestWorkflowRunMatch(t *testing.T) {
	baseTime := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name                  string
		runs                  []WorkflowRun
		notificationUpdatedAt time.Time
		expectedId            int64
		expectedNil           bool
	}{
		{
			name:                  "empty runs returns nil",
			runs:                  []WorkflowRun{},
			notificationUpdatedAt: baseTime,
			expectedNil:           true,
		},
		{
			name: "single run within time window is selected",
			runs: []WorkflowRun{
				{
					Id:        1,
					Name:      "CI",
					HtmlUrl:   "https://github.com/owner/repo/actions/runs/1",
					UpdatedAt: baseTime.Add(-5 * time.Minute),
				},
			},
			notificationUpdatedAt: baseTime,
			expectedId:            1,
		},
		{
			name: "closest run within time window is selected",
			runs: []WorkflowRun{
				{
					Id:        1,
					Name:      "CI",
					HtmlUrl:   "https://github.com/owner/repo/actions/runs/1",
					UpdatedAt: baseTime.Add(-30 * time.Minute),
				},
				{
					Id:        2,
					Name:      "CI",
					HtmlUrl:   "https://github.com/owner/repo/actions/runs/2",
					UpdatedAt: baseTime.Add(-5 * time.Minute),
				},
				{
					Id:        3,
					Name:      "CI",
					HtmlUrl:   "https://github.com/owner/repo/actions/runs/3",
					UpdatedAt: baseTime.Add(-15 * time.Minute),
				},
			},
			notificationUpdatedAt: baseTime,
			expectedId:            2, // 5 minutes is closest
		},
		{
			name: "run slightly after notification time is selected",
			runs: []WorkflowRun{
				{
					Id:        1,
					Name:      "CI",
					HtmlUrl:   "https://github.com/owner/repo/actions/runs/1",
					UpdatedAt: baseTime.Add(2 * time.Minute),
				},
				{
					Id:        2,
					Name:      "CI",
					HtmlUrl:   "https://github.com/owner/repo/actions/runs/2",
					UpdatedAt: baseTime.Add(-10 * time.Minute),
				},
			},
			notificationUpdatedAt: baseTime,
			expectedId:            1, // 2 minutes after is closer than 10 minutes before
		},
		{
			name: "no runs within time window falls back to first run",
			runs: []WorkflowRun{
				{
					Id:        1,
					Name:      "CI",
					HtmlUrl:   "https://github.com/owner/repo/actions/runs/1",
					UpdatedAt: baseTime.Add(-2 * time.Hour),
				},
				{
					Id:        2,
					Name:      "CI",
					HtmlUrl:   "https://github.com/owner/repo/actions/runs/2",
					UpdatedAt: baseTime.Add(-3 * time.Hour),
				},
			},
			notificationUpdatedAt: baseTime,
			expectedId:            1, // Falls back to first (most recent) run
		},
		{
			name: "exact time match is selected",
			runs: []WorkflowRun{
				{
					Id:        1,
					Name:      "CI",
					HtmlUrl:   "https://github.com/owner/repo/actions/runs/1",
					UpdatedAt: baseTime.Add(-10 * time.Minute),
				},
				{
					Id:        2,
					Name:      "CI",
					HtmlUrl:   "https://github.com/owner/repo/actions/runs/2",
					UpdatedAt: baseTime,
				},
				{
					Id:        3,
					Name:      "CI",
					HtmlUrl:   "https://github.com/owner/repo/actions/runs/3",
					UpdatedAt: baseTime.Add(-5 * time.Minute),
				},
			},
			notificationUpdatedAt: baseTime,
			expectedId:            2, // Exact match
		},
		{
			name: "run at edge of time window (59 minutes) is selected",
			runs: []WorkflowRun{
				{
					Id:        1,
					Name:      "CI",
					HtmlUrl:   "https://github.com/owner/repo/actions/runs/1",
					UpdatedAt: baseTime.Add(-59 * time.Minute),
				},
				{
					Id:        2,
					Name:      "CI",
					HtmlUrl:   "https://github.com/owner/repo/actions/runs/2",
					UpdatedAt: baseTime.Add(-61 * time.Minute),
				},
			},
			notificationUpdatedAt: baseTime,
			expectedId:            1, // 59 minutes is within the 1 hour window
		},
		{
			name: "notification time before all runs still finds closest",
			runs: []WorkflowRun{
				{
					Id:        1,
					Name:      "CI",
					HtmlUrl:   "https://github.com/owner/repo/actions/runs/1",
					UpdatedAt: baseTime.Add(30 * time.Minute),
				},
				{
					Id:        2,
					Name:      "CI",
					HtmlUrl:   "https://github.com/owner/repo/actions/runs/2",
					UpdatedAt: baseTime.Add(10 * time.Minute),
				},
			},
			notificationUpdatedAt: baseTime,
			expectedId:            2, // 10 minutes after is closer
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FindBestWorkflowRunMatch(tt.runs, tt.notificationUpdatedAt)

			if tt.expectedNil {
				if result != nil {
					t.Errorf("FindBestWorkflowRunMatch() = %v, want nil", result)
				}
				return
			}

			if result == nil {
				t.Errorf("FindBestWorkflowRunMatch() = nil, want run with id %d", tt.expectedId)
				return
			}

			if result.Id != tt.expectedId {
				t.Errorf(
					"FindBestWorkflowRunMatch() returned run id %d, want %d",
					result.Id,
					tt.expectedId,
				)
			}
		})
	}
}

func TestNotificationDataGetUrl(t *testing.T) {
	tests := []struct {
		name     string
		data     NotificationData
		expected string
	}{
		{
			name: "uses HtmlUrl from repository",
			data: NotificationData{
				Repository: NotificationRepository{
					FullName: "owner/repo",
					HtmlUrl:  "https://github.com/owner/repo",
				},
			},
			expected: "https://github.com/owner/repo",
		},
		{
			name: "uses GHE host from HtmlUrl",
			data: NotificationData{
				Repository: NotificationRepository{
					FullName: "org/repo",
					HtmlUrl:  "https://ghe.company.com/org/repo",
				},
			},
			expected: "https://ghe.company.com/org/repo",
		},
		{
			name: "trims trailing slash from HtmlUrl",
			data: NotificationData{
				Repository: NotificationRepository{
					FullName: "org/repo",
					HtmlUrl:  "https://ghe.company.com/org/repo/",
				},
			},
			expected: "https://ghe.company.com/org/repo",
		},
		{
			name: "falls back to github.com when HtmlUrl is empty",
			data: NotificationData{
				Repository: NotificationRepository{
					FullName: "owner/repo",
					HtmlUrl:  "",
				},
			},
			expected: "https://github.com/owner/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.data.GetUrl()
			if result != tt.expected {
				t.Errorf("GetUrl() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func isolateGitHubAuthEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GH_CONFIG_DIR", t.TempDir())
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_ENTERPRISE_TOKEN", "")
	t.Setenv("GITHUB_ENTERPRISE_TOKEN", "")
	t.Setenv("GH_HOST", "")
	t.Setenv("GH_PATH", filepath.Join(t.TempDir(), "gh-binary-not-found"))
}

func TestGetRESTClient_ConcurrentAccess(t *testing.T) {
	original := restClient
	defer func() { restClient = original }()
	restClient = nil

	isolateGitHubAuthEnv(t)

	const n = 50
	var wg sync.WaitGroup
	results := make([]*gh.RESTClient, n)
	errs := make([]error, n)
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = getRESTClient()
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
