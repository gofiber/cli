package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	cacheMu       sync.RWMutex
	responseCache = make(map[string][]byte)

	cacheDir = filepath.Join(os.TempDir(), "cli-httpcache")
	cacheTTL = 5 * time.Minute
)

type cacheEntry struct {
	Expiry time.Time `json:"expiry"`
	Body   []byte    `json:"body"`
}

func cacheFile(url string) string {
	h := sha256.Sum256([]byte(url))
	return filepath.Join(cacheDir, hex.EncodeToString(h[:])+".json")
}

func readFromFile(url string) ([]byte, bool) {
	b, err := os.ReadFile(cacheFile(url))
	if err != nil {
		return nil, false
	}
	var e cacheEntry
	if err := json.Unmarshal(b, &e); err != nil {
		_ = os.Remove(cacheFile(url)) //nolint:errcheck // best effort cleanup
		return nil, false
	}
	if time.Now().After(e.Expiry) {
		_ = os.Remove(cacheFile(url)) //nolint:errcheck // remove expired cache
		return nil, false
	}
	return e.Body, true
}

func writeToFile(url string, body []byte) {
	if err := os.MkdirAll(cacheDir, 0o750); err != nil {
		return
	}
	e := cacheEntry{Expiry: time.Now().Add(cacheTTL), Body: body}
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	if err := os.WriteFile(cacheFile(url), b, 0o600); err != nil {
		return
	}
}

// cachedGET performs an HTTP GET request and caches successful responses.
// Headers may be nil. Only responses with status 200 are cached.
func cachedGET(ctx context.Context, url string, headers map[string]string) (body []byte, status int, err error) {
	cacheMu.RLock()
	if b, ok := responseCache[url]; ok {
		cacheMu.RUnlock()
		return b, http.StatusOK, nil
	}
	cacheMu.RUnlock()

	if b, ok := readFromFile(url); ok {
		cacheMu.Lock()
		responseCache[url] = b
		cacheMu.Unlock()
		return b, http.StatusOK, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("do request: %w", err)
	}
	defer func() {
		if cerr := res.Body.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	body, err = io.ReadAll(res.Body)
	if err != nil {
		return nil, res.StatusCode, fmt.Errorf("read response: %w", err)
	}

	if res.StatusCode == http.StatusOK {
		cacheMu.Lock()
		responseCache[url] = body
		cacheMu.Unlock()
		writeToFile(url, body)
	}

	return body, res.StatusCode, nil
}

// clearHTTPCache removes all cached responses. It is intended for testing.
func clearHTTPCache() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	responseCache = make(map[string][]byte)
	_ = os.RemoveAll(cacheDir) //nolint:errcheck // best effort cleanup
}
