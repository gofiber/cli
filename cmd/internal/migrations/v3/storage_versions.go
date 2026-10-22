package v3

import (
	"fmt"
	"time"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"
	"golang.org/x/mod/modfile"
	xsemver "golang.org/x/mod/semver"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/gomod"
)

// storageLine is one module line of github.com/gofiber/storage: a module at a
// major version. minimum is the first release of the line that implements the
// storage interface of Fiber v3, whose methods all take a context as well
// (GetWithContext, SetWithContext, DeleteWithContext and ResetWithContext), and
// that go mod tidy can resolve. An older release of the line builds with Fiber
// v3 but is rejected as a fiber.Storage. target marks the line that imports are
// moved to, the one Fiber v3 was released with.
type storageLine struct {
	name    string
	major   string
	minimum string
	target  bool
}

// storageLines lists the storage modules the CLI knows. The minimums were found
// by reading every release on the module proxy for the four methods and running
// go mod tidy on a module that imports the release. The first releases of
// redis/v3 (3.3.0, 3.4.0), valkey (0.2.0) and rueidis (1.3.0, 1.3.1) have the
// methods but require a placeholder module for their tests, and tidy fails for a
// project that uses them; their minimum is the first one that tidies. The table
// only needs a row when a module gets a new major version. Modules that are not
// listed are looked up on the proxy (see storageMigration.unknown).
//
//nolint:goconst // each module's first usable release moves on its own
var storageLines = []storageLine{
	// v3 adapters
	{name: "postgres", major: "v3", minimum: "v3.3.0", target: true},
	{name: "redis", major: "v3", minimum: "v3.4.1", target: true},

	// v2 adapters
	{name: "arangodb", major: "v2", minimum: "v2.2.0", target: true},
	{name: "azureblob", major: "v2", minimum: "v2.2.0", target: true},
	{name: "badger", major: "v2", minimum: "v2.1.0", target: true},
	{name: "bbolt", major: "v2", minimum: "v2.1.0", target: true},
	{name: "couchbase", major: "v2", minimum: "v2.2.0", target: true},
	{name: "dynamodb", major: "v2", minimum: "v2.2.0", target: true},
	{name: "etcd", major: "v2", minimum: "v2.1.0", target: true},
	{name: "memcache", major: "v2", minimum: "v2.1.0", target: true},
	{name: "memory", major: "v2", minimum: "v2.1.0", target: true},
	{name: "mongodb", major: "v2", minimum: "v2.2.0", target: true},
	{name: "mssql", major: "v2", minimum: "v2.1.0", target: true},
	{name: "mysql", major: "v2", minimum: "v2.2.0", target: true},
	{name: "pebble", major: "v2", minimum: "v2.1.0", target: true},
	{name: "ristretto", major: "v2", minimum: "v2.1.0", target: true},
	{name: "s3", major: "v2", minimum: "v2.4.0", target: true},
	{name: "sqlite3", major: "v2", minimum: "v2.2.0", target: true},

	// Newer lines of a listed module. They are kept as they are and only raised.
	{name: "mongodb", major: "v3", minimum: "v3.0.0"},

	// Modules without a major version suffix, still on v0 or v1. Their path does
	// not change, but their old releases do not implement the interface either.
	{name: "aerospike", minimum: "v0.2.0", target: true},
	{name: "cassandra", minimum: "v0.2.0", target: true},
	{name: "clickhouse", minimum: "v0.3.0", target: true},
	{name: "cloudflarekv", minimum: "v0.2.0", target: true},
	{name: "coherence", minimum: "v1.4.0", target: true},
	{name: "leveldb", minimum: "v0.2.3", target: true},
	{name: "minio", minimum: "v0.4.0", target: true},
	{name: "mockstorage", minimum: "v1.1.0", target: true},
	{name: "nats", minimum: "v1.3.0", target: true},
	{name: "neo4j", minimum: "v0.3.0", target: true},
	{name: "rueidis", minimum: "v1.3.2", target: true},
	{name: "scylladb", minimum: "v0.2.0", target: true},
	{name: "surrealdb", minimum: "v0.2.0", target: true},
	{name: "valkey", minimum: "v0.2.1", target: true},
}

