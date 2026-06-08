package internal

import (
	"strings"
)

var contribModuleRenames = map[string]string{
	"fibernewrelic": "newrelic",
	"fiberi18n":     "i18n",
	"fibersentry":   "sentry",
	"fiberzap":      "zap",
	"fiberzerolog":  "zerolog",
	"otelfiber":     "otel",
}

func NormalizeContribModule(module string) string {
	parts := strings.Split(module, "/")
	if len(parts) == 0 {
		return module
	}

	renamed, ok := contribModuleRenames[parts[0]]
	if !ok {
		return module
	}

	rest := parts[1:]
	if len(rest) > 0 && isContribModuleMajorVersion(rest[0]) {
		rest = rest[1:]
	}

	if len(rest) == 0 {
		return renamed
	}
	return renamed + "/" + strings.Join(rest, "/")
}

func ContribModuleAliases(module string) []string {
	aliases := []string{module}
	for legacy, renamed := range contribModuleRenames {
		if renamed == module && legacy != module {
			aliases = append(aliases, legacy)
		}
	}
	return aliases
}

func isContribModuleMajorVersion(segment string) bool {
	if len(segment) < 2 || segment[0] != 'v' {
		return false
	}
	for _, ch := range segment[1:] {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}
