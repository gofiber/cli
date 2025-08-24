package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gofrs/flock"
)

var (
	cacheMu       sync.RWMutex
	responseCache = make(map[string][]byte)

	cacheDir = filepath.Join(os.TempDir(), "cli-httpcache")
	cacheTTL = 5 * time.Minute
)

func init() {
	if err := os.MkdirAll(cacheDir, 0o750); err != nil {
		log.Fatalf("httpcache: mkdir %s: %v", cacheDir, err)
	}
}

type cacheEntry struct {
	Expiry time.Time `json:"expiry"`
	Body   []byte    `json:"body"`
}

func cacheFile(url string) string {
	h := sha256.Sum256([]byte(url))
	return filepath.Join(cacheDir, hex.EncodeToString(h[:])+".json")
}

func readFromFile(url string) ([]byte, bool) {
	lock := flock.New(cacheFile(url) + ".lock")
	if err := lock.RLock(); err != nil {
		log.Printf("httpcache: acquire read lock for %s: %v", url, err)
		return nil, false
	}
	defer func() {
		if err := lock.Unlock(); err != nil {
			log.Printf("httpcache: release read lock for %s: %v", url, err)
		}
	}()

	b, err := os.ReadFile(cacheFile(url))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("httpcache: read cache file for %s: %v", url, err)
		}
		return nil, false
	}
	var e cacheEntry
	if err := json.Unmarshal(b, &e); err != nil {
		log.Printf("httpcache: unmarshal cache file for %s: %v", url, err)
		_ = os.Remove(cacheFile(url)) //nolint:errcheck // best effort cleanup
		return nil, false
	}
	if time.Now().After(e.Expiry) {
		log.Printf("httpcache: cache expired for %s", url)
		_ = os.Remove(cacheFile(url)) //nolint:errcheck // remove expired cache
		return nil, false
	}
	return e.Body, true
}

func writeToFile(url string, body []byte) {
	lock := flock.New(cacheFile(url) + ".lock")
	if err := lock.Lock(); err != nil {
		log.Printf("httpcache: acquire write lock for %s: %v", url, err)
		return
	}
	defer func() {
		if err := lock.Unlock(); err != nil {
			log.Printf("httpcache: release write lock for %s: %v", url, err)
		}
	}()

	e := cacheEntry{Expiry: time.Now().Add(cacheTTL), Body: body}
	b, err := json.Marshal(e)
	if err != nil {
		log.Printf("httpcache: marshal cache entry for %s: %v", url, err)
		return
	}
	if err := os.WriteFile(cacheFile(url), b, 0o600); err != nil {
		log.Printf("httpcache: write cache file for %s: %v", url, err)
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

	cacheMu.Lock()
	defer cacheMu.Unlock()

	if b, ok := responseCache[url]; ok {
		return b, http.StatusOK, nil
	}

	if b, ok := readFromFile(url); ok {
		responseCache[url] = b
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
		responseCache[url] = body
		writeToFile(url, body)
	}

	return body, res.StatusCode, nil
}

// clearHTTPCache removes all cached responses. It is intended for testing.
func clearHTTPCache() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	responseCache = make(map[string][]byte)
	_ = os.RemoveAll(cacheDir)       //nolint:errcheck // best effort cleanup
	_ = os.MkdirAll(cacheDir, 0o750) //nolint:errcheck // recreate cache dir
}
