package cmd

import (
	"fmt"
	"os"

	"github.com/muesli/termenv"

	"github.com/spf13/cobra"
)

var upgradeCmd = &cobra.Command{
	Use:     "upgrade",
	Aliases: []string{"u"},
	Short:   "Upgrade Fiber CLI if a newer version is available",
	RunE:    upgradeRunE,
}

func upgradeRunE(cmd *cobra.Command, _ []string) error {
	cliLatestVersion, err := latestVersion(true)
	if err != nil {
		return err
	}

	if version != cliLatestVersion {
		upgrader := execCommand("go", "get", "-u", "github.com/gofiber/fiber-cli/fiber")
		upgrader.Env = append(os.Environ(), "GO111MODULE=off")
		if err := runCmd(upgrader); err != nil {
			return fmt.Errorf("fiber: failed to upgrade: %w", err)
		}
		success := fmt.Sprintf("Congratulations! Fiber-cli is now at v%s!", cliLatestVersion)
		cmd.Println(termenv.String(success).Foreground(termenv.ANSIBrightGreen))
	}

	return nil
}