// storageWithContextRelease is when the *WithContext methods landed in the
// storage modules. No release of any module before it implements fiber.Storage.
var storageWithContextRelease = time.Date(2025, time.July, 28, 0, 0, 0, 0, time.UTC)

const storagePrefix = "github.com/gofiber/storage/"

var storageFamily = newImportsFamily(storagePrefix, resolveStorage)

// resolveStorage says where the storage module name on the major version line
// goes: to the line Fiber v3 was released with, unless it is on a newer one,
// which is kept. Every line gets the lowest release that implements
// fiber.Storage, when that is known.
func resolveStorage(name, major string) importsDestination {
	var target string
	known := false
	for _, l := range storageLines {
		if l.name == name && l.target {
			target, known = l.major, true
			break
		}
	}
	if !known {
		return importsDestination{}
	}

	line := major
	if importsMajorRank(major) < importsMajorRank(target) {
		line = target
	}

	dest := importsDestination{path: importsModulePath(storagePrefix, name, line), known: true}
	for _, l := range storageLines {
		if l.name == name && l.major == line {
			dest.minimum = l.minimum
			break
		}
	}
	return dest
}

// MigrateStorageVersions moves the storage modules to the lines that work with
// Fiber v3: the import paths and the go.mod requirements are pointed at the major
// version Fiber v3 was released with, and a requirement on a release that predates
// the context methods of fiber.Storage is raised to the first release that has
// them. A version is never lowered, nor a newer major version replaced.
func MigrateStorageVersions(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changedImports, err := internal.ChangeFileContentInfo(cwd, migrateStorageImports)
	if err != nil {
		return fmt.Errorf("failed to migrate storage versions: %w", err)
	}

	m := &storageMigration{cwd: cwd}
	modChanged, err := gomod.Edit(cwd, func(f *gomod.File) error {
		storageFamily.retarget(f, func(r *modfile.Require, _, _ string) { m.unknown(f, r) })
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to migrate storage modules: %w", err)
	}

	if !changedImports && !modChanged {
		return nil
	}

	cmd.Println("Migrated storage package versions")
	return nil
}

// storageMigration migrates the go.mod files of a project.
type storageMigration struct {
	cwd string
}

// unknown handles a requirement of a storage module that the table does not
// list. Nothing says whether its release implements fiber.Storage, but none
// that was published before the context methods landed does: such a release is
// raised to the newest of its line. Without a proxy to ask, the requirement is
// left alone and reported.
func (m *storageMigration) unknown(f *gomod.File, r *modfile.Require) {
	if r.Indirect {
		return
	}

	note := func(format string, args ...any) {
		internal.AddNote(m.cwd, internal.Note{File: f.Rel, Line: gomod.Line(r.Syntax), Message: fmt.Sprintf(format, args...)})
	}

	info, err := importsLookupModule(r.Mod.Path, r.Mod.Version)
	switch {
	case err != nil:
		note("%s is not in the CLI's list of storage modules and the module proxy could not be asked (%v): make sure %s implements the *WithContext methods of fiber.Storage, `go get %s@latest` gets a release that does.",
			r.Mod.Path, err, r.Mod.Version, r.Mod.Path)
	case info.published.Before(storageWithContextRelease) && info.latest != "" && xsemver.Compare(info.latest, r.Mod.Version) > 0:
		f.SetRequire(r, r.Mod.Path, info.latest)
	case info.published.Before(storageWithContextRelease):
		note("%s %s predates the *WithContext methods of fiber.Storage and the module has no newer release.", r.Mod.Path, r.Mod.Version)
	}
}
