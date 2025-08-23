package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/muesli/termenv"
	"github.com/spf13/cobra"
	"golang.org/x/mod/module"

	"github.com/gofiber/cli/cmd/internal/migrations"
)

func newMigrateCmd() *cobra.Command {
	var targetVersionS string
	var targetHash string
	var force bool
	var skipGoMod bool
	var verbose bool

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Migrate Fiber project version to a newer version",
	}

	cmd.Flags().StringVarP(&targetVersionS, "to", "t", "", "Migrate to a specific version. Default: latest")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Force migration even if already on version")
	cmd.Flags().BoolVarP(&skipGoMod, "skip_go_mod", "s", false, "Skip running go mod tidy, download and vendor")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")
	cmd.Flags().StringVar(&targetHash, "hash", "", "Commit hash for Fiber version")

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return migrateRunE(cmd, MigrateOptions{
			CurrentVersionFile: currentVersionFile,
			TargetVersionS:     targetVersionS,
			TargetHash:         targetHash,
			Force:              force,
			SkipGoMod:          skipGoMod,
			Verbose:            verbose,
		})
	}

	return cmd
}

var migrateCmd = newMigrateCmd()

type MigrateOptions struct {
	CurrentVersionFile string
	TargetVersionS     string
	TargetHash         string
	Force              bool
	SkipGoMod          bool
	Verbose            bool
}

func migrateRunE(cmd *cobra.Command, opts MigrateOptions) error {
	currentVersionS, err := currentVersionFromFile(opts.CurrentVersionFile)
	if err != nil {
		return fmt.Errorf("current fiber project version not found: %w", err)
	}
	currentVersionS = strings.TrimPrefix(currentVersionS, "v")
	currentVersion := semver.MustParse(currentVersionS)

	if opts.TargetVersionS == "" {
		opts.TargetVersionS, err = LatestFiberVersion()
		if err != nil {
			return fmt.Errorf("failed to determine latest fiber version: %w", err)
		}
	}
	opts.TargetVersionS = strings.TrimPrefix(opts.TargetVersionS, "v")
	baseVersion, err := semver.NewVersion(opts.TargetVersionS)
	if err != nil {
		return fmt.Errorf("invalid version for \"%s\": %w", opts.TargetVersionS, err)
	}

	targetVersion := baseVersion
	if opts.TargetHash != "" {
		pv, err := pseudoVersionFromHash(baseVersion, opts.TargetHash)
		if err != nil {
			return fmt.Errorf("pseudo version: %w", err)
		}
		opts.TargetVersionS = pv
		targetVersion, err = semver.NewVersion(pv)
		if err != nil {
			return fmt.Errorf("invalid pseudo version for \"%s\": %w", pv, err)
		}
	}

	if !targetVersion.GreaterThan(currentVersion) && !(opts.Force && targetVersion.Equal(currentVersion)) {
		return fmt.Errorf("target version v%s is not greater than current version v%s", opts.TargetVersionS, currentVersionS)
	}

	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cannot get current working directory: %w", err)
	}

	migrateFrom := currentVersion
	migrateFromS := currentVersionS
	if opts.Force && !targetVersion.GreaterThan(currentVersion) {
		prevMajor := targetVersion.Major() - 1
		migrateFrom, err = semver.NewVersion(fmt.Sprintf("%d.0.0", prevMajor))
		if err != nil {
			return fmt.Errorf("invalid previous major version %d: %w", prevMajor, err)
		}
		migrateFromS = migrateFrom.String()
	}

	err = migrations.DoMigration(cmd, wd, migrateFrom, targetVersion, opts.SkipGoMod, opts.Verbose)
	if err != nil {
		return fmt.Errorf("migration failed %w", err)
	}

	msg := fmt.Sprintf("Migration from Fiber %s to %s", migrateFromS, opts.TargetVersionS)
	cmd.Println(termenv.String(msg).
		Foreground(termenv.ANSIBrightBlue))

	return nil
}

func pseudoVersionFromHash(base *semver.Version, hash string) (string, error) {
	url := "https://api.github.com/repos/gofiber/fiber/commits/" + hash
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create http request: %w", err)
	}
	client := http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request failed: %w", err)
	}
	defer func() {
		if err := res.Body.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to close response body: %v\n", err)
		}
	}()

	var data struct {
		SHA    string `json:"sha"`
		Commit struct {
			Committer struct {
				Date time.Time `json:"date"`
			} `json:"committer"`
		} `json:"commit"`
	}
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	short := data.SHA
	if short == "" {
		short = hash
	}
	if len(short) > 12 {
		short = short[:12]
	}
	pv := module.PseudoVersion("v"+strconv.FormatUint(base.Major(), 10), "v"+base.String(), data.Commit.Committer.Date, short)
	return strings.TrimPrefix(pv, "v"), nil
}
