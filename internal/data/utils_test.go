package data

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

func TestGetAuthorRoleIcon_MapsGitLabAccessLevels(t *testing.T) {
	th := *theme.DefaultTheme

	tests := []struct {
		role     string
		wantIcon string
	}{
		// GitLab project access levels (AccessLevelEnum.stringValue).
		{"OWNER", th.OwnerIcon},
		{"MAINTAINER", th.MemberIcon},
		{"DEVELOPER", th.CollaboratorIcon},
		{"REPORTER", th.ContributorIcon},
		{"GUEST", th.NewContributorIcon},
		// Unknown / no membership falls back to the neutral icon.
		{"", th.UnknownRoleIcon},
		{"MINIMAL_ACCESS", th.UnknownRoleIcon},
	}

	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			got := GetAuthorRoleIcon(tt.role, th)
			assert.Truef(t, strings.Contains(got, tt.wantIcon),
				"role %q: expected rendered icon to contain %q, got %q", tt.role, tt.wantIcon, got)
		})
	}
}
