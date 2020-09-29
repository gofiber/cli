package fibercli

import (
	"encoding/json"
	"errors"
	"fiber-cli/pkg/file"
	"fmt"
	"io/ioutil"
	"net/http"
	"os/exec"
	"runtime"
)

// Lookup current Fiber version, if available
func CurrentVersion(path string) (string, error) {

	if !file.Exist(fmt.Sprintf("%s/go.mod", path)) {
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
