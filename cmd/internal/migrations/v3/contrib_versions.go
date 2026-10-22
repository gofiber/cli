package v3

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/gofiber/cli/cmd/internal"
)

// contribLookupTimeout bounds the lookup of one module version.
const contribLookupTimeout = time.Minute

var (
	contribV3VersionMu      sync.Mutex
	contribV3VersionCache   = make(map[string]string)
	contribV3VersionFetcher = fetchContribV3Version
	contribV3VersionGroup   singleflight.Group
)

// errContribNoVersion is returned when the module lookup answers without a version.
var errContribNoVersion = errors.New("no version in the answer")

func contribV3Version(module string) (string, error) {
	contribV3VersionMu.Lock()
	if v, ok := contribV3VersionCache[module]; ok {
		contribV3VersionMu.Unlock()
		return v, nil
	}
	fetcher := contribV3VersionFetcher
	contribV3VersionMu.Unlock()

	res, err, _ := contribV3VersionGroup.Do(module, func() (any, error) {
		v, fetchErr := fetcher(module)
		if fetchErr != nil {
			return "", fetchErr
		}

		contribV3VersionMu.Lock()
		contribV3VersionCache[module] = v
		contribV3VersionMu.Unlock()
		return v, nil
	})
	if err != nil {
		return "", fmt.Errorf("fetch contrib version: %w", err)
	}

	v, ok := res.(string)
	if !ok {
		return "", fmt.Errorf("unexpected contrib version type %T", res)
	}

	return v, nil
}

// fetchContribV3Version asks the go command for the latest version of
// github.com/gofiber/contrib/v3/<module>. The go command knows about GOPROXY,
// GOFLAGS, GOPRIVATE, credentials and the module cache, which a request to
// proxy.golang.org does not, so a network that only reaches a private proxy
// works too.
func fetchContribV3Version(module string) (version string, err error) {
	found, err := fetchContribModule(contribV3Prefix+module, "latest")
	return found.Version, err
}

// ContribModule is what the go command says about a version of a module.
type ContribModule struct {
	// Version is the version the query resolved to.
	Version string
	// GoMod is the go.mod file of that version in the module cache.
	GoMod string
}

var contribModuleFetcher = fetchContribModule

// SetContribModuleFetcher overrides the function that asks about a module
// version (the module path and a query such as "latest" or "v1.47.0"), the one
// the migration of the OpenTelemetry modules uses. It returns a restore function.
func SetContribModuleFetcher(fn func(path, query string) (ContribModule, error)) func() {
	contribV3VersionMu.Lock()
	prev := contribModuleFetcher
	contribModuleFetcher = fn
	contribV3VersionMu.Unlock()
	return func() {
		contribV3VersionMu.Lock()
		contribModuleFetcher = prev
		contribV3VersionMu.Unlock()
	}
}

// askContribModule runs the module fetcher.
func askContribModule(path, query string) (ContribModule, error) {
	contribV3VersionMu.Lock()
	fetcher := contribModuleFetcher
	contribV3VersionMu.Unlock()
	return fetcher(path, query)
}

// fetchContribModule asks the go command about path@query.
func fetchContribModule(path, query string) (found ContribModule, err error) {
	// Outside of any module, so that the go.mod of the project (a go directive
	// that asks for another toolchain, a vendor directory) cannot get in the way.
	dir, err := os.MkdirTemp("", "fiber-migrate-contrib-")
	if err != nil {
		return ContribModule{}, fmt.Errorf("create lookup directory: %w", err)
	}
	defer func() {
		if rerr := os.RemoveAll(dir); rerr != nil && err == nil {
			err = fmt.Errorf("remove lookup directory: %w", rerr)
		}
	}()

	// -mod=mod beats a GOFLAGS=-mod=vendor of the environment, which cannot
	// query a module.
	cmd := internal.ExecCommand("go", "list", "-mod=mod", "-m", "-json", path+"@"+query) // #nosec G204 -- fixed command, module name from the CLI's own table and the go.mod files of the project
	cmd.Dir = dir
	cmd.Env = append(slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "GOWORK=") }), "GOWORK=off")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	if err := cmd.Start(); err != nil {
		return ContribModule{}, fmt.Errorf("run go list: %w", err)
	}
	timer := time.AfterFunc(contribLookupTimeout, func() {
		_ = cmd.Process.Kill() //nolint:errcheck // the process may have exited already
	})
	runErr := cmd.Wait()
	timer.Stop()
	if runErr != nil {
		// The go command names the module and says what is wrong.
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return ContribModule{}, fmt.Errorf("%s: %w", msg, runErr)
		}
		return ContribModule{}, fmt.Errorf("go list %s@%s: %w", path, query, runErr)
	}

	var data struct {
		Version string `json:"Version"` //nolint:tagliatelle // field name defined by the go command
		GoMod   string `json:"GoMod"`   //nolint:tagliatelle // field name defined by the go command
	}
	if err := json.Unmarshal(stdout.Bytes(), &data); err != nil {
		return ContribModule{}, fmt.Errorf("parse go list output for %s: %w", path, err)
	}
	if data.Version == "" {
		return ContribModule{}, fmt.Errorf("version of %s@%s: %w", path, query, errContribNoVersion)
	}

	return ContribModule{Version: data.Version, GoMod: data.GoMod}, nil
}

// SetContribV3VersionFetcher overrides the function used to fetch contrib module versions.
// It resets the cached versions and returns a restore function to revert the change.
func SetContribV3VersionFetcher(fn func(string) (string, error)) func() {
	contribV3VersionMu.Lock()
	prev := contribV3VersionFetcher
	contribV3VersionFetcher = fn
	contribV3VersionCache = make(map[string]string)
	contribV3VersionMu.Unlock()
	return func() {
		contribV3VersionMu.Lock()
		contribV3VersionFetcher = prev
		contribV3VersionCache = make(map[string]string)
		contribV3VersionMu.Unlock()
	}
}
