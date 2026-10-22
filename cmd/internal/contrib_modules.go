package internal

import (
	"strings"
)

// contribModuleRenames maps the name of a v2-era github.com/gofiber/contrib
// module to the name its v3 successor lives under
// (github.com/gofiber/contrib/v3/<name>).
var contribModuleRenames = map[string]string{
	"fibernewrelic": "newrelic",
	"fiberi18n":     "i18n",
	"fibersentry":   "sentry",
	"fiberzap":      "zap",
	"fiberzerolog":  "zerolog",
	"opafiber":      "opa",
	"otelfiber":     "otel",
}

// contribPackageNames lists the contrib modules whose Go package name is not the
// name of the directory they live in. Every other module, the renamed ones
// included, declares the package it is named after (fiberzap/v2 declares
// fiberzap, v3/zap declares zap).
var contribPackageNames = map[string]string{
	"jwt":    "jwtware",
	"paseto": "pasetoware",
}

// NormalizeContribModule returns the name of the v3 module that replaces the v2
// era module. module is the part of the path after github.com/gofiber/contrib/,
// it may carry a major version element (otelfiber/v2) and a sub-package path
// (otelfiber/v2/middleware); both v2 era spellings give the same result.
func NormalizeContribModule(module string) string {
	parts := strings.Split(module, "/")
	if len(parts) == 0 {
		return module
	}

	name := parts[0]
	if renamed, ok := contribModuleRenames[name]; ok {
		name = renamed
	}

	rest := parts[1:]
	if len(rest) > 0 && isContribModuleMajorVersion(rest[0]) {
		rest = rest[1:]
	}

	if len(rest) == 0 {
		return name
	}
	return name + "/" + strings.Join(rest, "/")
}

// ContribModuleAliases returns module followed by the names the module had
// before it was renamed.
func ContribModuleAliases(module string) []string {
	aliases := []string{module}
	for legacy, renamed := range contribModuleRenames {
		if renamed == module && legacy != module {
			aliases = append(aliases, legacy)
		}
	}
	return aliases
}

// ContribPackageName returns the Go package name that code written against the
// v2 era module declares, for example "fiberzap" for fiberzap/v2 and "jwtware"
// for jwt. module is spelled as for NormalizeContribModule.
func ContribPackageName(module string) string {
	name, _, _ := strings.Cut(module, "/")
	if pkg, ok := contribPackageNames[name]; ok {
		return pkg
	}
	return name
}

// ContribV3PackageName returns the Go package name the v3 module declares. name
// is the module name as NormalizeContribModule returns it.
func ContribV3PackageName(name string) string {
	first, _, _ := strings.Cut(name, "/")
	if pkg, ok := contribPackageNames[first]; ok {
		return pkg
	}
	return first
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
