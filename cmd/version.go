package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"
	"golang.org/x/mod/modfile"

	"github.com/gofiber/cli/cmd/internal/gomod"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the local and released version number of fiber",
	Run:   versionRun,
}

func versionRun(cmd *cobra.Command, _ []string) {
	var (
		cur, latest string
		err         error
		cliLatest   string
		cliErr      error
		w           = cmd.OutOrStdout()
	)

	if cur, err = currentVersion(); err != nil {
		cur = err.Error()
	}

	if latest, err = LatestFiberVersion(); err != nil {
		_, _ = fmt.Fprintf(w, "fiber version: %v\n", err)
		return
	}

	_, _ = fmt.Fprintf(w, "fiber version: %s (latest %s)\n", cur, latest)
	if cliLatest, cliErr = LatestCliVersion(); cliErr != nil {
		_, _ = fmt.Fprintf(w, "fiber cli version: %s (latest check failed: %v)\n", getVersion(), cliErr)
		return
	}

	_, _ = fmt.Fprintf(w, "fiber cli version: %s (latest %s)\n", getVersion(), cliLatest)
}

var (
	// fiberModuleRegexp matches the module path of the core fiber module, at any
	// major version.
	fiberModuleRegexp  = regexp.MustCompile(`^github\.com/gofiber/fiber(/v\d+)?$`)
	currentVersionFile = "go.mod"
)

// errFiberNotRequired says that a go.mod does not require the fiber module.
var errFiberNotRequired = errors.New("github.com/gofiber/fiber was not found in go.mod")

// currentVersionFromFile returns the version of the fiber module that the
// go.mod at path requires, including the leading "v". When go.mod requires
// several majors of fiber, which happens while a project is half migrated, the
// oldest one is returned so that the migration continues from there.
func currentVersionFromFile(path string) (string, error) {
	b, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("read current version file: %w", err)
	}

	mod, err := modfile.ParseLax(path, b, nil)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", path, err)
	}

	oldest := ""
	for _, req := range mod.Require {
		if fiberModuleRegexp.MatchString(req.Mod.Path) {
			oldest = olderFiberVersion(oldest, req.Mod.Version)
		}
	}

	if oldest == "" {
		return "", errFiberNotRequired
	}

	return oldest, nil
}

// olderFiberVersion returns the older of two versions of the fiber module, an
// empty one counting as no version at all. A version that does not parse only
// wins when there is none before it.
func olderFiberVersion(current, candidate string) string {
	if current == "" {
		return candidate
	}
	cur, curErr := semver.NewVersion(current)
	cand, candErr := semver.NewVersion(candidate)
	if candErr == nil && (curErr != nil || cand.LessThan(cur)) {
		return candidate
	}
	return current
}

// currentVersionOf is currentVersionFromFile for the go.mod of the directory the
// command runs in, or, when that directory is no module of a fiber project (the
// root of a repository of several modules, with or without a go.work), the
// oldest version that the modules below it require.
func currentVersionOf(path string) (string, error) {
	version, err := currentVersionFromFile(path)
	if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, errFiberNotRequired) {
		return version, err
	}

	paths, walkErr := gomod.Paths(filepath.Dir(path))
	if walkErr != nil {
		return version, err
	}
	oldest := ""
	for _, p := range paths {
		if v, vErr := currentVersionFromFile(p); vErr == nil {
			oldest = olderFiberVersion(oldest, v)
		}
	}
	if oldest == "" {
		return version, err
	}
	return oldest, nil
}

func currentVersion() (string, error) {
	return currentVersionOf(currentVersionFile)
}

var latestVersionRegexp = regexp.MustCompile(`"name":\s*?"v(.*?)"`)

// LatestFiberVersion retrieves the most recent Fiber release version from GitHub.
func LatestFiberVersion() (string, error) {
	return latestVersionByURL("https://api.github.com/repos/gofiber/fiber/releases/latest")
}

// LatestCliVersion retrieves the latest Fiber CLI release version from GitHub.
func LatestCliVersion() (string, error) {
	return latestVersionByURL("https://api.github.com/repos/gofiber/cli/releases/latest")
}

// LatestFiberVersionForConstraint retrieves the most recent non-prerelease Fiber release matching the given semver constraint.
func LatestFiberVersionForConstraint(constraint *semver.Constraints) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	type release struct {
		TagName    string `json:"tag_name"`
		Prerelease bool   `json:"prerelease"`
		Draft      bool   `json:"draft"`
	}

	for page := 1; page <= 10; page++ {
		url := fmt.Sprintf("https://api.github.com/repos/gofiber/fiber/releases?per_page=100&page=%d", page)
		b, status, err := cachedGET(ctx, url, nil)
		if err != nil {
			return "", fmt.Errorf("http request failed: %w", err)
		}
		if status != http.StatusOK {
			msg := strings.TrimSpace(string(b))
			if msg == "" {
				msg = http.StatusText(status)
			}
			return "", fmt.Errorf("http request failed: %s", msg)
		}

		var releases []release
		if err := json.Unmarshal(b, &releases); err != nil {
			return "", fmt.Errorf("decode response: %w", err)
		}

		// No more releases; all pages exhausted.
		if len(releases) == 0 {
			break
		}

		for _, r := range releases {
			if r.Draft || r.Prerelease {
				continue
			}
			v, parseErr := semver.NewVersion(r.TagName)
			if parseErr != nil {
				continue
			}
			if constraint.Check(v) {
				return strings.TrimPrefix(r.TagName, "v"), nil
			}
		}
	}

	return "", errors.New("no matching release found")
}

func latestVersionByURL(url string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	b, status, err := cachedGET(ctx, url, nil)
	if err != nil {
		return "", fmt.Errorf("http request failed: %w", err)
	}
	if status != http.StatusOK {
		msg := strings.TrimSpace(string(b))
		if msg == "" {
			msg = http.StatusText(status)
		}
		return "", fmt.Errorf("http request failed: %s", msg)
	}

	if submatch := latestVersionRegexp.FindSubmatch(b); len(submatch) == 2 {
		return string(submatch[1]), nil
	}

	return "", errors.New("no version found in github response body")
}
