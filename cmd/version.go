package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"time"

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
}

var (
	currentVersionRegexp = regexp.MustCompile(`github\.com/gofiber/fiber[^\n]*? (.*)\n`)
	currentVersionFile   = "go.mod"
)

func currentVersion() (string, error) {
	b, err := os.ReadFile(currentVersionFile)
	if err != nil {
		return "", fmt.Errorf("read current version file: %w", err)
	}

	if submatch := currentVersionRegexp.FindSubmatch(b); len(submatch) == 2 {
		return string(submatch[1]), nil
	}

	return "", errors.New("github.com/gofiber/fiber was not found in go.mod")
}

var latestVersionRegexp = regexp.MustCompile(`"name":\s*?"v(.*?)"`)

func LatestFiberVersion() (string, error) {
	return latestVersionByURL("https://api.github.com/repos/gofiber/fiber/releases/latest")
}

func LatestCliVersion() (string, error) {
	return latestVersionByURL("https://api.github.com/repos/gofiber/cli/releases/latest")
}

func latestVersionByURL(url string) (string, error) {
	var (
		res *http.Response
		b   []byte
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create http request: %w", err)
	}

	client := &http.Client{}
	res, err = client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request failed: %w", err)
	}

	defer func() {
		if cerr := res.Body.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	b, err = io.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("read response body: %w", err)
	}

	if submatch := latestVersionRegexp.FindSubmatch(b); len(submatch) == 2 {
		return string(submatch[1]), nil
	}

	return "", errors.New("no version found in github response body")
}
