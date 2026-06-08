package internal

var contribModuleRenames = map[string]string{
	"fibernewrelic": "newrelic",
	"fibersentry":   "sentry",
	"fiberzap":      "zap",
	"otelfiber":     "otel",
	"otelfiber/v2":  "otel",
}

func NormalizeContribModule(module string) string {
	if renamed, ok := contribModuleRenames[module]; ok {
		return renamed
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
