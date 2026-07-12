package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func runGitCommand(t *testing.T, dir string, args ...string) string {
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
	return string(out)
}

func currentBranch(t *testing.T, dir string) string {
	t.Helper()
	out := runGitCommand(t, dir, "rev-parse", "--abbrev-ref", "HEAD")
	return strings.TrimSpace(out)
}

func initRemoteRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGitCommand(t, dir, "init", "-q", "-b", "main")
	runGitCommand(t, dir, "config", "commit.gpgsign", "false")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello"), 0o644))
	runGitCommand(t, dir, "add", "README.md")
	runGitCommand(t, dir, "commit", "-q", "-m", "init")
	return dir
}

func cloneRepo(t *testing.T, remote string) string {
	t.Helper()
	local := filepath.Join(t.TempDir(), "local")
	runGitCommand(t, remote, "clone", "-q", remote, local)
	runGitCommand(t, local, "config", "commit.gpgsign", "false")
	return local
}

func createMergeRequestRef(t *testing.T, remoteDir string, iid int, fileContent string) {
	t.Helper()
	tmpBranch := "tmp-mr-branch"
	runGitCommand(t, remoteDir, "checkout", "-q", "-b", tmpBranch)
	require.NoError(
		t,
		os.WriteFile(filepath.Join(remoteDir, "mr.txt"), []byte(fileContent), 0o644),
	)
	runGitCommand(t, remoteDir, "add", "mr.txt")
	runGitCommand(t, remoteDir, "commit", "-q", "-m", "mr change")
	runGitCommand(
		t,
		remoteDir,
		"update-ref",
		fmt.Sprintf("refs/merge-requests/%d/head", iid),
		tmpBranch,
	)
	runGitCommand(t, remoteDir, "checkout", "-q", "main")
	runGitCommand(t, remoteDir, "branch", "-q", "-D", tmpBranch)
}

func createRemoteBranch(t *testing.T, remoteDir, branchName, fileName, fileContent string) {
	t.Helper()
	runGitCommand(t, remoteDir, "checkout", "-q", "-b", branchName)
	require.NoError(
		t,
		os.WriteFile(filepath.Join(remoteDir, fileName), []byte(fileContent), 0o644),
	)
	runGitCommand(t, remoteDir, "add", fileName)
	runGitCommand(t, remoteDir, "commit", "-q", "-m", "branch change")
	runGitCommand(t, remoteDir, "checkout", "-q", "main")
}

func forceRemoteBranchToDivergedCommit(
	t *testing.T,
	remoteDir, branchName, fileName, fileContent string,
) {
	t.Helper()
	tmpBranch := "tmp-diverge-branch"
	runGitCommand(t, remoteDir, "checkout", "-q", "-b", tmpBranch, "main")
	require.NoError(
		t,
		os.WriteFile(filepath.Join(remoteDir, fileName), []byte(fileContent), 0o644),
	)
	runGitCommand(t, remoteDir, "add", fileName)
	runGitCommand(t, remoteDir, "commit", "-q", "-m", "diverged branch change")
	runGitCommand(t, remoteDir, "branch", "-q", "-f", branchName, tmpBranch)
	runGitCommand(t, remoteDir, "checkout", "-q", "main")
	runGitCommand(t, remoteDir, "branch", "-q", "-D", tmpBranch)
}

func TestCheckoutMergeRequest_ChecksOutExistingRef(t *testing.T) {
	remote := initRemoteRepo(t)
	createMergeRequestRef(t, remote, 5, "mr content")
	local := cloneRepo(t, remote)

	err := CheckoutMergeRequest(local, 5)

	require.NoError(t, err)
	require.Equal(t, "mr-5", currentBranch(t, local))
	require.FileExists(t, filepath.Join(local, "mr.txt"))
}

func TestCheckoutMergeRequest_RefNotFound_ReturnsError(t *testing.T) {
	remote := initRemoteRepo(t)
	local := cloneRepo(t, remote)

	err := CheckoutMergeRequest(local, 999)

	require.Error(t, err)
}

