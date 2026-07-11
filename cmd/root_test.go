package cmd

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

func initTestGitRepo(t *testing.T, originURL string) string {
	t.Helper()
	dir := t.TempDir()

	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	require.NoError(t, cmd.Run())

	if originURL != "" {
		cmd = exec.Command("git", "remote", "add", "origin", originURL)
		cmd.Dir = dir
		require.NoError(t, cmd.Run())
	}

	return dir
}

func TestGetCurrentGitAndGitHubRepos(t *testing.T) {
	t.Run("git repo with gitlab origin remote", func(t *testing.T) {
		t.Setenv("GH_REPO", "")
		dir := initTestGitRepo(t, "https://gitlab.com/ns/proj.git")
		t.Chdir(dir)

		gitRepo, ghRepo, err := getCurrentGitAndGitHubRepos()

		require.NoError(t, err)
		require.NotNil(t, gitRepo)
		require.Equal(t, "ns", ghRepo.Owner)
		require.Equal(t, "proj", ghRepo.Name)
	})

	t.Run("directory without git repo", func(t *testing.T) {
		t.Setenv("GH_REPO", "")
		dir := t.TempDir()
		t.Chdir(dir)

		require.NotPanics(t, func() {
			_, _, err := getCurrentGitAndGitHubRepos()
			require.Error(t, err)
		})
	})

	t.Run("git repo without origin remote", func(t *testing.T) {
		t.Setenv("GH_REPO", "")
		dir := initTestGitRepo(t, "")
		t.Chdir(dir)

		require.NotPanics(t, func() {
			_, _, err := getCurrentGitAndGitHubRepos()
			require.Error(t, err)
		})
	})
}
