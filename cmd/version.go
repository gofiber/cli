package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"
)

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Output the fiber version number",
	RunE: func(cmd *cobra.Command, args []string) error {

		latestVersion, err := ReleaseVersion()
		if err != nil {
			return err
		}

		wd, err := os.Getwd()
		if err != nil {
			return err
		}

		fmt.Printf("Latest fiber release: %s\n", latestVersion)

		currentVersion, err := CurrentVersion(wd)
		if err != nil {
			fmt.Printf("Error in getting current Fiber version: %s", err)
		}

		fmt.Printf("Current fiber release: %s\n", currentVersion)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

// Lookup current Fiber version, if available
func CurrentVersion(path string) (string, error) {

	if !Exist(fmt.Sprintf("%s/go.mod", path)) {
		return "", errors.New("go mod not found")
	}

	cmd := "go list -u -m all | grep github.com/gofiber/fiber | awk '{print $2}'"

	var out []byte
	var err error
	switch runtime.GOOS {
	case "windows":
		out, err = exec.Command("cmd", "/C", cmd).Output()
		break
	default:
		out, err = exec.Command("bash", "-c", cmd).Output()
		break
	}
	if err != nil {
		return "", err
	}

	return string(out), nil

}

// Lookup Fiber latest release version
func ReleaseVersion() (string, error) {

	res, err := http.Get("https://api.github.com/repos/gofiber/fiber/releases/latest")
	if err != nil {
		return "", err
	}
	release, err := ioutil.ReadAll(res.Body)
	if res.Body.Close() != nil {
		return "", err
	}

	if err != nil {
		return "", err

	}

	jsonRes := make(map[string]interface{})

	parseErr := json.Unmarshal(release, &jsonRes)
	if parseErr != nil {
		return "", err
	}

	return fmt.Sprintf("%s", jsonRes["name"]), nil
}
