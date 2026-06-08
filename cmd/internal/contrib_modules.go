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
	if renamed, ok := contribModuleRenames[module]; ok {
		return renamed
	}
	if base, ok := trimContribModuleMajorSuffix(module); ok {
		if renamed, exists := contribModuleRenames[base]; exists {
			return renamed
		}
	}
	return module
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

func trimContribModuleMajorSuffix(module string) (string, bool) {
	idx := strings.LastIndex(module, "/v")
	if idx < 0 || idx+2 >= len(module) {
		return "", false
	}
	for _, ch := range module[idx+2:] {
		if ch < '0' || ch > '9' {
			return "", false
		}
	}
	return module[:idx], true
}
