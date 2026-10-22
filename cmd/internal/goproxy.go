package internal

import (
	"os"
	"strings"
)

// GoProxyBase returns the module proxy to ask about versions: the first http(s)
// entry of GOPROXY, or proxy.golang.org when it is not set, so that a network
// that only reaches a private proxy works. ok is false when GOPROXY allows no
// proxy at all (off, or direct only).
func GoProxyBase() (base string, ok bool) {
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
