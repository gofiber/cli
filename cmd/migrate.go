package cmd

import (
	"fmt"
	"github.com/Masterminds/semver/v3"
	"github.com/gofiber/cli/cmd/internal/migrations"
	"github.com/muesli/termenv"
	"github.com/spf13/cobra"
	"os"
	"strings"
)

var (
	targetVersionS     string
	latestFiberVersion string
)

func init() {
	latestFiberVersion, _ := latestVersion(false)

	migrateCmd.Flags().StringVarP(&targetVersionS, "to", "t", "", "Migrate to a specific version e.g: "+latestFiberVersion+" Format: X.Y.Z")
	migrateCmd.MarkFlagRequired("to")
}

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate Fiber project version to a newer version",
	RunE:  migrateRunE,
}

func migrateRunE(cmd *cobra.Command, _ []string) error {
	currentVersionS, err := currentVersion()
	if err != nil {
		return fmt.Errorf("current fiber project version not found: %v", err)
	}
	currentVersionS = strings.TrimPrefix(currentVersionS, "v")
	currentVersion := semver.MustParse(currentVersionS)

	targetVersionS = strings.TrimPrefix(targetVersionS, "v")
	targetVersion, err := semver.NewVersion(targetVersionS)
	if err != nil {
		return fmt.Errorf("invalid version for \"%s\": %v", targetVersionS, err)
	}

	if !targetVersion.GreaterThan(currentVersion) {
		return fmt.Errorf("target version v%s is not greater than current version v%s", targetVersionS, currentVersionS)
	}

	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cannot get current working directory: %v", err)
	}

	err = migrations.DoMigration(cmd, wd, currentVersion, targetVersion)
	if err != nil {
		return fmt.Errorf("migration failed %v", err)
	}

	msg := fmt.Sprintf("Migration from Fiber %s to %s", currentVersionS, targetVersionS)
	cmd.Println(termenv.String(msg).
		Foreground(termenv.ANSIBrightBlue))

	return nil
}
