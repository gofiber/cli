package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade Fiber CLI if a newer version is available",
	RunE:  upgradeRun,
}

func upgradeRun(cmd *cobra.Command, _ []string) error {

	cliLatestVersion, err := latestVersion(true)
	if err != nil {
		return err
	}

	update := execCommand("go", "get", "-u", "-v", "github.com/gofiber/fiber-cli")
	if out, err := update.CombinedOutput(); err != nil {
		fmt.Printf("fiber upgrade: failed to update: %s\nCheck the logs for more info.\n", out)
		return err
	}

	fmt.Printf("fiber upgrade: updated %s -> %s successfully!\n", version, cliLatestVersion)

	return nil
}
