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

func newMigrateCmd() *cobra.Command {
	var targetVersionS string
	var force bool
	var skipGoMod bool

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
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Force migration even if already on version")
	cmd.Flags().BoolVarP(&skipGoMod, "skip_go_mod", "s", false, "Skip running go mod tidy, download and vendor")

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return migrateRunE(cmd, MigrateOptions{
			CurrentVersionFile: currentVersionFile,
			TargetVersionS:     targetVersionS,
			Force:              force,
			SkipGoMod:          skipGoMod,
		})
	}

	return cmd
}

var migrateCmd = newMigrateCmd()

type MigrateOptions struct {
	CurrentVersionFile string
	TargetVersionS     string
	Force              bool
	SkipGoMod          bool
}

func migrateRunE(cmd *cobra.Command, opts MigrateOptions) error {
	currentVersionS, err := currentVersionFromFile(opts.CurrentVersionFile)
	if err != nil {
		return fmt.Errorf("current fiber project version not found: %w", err)
	}
	currentVersionS = strings.TrimPrefix(currentVersionS, "v")
	currentVersion := semver.MustParse(currentVersionS)

	opts.TargetVersionS = strings.TrimPrefix(opts.TargetVersionS, "v")
	targetVersion, err := semver.NewVersion(opts.TargetVersionS)
	if err != nil {
		return fmt.Errorf("invalid version for \"%s\": %w", opts.TargetVersionS, err)
	}

	if !targetVersion.GreaterThan(currentVersion) && !(opts.Force && targetVersion.Equal(currentVersion)) {
		return fmt.Errorf("target version v%s is not greater than current version v%s", opts.TargetVersionS, currentVersionS)
	}

	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cannot get current working directory: %w", err)
	}

	err = migrations.DoMigration(cmd, wd, currentVersion, targetVersion)
	if err != nil {
		return fmt.Errorf("migration failed %w", err)
	}

	if !opts.SkipGoMod {
		if err := runGoMod(wd); err != nil {
			return fmt.Errorf("go mod: %w", err)
		}
	}

	msg := fmt.Sprintf("Migration from Fiber %s to %s", currentVersionS, opts.TargetVersionS)
	cmd.Println(termenv.String(msg).
		Foreground(termenv.ANSIBrightBlue))

	return nil
}
