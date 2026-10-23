package migrations

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
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
	// Review, when set, runs after every migration and the go.mod work, if the
	// migration applied. It changes nothing: it records notes about what the
	// migrations could not finish or could not make behave the same.
	Review func(cwd string)
	// Dependencies, when set, runs after the go.mod work and before Review, if the
	// migration applied. tidied says that go.mod was tidied by it: false when the
	// go.mod work was skipped or failed. It looks at the dependencies of the
	// project, which only a tidied go.mod and a module cache that holds them can
	// tell, and records notes.
	Dependencies func(cwd string, tidied bool)
	From         string
	To           string
	Functions    []MigrationFn
}

// Migrations is a list of all migrations
// Example structure:
// {"from": ">=2.0.0", "to": "<=3.*.*", "fn": [MigrateFN, MigrateFN]}
//
// Both ends are constraints: From is checked against the version the project
// is on, To against the version it is moving to. Pre-releases are part of a
// band only when the constraint names one (">=2.0.0-0 <3.0.0-0" is 2.x without
// the 3.0.0 pre-releases, ">=3.0.0-0 <3.0.0" is only those pre-releases).
var Migrations = []Migration{
	{From: ">=1.0.0-0", To: ">=0.0.0-0", Functions: []MigrationFn{MigrateGoPkgs}},
	{
		// v2 to v3. A project that is already on v3 must not get the v2 rewrites
		// again, and a v2 target must not get v3 code.
		From:         ">=2.0.0-0 <3.0.0-0",
		To:           ">=3.0.0-0 <4.0.0-0",
		Review:       v3migrations.Review,
		Dependencies: v3migrations.ReviewDependencies,
		Functions: []MigrationFn{
			v3migrations.MigrateHandlerSignatures,
			v3migrations.MigrateViewBind,
			v3migrations.MigrateParserMethods,
			v3migrations.MigrateParamsTagKeys,
			v3migrations.MigrateRedirectMethods,
			v3migrations.MigrateClientUsage,
			v3migrations.MigrateGenericHelpers,
			v3migrations.MigrateAddMethod,
			v3migrations.MigrateMimeConstants,
			v3migrations.MigrateCoreIdentifiers,
			v3migrations.MigrateLoggerTags,
			v3migrations.MigrateLoggerGenerics,
			v3migrations.MigrateLoggerConfig,
			v3migrations.MigrateStaticRoutes,
			v3migrations.MigrateTrustedProxyConfig,
			v3migrations.MigrateAppConfig,
			v3migrations.MigrateMount,
			v3migrations.MigrateConfigListenerFields,
			v3migrations.MigrateShutdownHook,
			v3migrations.MigrateListenMethods,
			v3migrations.MigrateContextMethods,
			v3migrations.MigrateCtxRenames,
			v3migrations.MigrateHeaderMaps,
			v3migrations.MigrateOlderV2Callbacks,
			v3migrations.MigrateCtxBehaviorNotes,
			v3migrations.MigrateSendFileConfig,
			v3migrations.MigrateContextMirrors,
			v3migrations.MigrateCORSConfig,
			v3migrations.MigrateCSRFConfig,
			v3migrations.MigrateMonitorImport,
			v3migrations.MigrateSwaggerPackages,
			v3migrations.MigrateContribPackages,
			v3migrations.MigrateSentryAPI,
			v3migrations.MigrateOtelOptions,
			v3migrations.MigrateWebsocketConfig,
			v3migrations.MigrateZerologConfig,
			v3migrations.MigrateI18nTranslator,
			v3migrations.MigrateUtilsImport,
			v3migrations.MigrateHealthcheckConfig,
			v3migrations.MigrateProxyTLSConfig,
			v3migrations.MigrateAppTestConfig,
			v3migrations.MigrateMiddlewareLocals,
			v3migrations.MigrateFilesystemMiddleware,
			v3migrations.MigrateLimiterConfig,
			v3migrations.MigrateEncryptcookieConfig,
			v3migrations.MigrateCacheConfig,
			v3migrations.MigrateEnvVarConfig,
			v3migrations.MigrateFaviconConfig,
			v3migrations.MigrateSessionConfig,
			v3migrations.MigrateSessionExtractor,
			v3migrations.MigrateSessionStore,
			v3migrations.MigrateStorageVersions,
			v3migrations.MigrateStorageInterface,
			v3migrations.MigrateTemplateVersions,
			MigrateReplaceDirectives,
			v3migrations.MigrateSessionRelease,
			v3migrations.MigrateSessionMethods,
			v3migrations.MigrateKeyAuthConfig,
			v3migrations.MigrateJWTExtractor,
			v3migrations.MigrateJWTClaims,
			v3migrations.MigratePasetoExtractor,
			v3migrations.MigrateTimeoutConfig,
			v3migrations.MigrateBasicauthAuthorizer,
			v3migrations.MigrateBasicauthConfig,
			v3migrations.MigrateReqHeaderParser,
			MigrateGoVersion,
		},
	},
	{
		// Only the v3 pre-releases (alpha, beta, rc) had the shutdown callbacks
		// that were dropped before 3.0.0, neither v2 nor v3 has them.
		From: ">=3.0.0-0 <3.0.0",
		To:   ">=3.0.0-0 <4.0.0-0",
		Functions: []MigrationFn{
			v3migrations.MigrateListenerCallbacks,
		},
	},
	{
		// Keyed on where the migration is going: redirect and rewrite gained
		// RuleList in 3.6.0, and it is a v3 field, so the target has to land in
		// that band. Any current version qualifies, since a v2 config carries
		// the same Rules map.
		From: ">=2.0.0-0",
		To:   ">=3.6.0-0 <4.0.0-0",
		Functions: []MigrationFn{
			v3migrations.MigrateRuleList,
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

// matches reports whether m applies when a project moves from curr to target.
func (m Migration) matches(curr, target *semver.Version) (bool, error) {
	toC, err := semver.NewConstraint(m.To)
	if err != nil {
		return false, fmt.Errorf("parse to constraint %s: %w", m.To, err)
	}
	fromC, err := semver.NewConstraint(m.From)
	if err != nil {
		return false, fmt.Errorf("parse from constraint %s: %w", m.From, err)
	}

	return fromC.Check(curr) && toC.Check(target), nil
}

// runMigration runs fn and, in verbose mode, reports whether it changed anything.
func runMigration(cmd *cobra.Command, fn MigrationFn, name, cwd string, curr, target *semver.Version, verbose bool) error {
	if !verbose {
		return fn(cmd, cwd, curr, target)
	}

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
	return err
}

// DoMigration runs all migrations
// It will run all migrations that match the current and target version
func DoMigration(cmd *cobra.Command, cwd string, curr, target *semver.Version, skipGoMod, verbose bool, includeFiles, excludeFiles []string) error {
	internal.SetFileFilters(includeFiles, excludeFiles)
	defer internal.SetFileFilters(nil, nil)
	// Whatever an earlier run in this process left behind is not ours to report.
	internal.DrainNotes(cwd)
	// The files are formatted as they are rewritten; the text the author wrote is
	// put back where no migration changed it once they all ran.
	internal.BeginRun(cwd)
	defer internal.AbandonRun(cwd)
	var errs []error
	var notes []internal.Note
	var reviews []func(cwd string)
	var dependencies []func(cwd string, tidied bool)
	var origDeps map[string]map[string]*semver.Version
	if !skipGoMod {
		var err error
		origDeps, err = dependencyVersions(cwd)
		if err != nil {
			return fmt.Errorf("record dependencies: %w", err)
		}
	}
	for _, m := range Migrations {
		applies, err := m.matches(curr, target)
		if err != nil {
			return err
		}

		if !applies {
			if verbose {
				cmd.Printf("Skipping migration from %s to %s\n", m.From, m.To)
			}
			continue
		}

		if m.Review != nil {
			reviews = append(reviews, m.Review)
		}
		if m.Dependencies != nil {
			dependencies = append(dependencies, m.Dependencies)
		}

		for _, fn := range m.Functions {
			name := migrationName(fn)
			if err := runMigration(cmd, fn, name, cwd, curr, target, verbose); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", name, err))
			}
			for _, n := range internal.DrainNotes(cwd) {
				n.Migration = name
				notes = append(notes, n)
			}
		}
	}

	moved, err := internal.FinishRun(cwd)
	if err != nil {
		errs = append(errs, fmt.Errorf("restore the formatting: %w", err))
	}
	notes = followLines(notes, moved)

	tidied := false
	if !skipGoMod {
		if err := internal.RunGoMod(cwd); err != nil {
			errs = append(errs, fmt.Errorf("go mod: %w", err))
			internal.AddNote(cwd, internal.Note{Message: goModFailedNote})
		}
		if err := MigrateDependencies(cmd, cwd, origDeps, target); err != nil {
			errs = append(errs, fmt.Errorf("migrate dependencies: %w", err))
		}
		if err := internal.RunGoMod(cwd); err != nil {
			errs = append(errs, fmt.Errorf("go mod: %w", err))
			internal.AddNote(cwd, internal.Note{Message: goModFailedNote})
		} else {
			tidied = true
		}
	}
	for _, review := range dependencies {
		review(cwd, tidied)
	}
	for _, review := range reviews {
		review(cwd)
	}
	notes = append(notes, internal.DrainNotes(cwd)...)

	printNotes(cmd, notes)

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

// goModFailedNote says what state go.mod is in when the go command failed.
const goModFailedNote = "go mod tidy failed (see its output above), so go.mod and go.sum hold the edits of the migration (the requirements moved to the v3 modules, " +
	"the raised go directive and requirements) but are not tidied; fix the cause and run go mod tidy and go mod download"

// followLines gives the notes the line numbers of the files as they are after the
// formatting of the author was put back (see internal.FinishRun).
func followLines(notes []internal.Note, moved map[string]func(line int) int) []internal.Note {
	for i, n := range notes {
		if remap, ok := moved[filepath.ToSlash(n.File)]; ok && n.Line > 0 {
			notes[i].Line = remap(n.Line)
		}
	}
	return notes
}

// printNotes tells the user what the migrations left for them to look at. The
// same finding reported by several migrations is only listed once, and the notes
// of one line are one item: the first is the line, the others follow it.
func printNotes(cmd *cobra.Command, notes []internal.Note) {
	type key struct {
		file    string
		message string
		line    int
	}
	seen := make(map[key]struct{}, len(notes))
	unique := make([]internal.Note, 0, len(notes))
	for _, n := range notes {
		k := key{file: n.File, line: n.Line, message: n.Message}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		unique = append(unique, n)
	}
	if len(unique) == 0 {
		return
	}

	slices.SortStableFunc(unique, func(a, b internal.Note) int {
		return cmp.Or(
			cmp.Compare(a.File, b.File),
			cmp.Compare(a.Line, b.Line),
		)
	})

	// The notes are sorted by place, so those of one line are next to each other.
	var items [][]internal.Note
	for _, n := range unique {
		if last := len(items) - 1; last >= 0 && n.File != "" && n.Line > 0 && items[last][0].File == n.File && items[last][0].Line == n.Line {
			items[last] = append(items[last], n)
			continue
		}
		items = append(items, []internal.Note{n})
	}

	cmd.Printf("\n%d %s to review by hand:\n", len(items), pluralize(len(items), "item", "items"))
	for _, item := range items {
		cmd.Printf("  %s\n", item[0])
		for _, n := range item[1:] {
			cmd.Printf("      also: %s\n", internal.Note{Migration: n.Migration, Message: n.Message})
		}
	}
}

func pluralize(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
