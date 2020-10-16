package cmd

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"regexp"

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

	if latest, err = latestVersion(false); err != nil {
		_, _ = fmt.Fprintf(w, "fiber version: %v\n", err)
		return
	}

	_, _ = fmt.Fprintf(w, "fiber version: %s (latest %s)\n", cur, latest)
}

var currentVersionRegexp = regexp.MustCompile(`github\.com/gofiber/fiber[^\n]*? (.*)\n`)
var currentVersionFile = "go.mod"

func currentVersion() (string, error) {
	b, err := ioutil.ReadFile(currentVersionFile)
	if err != nil {
		return "", err
	}

	if submatch := currentVersionRegexp.FindSubmatch(b); len(submatch) == 2 {
		return string(submatch[1]), nil
	}

	return "", errors.New("github.com/gofiber/fiber was not found in go.mod")
}

var latestVersionRegexp = regexp.MustCompile(`"name":\s*?"v(.*?)"`)

func latestVersion(getCliVersion bool) (v string, err error) {
	var (
		res *http.Response
		b   []byte
	)

	if getCliVersion {
		res, err = http.Get("https://api.github.com/repos/gofiber/fiber-cli/releases/latest")
	} else {
		res, err = http.Get("https://api.github.com/repos/gofiber/fiber/releases/latest")
	}

	if err != nil {
		return
	}

	defer func() {
		_ = res.Body.Close()
	}()

	if b, err = ioutil.ReadAll(res.Body); err != nil {
		return
	}

	if submatch := latestVersionRegexp.FindSubmatch(b); len(submatch) == 2 {
		return string(submatch[1]), nil
	}

	return "", errors.New("no version found in github response body")
}
