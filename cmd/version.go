package cmd

import (
	"fiber-cli/pkg/fibercli"
	"fmt"
	"github.com/spf13/cobra"
	"os"
)

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Output the fiber version number",
	RunE: func(cmd *cobra.Command, args []string) error {

		latestVersion, err := fibercli.ReleaseVersion()
		if err != nil {
			return err
		}

		wd, err := os.Getwd()
		if err != nil {
			return err
		}

		fmt.Printf("Latest fiber release: %s\n", latestVersion)

		currentVersion, err := fibercli.CurrentVersion(wd)
		if err != nil {
			fmt.Printf("Error in getting current Fiber version: %s", err)
		}

		fmt.Printf("Current fiber release: %s\n", currentVersion)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
