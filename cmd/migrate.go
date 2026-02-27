package cmd

import (
	"context"
	"encoding/json"
	"errors"
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

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations"
)

func newMigrateCmd() *cobra.Command {
	var targetVersionS string
	var targetHash string
	var force bool
	var skipGoMod bool
	var verbose bool
	var thirdParty []string
	var includeFiles []string
	var excludeFiles []string

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Migrate Fiber project version to a newer version",
	}

	cmd.Flags().StringVarP(&targetVersionS, "to", "t", "", "Migrate to a specific version. Default: latest")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Force migration even if already on version")
	cmd.Flags().BoolVarP(&skipGoMod, "skip_go_mod", "s", false, "Skip running go mod tidy, download and vendor")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")
	cmd.Flags().StringVar(&targetHash, "hash", "", "Commit hash for Fiber version")
	cmd.Flags().StringSliceVar(&thirdParty, "third-party", nil, "Refresh third-party modules (contrib,storage,template). Use a comma-separated list like --third-party=contrib,storage and append @<commit> to pin a commit")
	cmd.Flags().StringSliceVar(&includeFiles, "include", nil, "Comma-separated list of files to include. Supports glob and regex patterns")
	cmd.Flags().StringSliceVar(&excludeFiles, "exclude", nil, "Comma-separated list of files to exclude. Supports glob and regex patterns")

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		tps := make([]ThirdPartyParam, 0, len(thirdParty))
		for _, tp := range thirdParty {
			parts := strings.SplitN(tp, "@", 2)
			p := ThirdPartyParam{Name: parts[0]}
			if len(parts) == 2 {
				p.Hash = parts[1]
			}
			if p.Name != "" {
				tps = append(tps, p)
			}
		}

		return migrateRunE(cmd, MigrateOptions{
			CurrentVersionFile: currentVersionFile,
			TargetVersionS:     targetVersionS,
			TargetHash:         targetHash,
			Force:              force,
			SkipGoMod:          skipGoMod,
			Verbose:            verbose,
			ThirdParty:         tps,
			IncludeFiles:       includeFiles,
			ExcludeFiles:       excludeFiles,
		})
	}

	return cmd
}

var migrateCmd = newMigrateCmd()

type MigrateOptions struct {
	CurrentVersionFile string
	TargetVersionS     string
	TargetHash         string
	ThirdParty         []ThirdPartyParam
	IncludeFiles       []string
	ExcludeFiles       []string
	Force              bool
	SkipGoMod          bool
	Verbose            bool
}