func TestCheckoutMergeRequest_InvalidRepoPath_ReturnsError(t *testing.T) {
	err := CheckoutMergeRequest(t.TempDir(), 1)

	require.Error(t, err)
}

func TestCheckoutMergeRequest_RerunAfterAlreadyCheckedOut_DoesNotFail(t *testing.T) {
	remote := initRemoteRepo(t)
	createMergeRequestRef(t, remote, 5, "mr content")
	local := cloneRepo(t, remote)

	require.NoError(t, CheckoutMergeRequest(local, 5))

	err := CheckoutMergeRequest(local, 5)

	require.NoError(
		t,
		err,
		"re-checking out the same MR while already on its branch must not fail",
	)
	require.Equal(t, "mr-5", currentBranch(t, local))
}

func TestCheckoutMergeRequest_DivergedRemoteRef_ReturnsErrorAndPreservesLocalCommit(
	t *testing.T,
) {
	remote := initRemoteRepo(t)
	createMergeRequestRef(t, remote, 5, "mr content v1")
	local := cloneRepo(t, remote)
	require.NoError(t, CheckoutMergeRequest(local, 5))

	localOnlyFile := "local-work.txt"
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(local, localOnlyFile),
			[]byte("uncommitted local work"),
			0o644,
		),
	)
	runGitCommand(t, local, "add", localOnlyFile)
	runGitCommand(t, local, "commit", "-q", "-m", "local work not pushed to remote")
	localHeadBeforeRetry := strings.TrimSpace(runGitCommand(t, local, "rev-parse", "HEAD"))

	createMergeRequestRef(t, remote, 5, "mr content v2 diverged")

	err := CheckoutMergeRequest(local, 5)

	require.Error(t, err)
	require.Equal(t, "mr-5", currentBranch(t, local))
	require.Equal(
		t,
		localHeadBeforeRetry,
		strings.TrimSpace(runGitCommand(t, local, "rev-parse", "HEAD")),
		"HEAD must still point at the local commit after a failed fast-forward",
	)
	require.FileExists(t, filepath.Join(local, localOnlyFile))
	require.Contains(
		t,
		runGitCommand(t, local, "log", "--oneline", "-n", "5"),
		"local work not pushed to remote",
	)
}

func TestCheckoutBranch_RemoteBranchExists_FetchesAndChecksOut(t *testing.T) {
	remote := initRemoteRepo(t)
	createRemoteBranch(t, remote, "feature-x", "feature.txt", "feature data")
	local := cloneRepo(t, remote)

	err := CheckoutBranch(local, "feature-x")

	require.NoError(t, err)
	require.Equal(t, "feature-x", currentBranch(t, local))
	require.FileExists(t, filepath.Join(local, "feature.txt"))
}

func TestCheckoutBranch_RemoteBranchMissing_FallsBackToLocalCreationFromHead(t *testing.T) {
	remote := initRemoteRepo(t)
	local := cloneRepo(t, remote)

	err := CheckoutBranch(local, "issue-42")

	require.NoError(t, err)
	require.Equal(t, "issue-42", currentBranch(t, local))
	require.FileExists(t, filepath.Join(local, "README.md"))
}

func TestCheckoutBranch_RerunAfterFallbackCreation_DoesNotFail(t *testing.T) {
	remote := initRemoteRepo(t)
	local := cloneRepo(t, remote)

	require.NoError(t, CheckoutBranch(local, "issue-42"))

	err := CheckoutBranch(local, "issue-42")

	require.NoError(
		t,
		err,
		"re-checking out the same local-only branch must not fail",
	)
	require.Equal(t, "issue-42", currentBranch(t, local))
}

