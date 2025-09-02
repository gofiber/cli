package migrations

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"runtime"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
	v3migrations "github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// MigrationFn is a function that will be called during migration
type MigrationFn func(cmd *cobra.Command, cwd string, curr, target *semver.Version) error

// Migration is a single migration
type Migration struct {
	From      string
	To        string
	Functions []MigrationFn
}

// Migrations is a list of all migrations
// Example structure:
// {"from": ">=2.0.0", "to": "<=3.*.*", "fn": [MigrateFN, MigrateFN]}
var Migrations = []Migration{
	{From: ">=1.0.0-0", To: ">=0.0.0-0", Functions: []MigrationFn{MigrateGoPkgs}},
	{
		From: ">=2.0.0-0",
		To:   "<4.0.0-0",
		Functions: []MigrationFn{
			v3migrations.MigrateHandlerSignatures,
			v3migrations.MigrateViewBind,
			v3migrations.MigrateParserMethods,
			v3migrations.MigrateRedirectMethods,
			v3migrations.MigrateGenericHelpers,
			v3migrations.MigrateAddMethod,
			v3migrations.MigrateMimeConstants,
			v3migrations.MigrateLoggerTags,
			v3migrations.MigrateLoggerGenerics,
			v3migrations.MigrateStaticRoutes,
			v3migrations.MigrateTrustedProxyConfig,
			v3migrations.MigrateMount,
			v3migrations.MigrateConfigListenerFields,
			v3migrations.MigrateListenerCallbacks,
			v3migrations.MigrateShutdownHook,
			v3migrations.MigrateListenMethods,
			v3migrations.MigrateContextMethods,
			v3migrations.MigrateCORSConfig,
			v3migrations.MigrateCSRFConfig,
			v3migrations.MigrateMonitorImport,
			v3migrations.MigrateUtilsImport,
			v3migrations.MigrateHealthcheckConfig,
			v3migrations.MigrateProxyTLSConfig,
			v3migrations.MigrateAppTestConfig,
			v3migrations.MigrateMiddlewareLocals,
			v3migrations.MigrateFilesystemMiddleware,
			v3migrations.MigrateLimiterConfig,
			v3migrations.MigrateCacheConfig,
			v3migrations.MigrateEnvVarConfig,
			v3migrations.MigrateSessionConfig,
			v3migrations.MigrateSessionExtractor,
			v3migrations.MigrateSessionStore,
			v3migrations.MigrateKeyAuthConfig,
			v3migrations.MigrateTimeoutConfig,
			v3migrations.MigrateBasicauthAuthorizer,
			v3migrations.MigrateBasicauthConfig,
			v3migrations.MigrateBasicauthStorePassword,
			v3migrations.MigrateReqHeaderParser,
			MigrateGoVersion("1.25"),
		},
	},
}

func migrationName(fn MigrationFn) string {
	f := runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name()
	f = strings.TrimSuffix(f, "-fm")
	if idx := strings.LastIndex(f, ".func"); idx >= 0 {
		f = f[:idx]
	}
	if idx := strings.LastIndex(f, "."); idx >= 0 {
		return f[idx+1:]
	}
	return f
}

// DoMigration runs all migrations
// It will run all migrations that match the current and target version
func DoMigration(cmd *cobra.Command, cwd string, curr, target *semver.Version, skipGoMod, verbose bool, includeFiles, excludeFiles []string) error {
	internal.SetFileFilters(includeFiles, excludeFiles)
	defer internal.SetFileFilters(nil, nil)
	var errs []error
	var origDeps map[string]map[string]*semver.Version
	if !skipGoMod {
		var err error
		origDeps, err = dependencyVersions(cwd)
		if err != nil {
			return fmt.Errorf("record dependencies: %w", err)
		}
	}
	for _, m := range Migrations {
		toC, err := semver.NewConstraint(m.To)
		if err != nil {
			return fmt.Errorf("parse to constraint %s: %w", m.To, err)
		}
		fromC, err := semver.NewConstraint(m.From)
		if err != nil {
			return fmt.Errorf("parse from constraint %s: %w", m.From, err)
		}

		if fromC.Check(curr) && toC.Check(target) {
			for _, fn := range m.Functions {
				name := migrationName(fn)
				if verbose {
					var buf bytes.Buffer
					origOut := cmd.OutOrStdout()
					cmd.SetOut(io.MultiWriter(origOut, &buf))
					err := fn(cmd, cwd, curr, target)
					cmd.SetOut(origOut)
					if buf.Len() == 0 {
						cmd.Printf("%s: no changes\n", name)
					} else {
						cmd.Printf("%s: changed\n", name)
					}
					if err != nil {
						errs = append(errs, fmt.Errorf("%s: %w", name, err))
					}
				} else {
					if err := fn(cmd, cwd, curr, target); err != nil {
						errs = append(errs, fmt.Errorf("%s: %w", name, err))
					}
				}
			}
		} else if verbose {
			cmd.Printf("Skipping migration from %s to %s\n", m.From, m.To)
		}
	}

	if !skipGoMod {
		if err := internal.RunGoMod(cwd); err != nil {
			errs = append(errs, fmt.Errorf("go mod: %w", err))
		}
		if err := MigrateDependencies(cmd, cwd, origDeps, target); err != nil {
			errs = append(errs, fmt.Errorf("migrate dependencies: %w", err))
		}
		if err := internal.RunGoMod(cwd); err != nil {
			errs = append(errs, fmt.Errorf("go mod: %w", err))
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}