type ThirdPartyParam struct {
	Name string
	Hash string
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
	// Resolve partial/wildcard versions (e.g. "3", "3.*", "3.1", "3.1.x") to the
	// latest matching release from GitHub. Skip when a target hash is provided so
	// the pseudo-version base is derived directly from the user-specified version.
	if opts.TargetHash == "" {
		if constraint, parseErr := partialVersionConstraint(opts.TargetVersionS); parseErr == nil {
			resolved, resolveErr := LatestFiberVersionForConstraint(constraint)
			if resolveErr != nil {
				return fmt.Errorf("failed to resolve version %q: %w", opts.TargetVersionS, resolveErr)
			}
			opts.TargetVersionS = resolved
		}
	}
	baseVersion, err := semver.NewVersion(opts.TargetVersionS)
	if err != nil {
		return fmt.Errorf("invalid version for \"%s\": %w", opts.TargetVersionS, err)
	}
	targetVersion := baseVersion
	if opts.TargetHash != "" {
		pv, err := pseudoVersionFromHash("gofiber/fiber", baseVersion, opts.TargetHash)
		if err != nil {
			return fmt.Errorf("pseudo version: %w", err)
		}
		opts.TargetVersionS = pv
		targetVersion, err = semver.NewVersion(pv)
		if err != nil {
			return fmt.Errorf("invalid pseudo version for \"%s\": %w", pv, err)
		}
	}

	if !opts.Force && !targetVersion.GreaterThan(currentVersion) {
		cmd.Printf("Fiber already at %s\n", currentVersionS)
		return nil
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

	err = migrations.DoMigration(cmd, wd, migrateFrom, targetVersion, opts.SkipGoMod, opts.Verbose, opts.IncludeFiles, opts.ExcludeFiles)
	if err != nil {
		return fmt.Errorf("migration failed %w", err)
	}

	tpChanged := false
	for _, tp := range opts.ThirdParty {
		var (
			changed bool
			err     error
		)
		switch tp.Name {
		case "contrib":
			changed, err = refreshContrib(cmd, wd, tp.Hash)
		case "storage":
			changed, err = refreshStorage(cmd, wd, tp.Hash)
		case "template", "templates":
			changed, err = refreshTemplates(cmd, wd, tp.Hash)
		default:
			continue
		}
		if err != nil {
			return fmt.Errorf("refresh %s packages: %w", tp.Name, err)
		}
		if changed {
			tpChanged = true
		}
	}
	if tpChanged && !opts.SkipGoMod {
		if err := internal.RunGoMod(wd); err != nil {
			return fmt.Errorf("go mod: %w", err)
		}
	}

	msg := fmt.Sprintf("Migration from Fiber %s to %s", migrateFromS, targetVersion.String())
	cmd.Println(termenv.String(msg).
		Foreground(termenv.ANSIBrightBlue))

	return nil
}

func pseudoVersionFromHash(repo string, base *semver.Version, hash string) (string, error) {
	url := "https://api.github.com/repos/" + repo + "/commits/" + hash
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	headers := map[string]string{"User-Agent": "fiber-cli"}
	b, status, err := cachedGET(ctx, url, headers)
	if err != nil {
		return "", fmt.Errorf("http request failed: %w", err)
	}
	if status != http.StatusOK {
		msg := b
		if len(msg) == 0 {
			msg = []byte(http.StatusText(status))
		}
		return "", fmt.Errorf("http request failed: %s", strings.TrimSpace(string(msg)))
	}

	var data struct {
		Commit struct {
			Committer struct {
				Date *time.Time `json:"date"`
			} `json:"committer"`
			Author struct {
				Date *time.Time `json:"date"`
			} `json:"author"`
		} `json:"commit"`
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal(b, &data); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	var commitTime time.Time
	switch {
	case data.Commit.Committer.Date != nil && !data.Commit.Committer.Date.IsZero():
		commitTime = *data.Commit.Committer.Date
	case data.Commit.Author.Date != nil && !data.Commit.Author.Date.IsZero():
		commitTime = *data.Commit.Author.Date
	default:
		return "", errors.New("commit date not found")
	}

	commitTime = commitTime.UTC().Truncate(time.Second)

	short := data.SHA
	if short == "" {
		short = hash
	}
	if len(short) > 12 {
		short = short[:12]
	}
	pv := module.PseudoVersion("v"+strconv.FormatUint(base.Major(), 10), "v"+base.String(), commitTime, short)
	return strings.TrimPrefix(pv, "v"), nil
}

// partialVersionConstraint builds a semver constraint for partial or wildcard version strings such as
// "3", "3.*", "3.x", "3.1", "3.1.*", "3.1.x". It returns an error for full semver strings (x.y.z)
// or strings with pre-release/build metadata, which should be used as-is.
func partialVersionConstraint(v string) (*semver.Constraints, error) {
	// Normalize trailing wildcard segments in order (longest first).
	v = strings.TrimSuffix(v, ".*.*")
	v = strings.TrimSuffix(v, ".x.x")
	v = strings.TrimSuffix(v, ".*")
	v = strings.TrimSuffix(v, ".x")

	parts := strings.Split(v, ".")
	if len(parts) >= 3 {
		return nil, fmt.Errorf("not a partial version: %s", v)
	}

	// All parts must be plain non-negative integers (no pre-release or build metadata).
	nums := make([]uint64, len(parts))
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("not a partial version: %s", v)
		}
		nums[i] = n
	}

	var constraintStr string
	switch len(nums) {
	case 1: // e.g. "3" → >= 3.0.0, < 4.0.0
		constraintStr = fmt.Sprintf(">= %d.0.0, < %d.0.0", nums[0], nums[0]+1)
	case 2: // e.g. "3.1" → >= 3.1.0, < 3.2.0
		constraintStr = fmt.Sprintf(">= %d.%d.0, < %d.%d.0", nums[0], nums[1], nums[0], nums[1]+1)
	default:
		return nil, fmt.Errorf("not a partial version: %s", v)
	}

	c, err := semver.NewConstraint(constraintStr)
	if err != nil {
		return nil, fmt.Errorf("build constraint: %w", err)
	}
	return c, nil
}
