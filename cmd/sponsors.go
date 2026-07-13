/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"
	"os"

	"charm.land/lipgloss/v2"
	"charm.land/log/v2"
	"github.com/spf13/cobra"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
)

// sponsorsCmd represents the sponsors command
var sponsorsCmd = &cobra.Command{
	Use:   "sponsors",
	Short: "Show the list of current sponsors for gl-dash's upstream project (gh-dash)",
	Long: `gl-dash doesn't have its own sponsorship program yet. This shows the current sponsors
of the upstream project (gh-dash) from GitHub Sponsors under https://github.com/sponsors/dlvhdr.
Requires the GITHUB_TOKEN env var to be set, since this queries the GitHub GraphQL API.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		log.SetLevel(log.ErrorLevel)
		if os.Getenv("GITHUB_TOKEN") == "" {
			return fmt.Errorf(
				"gl-dash sponsors requires the GITHUB_TOKEN env var to be set " +
					"(this command queries the upstream gh-dash project on GitHub, not GitLab)",
			)
		}
		sponsors, err := data.FetchSponsors()
		if err != nil {
			return err
		}

		fmt.Print("\n")
		fmt.Print(
			lipgloss.JoinHorizontal(
				lipgloss.Top,
				lipgloss.NewStyle().
					Foreground(lipgloss.Color("1")).
					Bold(true).
					Render("Thank you ❤️ "),
				lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Render(
					"to all the current (and past!) sponsors - you rock! 🤘🏽"),
			))
		fmt.Print("\n")
		fmt.Print("To help this project with a donation go to https://github.com/sponsors/dlvhdr\n")
		fmt.Print("\n")
		for _, sponsor := range sponsors.User.Sponsors.Nodes {
			if sponsor.Typename == "User" {
				fmt.Printf("  • %s (%s)\n", lipgloss.NewStyle().Bold(true).Render(
					fmt.Sprintf("@%s", sponsor.User.Login)), sponsor.User.Url)
			} else {
				fmt.Printf("  • %s (%s)\n", lipgloss.NewStyle().Bold(true).Render(
					sponsor.Organization.Name), sponsor.Organization.Url)
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(sponsorsCmd)
}
