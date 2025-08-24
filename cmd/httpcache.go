package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
)

var (
	cacheMu       sync.RWMutex
	responseCache = make(map[string][]byte)
)

// cachedGET performs an HTTP GET request and caches successful responses.
// Headers may be nil. Only responses with status 200 are cached.
func cachedGET(ctx context.Context, url string, headers map[string]string) (body []byte, status int, err error) {
	cacheMu.RLock()
	if b, ok := responseCache[url]; ok {
		cacheMu.RUnlock()
		return b, http.StatusOK, nil
	}
	cacheMu.RUnlock()

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
	}

	return body, res.StatusCode, nil
}

// clearHTTPCache removes all cached responses. It is intended for testing.
func clearHTTPCache() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	responseCache = make(map[string][]byte)
}
