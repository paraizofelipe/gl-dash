package issueview

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/issuerow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

func newTestModelForCheckout(t *testing.T, repoPaths map[string]string) Model {
	t.Helper()
	cfg, err := config.ParseConfig(config.Location{
		ConfigFlag:       "../../../config/testdata/test-config.yml",
		SkipGlobalConfig: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if repoPaths == nil {
		repoPaths = map[string]string{}
	}
	cfg.RepoPaths = repoPaths
	thm := theme.ParseTheme(&cfg)
	ctx := &context.ProgramContext{
		Config:    &cfg,
		Theme:     thm,
		Styles:    context.InitStyles(thm),
		StartTask: func(task context.Task) tea.Cmd { return nil },
	}

	return NewModel(ctx)
}

func runCheckoutCmd(t *testing.T, cmd tea.Cmd) constants.TaskFinishedMsg {
	t.Helper()
	require.NotNil(t, cmd)

	msg := cmd()
	if finished, ok := msg.(constants.TaskFinishedMsg); ok {
		return finished
	}

	batch, ok := msg.(tea.BatchMsg)
	require.True(t, ok, "expected constants.TaskFinishedMsg or tea.BatchMsg, got %T", msg)

	for _, sub := range batch {
		if sub == nil {
			continue
		}
		if finished, ok := sub().(constants.TaskFinishedMsg); ok {
			return finished
		}
	}

	t.Fatal("checkout command did not produce a constants.TaskFinishedMsg")
	return constants.TaskFinishedMsg{}
}

func runGitCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(
		os.Environ(),
		"GIT_AUTHOR_NAME=tester",
		"GIT_AUTHOR_EMAIL=tester@example.com",
		"GIT_COMMITTER_NAME=tester",
		"GIT_COMMITTER_EMAIL=tester@example.com",
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %s failed: %s", strings.Join(args, " "), out)
}

func currentBranch(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}

func initLocalCloneWithOrigin(t *testing.T) string {
	t.Helper()
	remote := t.TempDir()
	runGitCommand(t, remote, "init", "-q", "-b", "main")
	runGitCommand(t, remote, "config", "commit.gpgsign", "false")
	require.NoError(t, os.WriteFile(filepath.Join(remote, "README.md"), []byte("hi"), 0o644))
	runGitCommand(t, remote, "add", "README.md")
	runGitCommand(t, remote, "commit", "-q", "-m", "init")

	local := filepath.Join(t.TempDir(), "local")
	runGitCommand(t, remote, "clone", "-q", remote, local)
	runGitCommand(t, local, "config", "commit.gpgsign", "false")
	return local
}

func TestCheckout_NoIssueSelected_ReturnsError(t *testing.T) {
	m := newTestModelForCheckout(t, nil)
	m.issue = nil

	cmd, err := m.Checkout()

	require.Nil(t, cmd)
	require.EqualError(t, err, "no issue selected")
}

func TestCheckout_RepoPathNotConfigured_ReturnsError(t *testing.T) {
	m := newTestModelForCheckout(t, map[string]string{})
	m.issue = &issuerow.Issue{
		Ctx: m.ctx,
		Data: data.IssueData{
			Number:     7,
			Repository: data.Repository{NameWithOwner: "owner/repo"},
		},
	}

	cmd, err := m.Checkout()

	require.Nil(t, cmd)
	require.EqualError(
		t,
		err,
		"local path to repo not specified, set one in your config.yml under repoPaths",
	)
}

func TestCheckout_ChecksOutDeterministicIssueBranchViaLocalFallback(t *testing.T) {
	repoPath := initLocalCloneWithOrigin(t)
	m := newTestModelForCheckout(t, map[string]string{"owner/repo": repoPath})
	m.issue = &issuerow.Issue{
		Ctx: m.ctx,
		Data: data.IssueData{
			Number:     7,
			Repository: data.Repository{NameWithOwner: "owner/repo"},
		},
	}

	cmd, err := m.Checkout()
	require.NoError(t, err)

	finished := runCheckoutCmd(t, cmd)

	require.NoError(t, finished.Err)
	require.Equal(t, "7-issue", currentBranch(t, repoPath))
}

func TestCheckout_TaskIdentifiersIncludeIssueNumberAndRepoPath(t *testing.T) {
	repoPath := initLocalCloneWithOrigin(t)
	var capturedTask context.Task
	m := newTestModelForCheckout(t, map[string]string{"owner/repo": repoPath})
	m.ctx.StartTask = func(task context.Task) tea.Cmd {
		capturedTask = task
		return nil
	}
	m.issue = &issuerow.Issue{
		Ctx: m.ctx,
		Data: data.IssueData{
			Number:     13,
			Repository: data.Repository{NameWithOwner: "owner/repo"},
		},
	}

	_, err := m.Checkout()

	require.NoError(t, err)
	require.Equal(t, "issue_checkout_13", capturedTask.Id)
	require.Contains(t, capturedTask.StartText, "#13")
	require.Contains(t, capturedTask.FinishedText, repoPath)
}

func TestIssueBranchName_IsDeterministicFromIssueNumber(t *testing.T) {
	require.Equal(t, "7-issue", issueBranchName(7))
	require.Equal(t, "7-issue", issueBranchName(7))
	require.Equal(t, "99999-issue", issueBranchName(99999))
	require.NotEqual(t, issueBranchName(7), issueBranchName(8))
}

func TestIssueBranchName_StartsWithIssueNumberPrefix(t *testing.T) {
	require.True(t, strings.HasPrefix(issueBranchName(7), "7-"))
	require.True(t, strings.HasPrefix(issueBranchName(99999), "99999-"))
}
