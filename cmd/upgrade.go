package cmd

import (
	"fmt"

	"github.com/muesli/termenv"

	"github.com/spf13/cobra"
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade Fiber cli if a newer version is available",
	RunE:  upgradeRunE,
}

func upgradeRunE(cmd *cobra.Command, _ []string) error {
	cliLatestVersion, err := latestVersion(true)
	if err != nil {
		return err
	}

	if version != cliLatestVersion {
		upgrade(cmd, cliLatestVersion)
	} else {
		msg := fmt.Sprintf("Currently Fiber cli is the latest version %s.", cliLatestVersion)
		cmd.Println(termenv.String(msg).
			Foreground(termenv.ANSIBrightBlue))
	}

	return nil
}

func upgrade(cmd *cobra.Command, cliLatestVersion string) {
	if err := runCmd(execCommand("go", "get", "-u", "github.com/gofiber/fiber-cli/fiber")); err != nil {
		cmd.Printf("fiber: failed to upgrade: %v", err)
		return
	}

	success := fmt.Sprintf("Done! Fiber-cli is now at v%s!", cliLatestVersion)
	cmd.Println(termenv.String(success).Foreground(termenv.ANSIBrightGreen))
}
