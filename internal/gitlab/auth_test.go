package gitlab

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	fixtureToken       = "fixture-token-123"
	fixtureAPIProtocol = "https"
	fixtureHost        = "gitlab.com"
)

func writeFixtureConfig(t *testing.T, homeDir string) {
	t.Helper()

	fixture, err := os.ReadFile(filepath.Join("testdata", "glab-config.yml"))
	require.NoError(t, err)

	configDir := filepath.Join(homeDir, ".config", "glab-cli")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yml"), fixture, 0o644))
}

func clearGitLabEnv(t *testing.T) {
	t.Helper()

	t.Setenv("GITLAB_TOKEN", "")
	t.Setenv("GITLAB_HOST", "")
	t.Setenv("CI_JOB_TOKEN", "")
}

func TestLoadAuthConfig(t *testing.T) {
	t.Run(
		"reads token host and api protocol from glab cli config file when env vars are unset",
		func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			clearGitLabEnv(t)
			writeFixtureConfig(t, homeDir)

			cfg, err := LoadAuthConfig()

			require.NoError(t, err)
			require.Equal(t, fixtureToken, cfg.Token)
			require.Equal(t, fixtureHost, cfg.Host)
			require.Equal(t, fixtureAPIProtocol, cfg.APIProtocol)
			require.False(t, cfg.IsJobToken)
		},
	)

	t.Run("falls back to env vars when no glab cli config file exists", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		clearGitLabEnv(t)
		t.Setenv("GITLAB_TOKEN", "envtok")
		t.Setenv("GITLAB_HOST", "gitlab.example.com")

		cfg, err := LoadAuthConfig()

		require.NoError(t, err)
		require.Equal(t, "envtok", cfg.Token)
		require.Equal(t, "gitlab.example.com", cfg.Host)
		require.False(t, cfg.IsJobToken)
	})

	t.Run("falls back to ci job token when gitlab token env var is empty", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		clearGitLabEnv(t)
		t.Setenv("CI_JOB_TOKEN", "ci-job-token-456")

		cfg, err := LoadAuthConfig()

		require.NoError(t, err)
		require.Equal(t, "ci-job-token-456", cfg.Token)
		require.True(t, cfg.IsJobToken)
	})

	t.Run(
		"gitlab token env var takes precedence over ci job token and is not treated as a job token",
		func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			clearGitLabEnv(t)
			t.Setenv("GITLAB_TOKEN", "personal-token-789")
			t.Setenv("CI_JOB_TOKEN", "ci-job-token-789")

			cfg, err := LoadAuthConfig()

			require.NoError(t, err)
			require.Equal(t, "personal-token-789", cfg.Token)
			require.False(t, cfg.IsJobToken)
		},
	)

	t.Run(
		"gitlab token env var takes precedence over file token when both are present",
		func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			clearGitLabEnv(t)
			writeFixtureConfig(t, homeDir)
			t.Setenv("GITLAB_TOKEN", "env-wins-over-file-token")

			cfg, err := LoadAuthConfig()

			require.NoError(t, err)
			require.Equal(t, "env-wins-over-file-token", cfg.Token)
			require.NotEqual(t, fixtureToken, cfg.Token)
			require.False(t, cfg.IsJobToken)
		},
	)

	t.Run(
		"returns empty token and default host without error when no file or env vars exist",
		func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			clearGitLabEnv(t)

			cfg, err := LoadAuthConfig()

			require.NoError(t, err)
			require.Equal(t, "", cfg.Token)
			require.Equal(t, "gitlab.com", cfg.Host)
			require.Equal(t, "https", cfg.APIProtocol)
			require.False(t, cfg.IsJobToken)
		},
	)
}
