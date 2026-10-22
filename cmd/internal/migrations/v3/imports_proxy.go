package v3

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/mod/module"
)

// importsModuleInfo is what the Go module proxy says about a module.
type importsModuleInfo struct {
	// published is when the version that was asked about was released.
	published time.Time
	// latest is the newest release of the module line.
	latest string
}

// importsLookupModule asks the module proxy about version vers of the module.
// It is a variable so that tests do not need the network.
var importsLookupModule = importsLookupOnProxy

var errImportsNoProxy = errors.New("no module proxy is configured (GOPROXY is off)")

// importsProxyTimeout bounds a request to the module proxy.
const importsProxyTimeout = 5 * time.Second

var importsHTTPClient = &http.Client{Timeout: importsProxyTimeout}

// importsProxyBase returns the module proxy to ask: the first http(s) entry of
// GOPROXY, or proxy.golang.org when it is not set. ok is false when GOPROXY
// allows no proxy at all.
func importsProxyBase() (base string, ok bool) {
	goproxy, set := os.LookupEnv("GOPROXY")
	if !set || strings.TrimSpace(goproxy) == "" {
		return "https://proxy.golang.org", true
	}

	for _, entry := range strings.FieldsFunc(goproxy, func(r rune) bool { return r == ',' || r == '|' }) {
		if strings.HasPrefix(entry, "https://") || strings.HasPrefix(entry, "http://") {
			return strings.TrimRight(entry, "/"), true
		}
	}
	return "", false
}

func importsLookupOnProxy(path, vers string) (importsModuleInfo, error) {
	base, ok := importsProxyBase()
	if !ok {
		return importsModuleInfo{}, errImportsNoProxy
	}
	escaped, err := module.EscapePath(path)
	if err != nil {
		return importsModuleInfo{}, fmt.Errorf("escape %s: %w", path, err)
	}
	escapedVers, err := module.EscapeVersion(vers)
	if err != nil {
		return importsModuleInfo{}, fmt.Errorf("escape %s: %w", vers, err)
	}

	current, err := importsFetchProxyInfo(base + "/" + escaped + "/@v/" + escapedVers + ".info")
	if err != nil {
		return importsModuleInfo{}, err
	}
	latest, err := importsFetchProxyInfo(base + "/" + escaped + "/@latest")
	if err != nil {
		return importsModuleInfo{}, err
	}
	return importsModuleInfo{published: current.Time, latest: latest.Version}, nil
}

type importsProxyInfo struct {
	Time    time.Time `json:"Time"`    //nolint:tagliatelle // field name defined by the proxy
	Version string    `json:"Version"` //nolint:tagliatelle // field name defined by the proxy
}

func importsFetchProxyInfo(url string) (info importsProxyInfo, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), importsProxyTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return info, fmt.Errorf("create request: %w", err)
	}
	res, err := importsHTTPClient.Do(req)
	if err != nil {
		return info, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer func() {
		if cerr := res.Body.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close response of %s: %w", url, cerr)
		}
	}()

	if res.StatusCode != http.StatusOK {
		return info, fmt.Errorf("fetch %s: unexpected status %d", url, res.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&info); err != nil {
		return info, fmt.Errorf("parse %s: %w", url, err)
	}
	return info, nil
}
