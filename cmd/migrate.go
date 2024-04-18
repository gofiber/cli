package cmd

import (
	"fmt"
	"os"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/muesli/termenv"
	"github.com/spf13/cobra"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate Fiber project version to a newer version",
	RunE:  migrateRunE,
}

var migrated bool

// TODO: fetch current version from go.mod
// TODO: use the --to flag to specify the version to migrate
// TODO: build migration scripts in a separate folder

func migrateRunE(cmd *cobra.Command, _ []string) error {
	cliLatestVersion, err := latestVersion(true)
	if err != nil {
		return err
	}

	if version != cliLatestVersion {
		migrate(cmd, cliLatestVersion)
	} else {
		msg := fmt.Sprintf("Currently Fiber cli is the latest version %s.", cliLatestVersion)
		cmd.Println(termenv.String(msg).
			Foreground(termenv.ANSIBrightBlue))
	}

	return nil
}

func migrate(cmd *cobra.Command, cliLatestVersion string) {
	migrater := execCommand("go", "get", "-u", "-v", "github.com/gofiber/cli/fiber")
	migrater.Env = append(migrater.Env, os.Environ()...)
	migrater.Env = append(migrater.Env, "GO111MODULE=off")

	scmd := internal.NewSpinnerCmd(migrater, "Upgrading")

	if err := scmd.Run(); err != nil && !skipSpinner {
		cmd.Printf("fiber: failed to migrate: %v", err)
		return
	}

	success := fmt.Sprintf("Done! Fiber cli is now at v%s!", cliLatestVersion)
	cmd.Println(termenv.String(success).Foreground(termenv.ANSIBrightGreen))

	migrated = true
}