func TestCheckoutBranch_DivergedRemoteBranch_ReturnsErrorAndPreservesLocalCommit(
	t *testing.T,
) {
	remote := initRemoteRepo(t)
	createRemoteBranch(t, remote, "feature-y", "feature.txt", "feature v1")
	local := cloneRepo(t, remote)
	require.NoError(t, CheckoutBranch(local, "feature-y"))

	localOnlyFile := "local-work.txt"
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(local, localOnlyFile),
			[]byte("uncommitted local work"),
			0o644,
		),
	)
	runGitCommand(t, local, "add", localOnlyFile)
	runGitCommand(t, local, "commit", "-q", "-m", "local work not pushed to remote")
	localHeadBeforeRetry := strings.TrimSpace(runGitCommand(t, local, "rev-parse", "HEAD"))

	forceRemoteBranchToDivergedCommit(t, remote, "feature-y", "feature.txt", "feature v2 diverged")

	err := CheckoutBranch(local, "feature-y")

	require.Error(t, err)
	require.Equal(t, "feature-y", currentBranch(t, local))
	require.Equal(
		t,
		localHeadBeforeRetry,
		strings.TrimSpace(runGitCommand(t, local, "rev-parse", "HEAD")),
		"HEAD must still point at the local commit after a failed fast-forward",
	)
	require.FileExists(t, filepath.Join(local, localOnlyFile))
	require.Contains(
		t,
		runGitCommand(t, local, "log", "--oneline", "-n", "5"),
		"local work not pushed to remote",
	)
}

func TestCheckoutBranch_InvalidRepoPath_ReturnsError(t *testing.T) {
	err := CheckoutBranch(t.TempDir(), "any-branch")

	require.Error(t, err)
}

func TestParseRemoteURL(t *testing.T) {
	tests := []struct {
		name   string
		url    string
		want   RemoteRepo
		wantOk bool
	}{
		{
			name:   "https gitlab.com",
			url:    "https://gitlab.com/ns/proj.git",
			want:   RemoteRepo{Host: "gitlab.com", Owner: "ns", Name: "proj"},
			wantOk: true,
		},
		{
			name:   "https self-managed gitlab",
			url:    "https://gitlab.empresa.com/ns/proj.git",
			want:   RemoteRepo{Host: "gitlab.empresa.com", Owner: "ns", Name: "proj"},
			wantOk: true,
		},
		{
			name:   "https with custom port",
			url:    "https://gitlab.empresa.com:8443/ns/proj.git",
			want:   RemoteRepo{Host: "gitlab.empresa.com", Owner: "ns", Name: "proj"},
			wantOk: true,
		},
		{
			name:   "ssh gitlab.com",
			url:    "git@gitlab.com:ns/proj.git",
			want:   RemoteRepo{Host: "gitlab.com", Owner: "ns", Name: "proj"},
			wantOk: true,
		},
		{
			name:   "ssh self-managed nested subgroup",
			url:    "git@gitlab.empresa.com:group/subgroup/proj.git",
			want:   RemoteRepo{Host: "gitlab.empresa.com", Owner: "group/subgroup", Name: "proj"},
			wantOk: true,
		},
		{
			name:   "ssh explicit scheme without port",
			url:    "ssh://git@gitlab.com/group/proj.git",
			want:   RemoteRepo{Host: "gitlab.com", Owner: "group", Name: "proj"},
			wantOk: true,
		},
		{
			name:   "ssh with custom port",
			url:    "ssh://git@host:2222/group/proj.git",
			want:   RemoteRepo{Host: "host", Owner: "group", Name: "proj"},
			wantOk: true,
		},
		{
			name:   "invalid url",
			url:    "not a url",
			wantOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseRemoteURL(tt.url)

			require.Equal(t, tt.wantOk, ok)
			if tt.wantOk {
				require.Equal(t, tt.want, got)
			}
		})
	}
}

func TestGetRepoShortName(t *testing.T) {
	require.Equal(t, "ns/proj", GetRepoShortName("https://gitlab.com/ns/proj.git"))
}
