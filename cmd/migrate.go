package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/muesli/termenv"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal/migrations"
)

func newMigrateCmd(currentVersionFile string) *cobra.Command {
	var targetVersionS string

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Migrate Fiber project version to a newer version",
	}

	latestFiberVersion, err := LatestFiberVersion()
	if err != nil {
		latestFiberVersion = ""
	}

	cmd.Flags().StringVarP(&targetVersionS, "to", "t", "", "Migrate to a specific version e.g:"+latestFiberVersion+" Format: X.Y.Z")
	if err := cmd.MarkFlagRequired("to"); err != nil {
		panic(err)
	}

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return migrateRunE(cmd, currentVersionFile, targetVersionS)
	}

	return cmd
}

var migrateCmd = newMigrateCmd("go.mod")

func migrateRunE(cmd *cobra.Command, currentVersionFile, targetVersionS string) error {
	currentVersionS, err := currentVersionFromFile(currentVersionFile)
	if err != nil {
		return fmt.Errorf("current fiber project version not found: %w", err)
	}
	currentVersionS = strings.TrimPrefix(currentVersionS, "v")
	currentVersion := semver.MustParse(currentVersionS)

	targetVersionS = strings.TrimPrefix(targetVersionS, "v")
	targetVersion, err := semver.NewVersion(targetVersionS)
	if err != nil {
		return fmt.Errorf("invalid version for \"%s\": %w", targetVersionS, err)
	}

	if !targetVersion.GreaterThan(currentVersion) {
		return fmt.Errorf("target version v%s is not greater than current version v%s", targetVersionS, currentVersionS)
	}

	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cannot get current working directory: %w", err)
	}

	err = migrations.DoMigration(cmd, wd, currentVersion, targetVersion)
	if err != nil {
		return fmt.Errorf("migration failed %w", err)
	}

	msg := fmt.Sprintf("Migration from Fiber %s to %s", currentVersionS, targetVersionS)
	cmd.Println(termenv.String(msg).
		Foreground(termenv.ANSIBrightBlue))

	return nil
}
