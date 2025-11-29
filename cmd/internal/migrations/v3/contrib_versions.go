package v3

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const contribV3ProxyPrefix = "https://proxy.golang.org/github.com/gofiber/contrib/v3/"

var (
	contribV3VersionMu      sync.Mutex
	contribV3VersionCache   = make(map[string]string)
	contribV3VersionFetcher = fetchContribV3Version
)

func contribV3Version(module string) (string, error) {
	contribV3VersionMu.Lock()
	if v, ok := contribV3VersionCache[module]; ok {
		contribV3VersionMu.Unlock()
		return v, nil
	}
	fetcher := contribV3VersionFetcher
	contribV3VersionMu.Unlock()

	v, err := fetcher(module)
	if err != nil {
		return "", err
	}

	contribV3VersionMu.Lock()
	contribV3VersionCache[module] = v
	contribV3VersionMu.Unlock()
	return v, nil
}

func fetchContribV3Version(module string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	url := contribV3ProxyPrefix + module + "/@latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch latest version: %w", err)
	}
	defer func() {
		if cerr := res.Body.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch latest version: unexpected status %d", res.StatusCode)
	}

	var data struct {
		Version string `json:"Version"` //nolint:tagliatelle // field name defined by proxy
	}
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return "", fmt.Errorf("parse latest version: %w", err)
	}
	if data.Version == "" {
		return "", fmt.Errorf("latest version not found for %s", module)
	}

	return data.Version, nil
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
