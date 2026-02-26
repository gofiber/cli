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
	currentVersionRegexp = regexp.MustCompile(`github\.com/gofiber/fiber[^\n]*?\s+(.*)\n`)
	currentVersionFile   = "go.mod"
)

func currentVersionFromFile(path string) (string, error) {
	b, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("read current version file: %w", err)
	}

	if submatch := currentVersionRegexp.FindSubmatch(b); len(submatch) == 2 {
		return strings.TrimSpace(string(submatch[1])), nil
	}

	return "", errors.New("github.com/gofiber/fiber was not found in go.mod")
}

func currentVersion() (string, error) {
	return currentVersionFromFile(currentVersionFile)
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

	url := "https://api.github.com/repos/gofiber/fiber/releases?per_page=100"
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

	var releases []struct {
		TagName    string `json:"tag_name"`
		Prerelease bool   `json:"prerelease"`
		Draft      bool   `json:"draft"`
	}
	if err := json.Unmarshal(b, &releases); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
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
