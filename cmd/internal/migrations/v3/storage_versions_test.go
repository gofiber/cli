package v3

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
)

// storageTree writes files (path relative to the returned directory -> content).
func storageTree(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
	return dir
}

func storageRead(t *testing.T, dir, name string) string {
	t.Helper()

	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name))) // #nosec G304 -- the path is this test's own temp dir
	require.NoError(t, err)
	return string(b)
}

// storageRun runs the migration and returns what it printed.
func storageRun(t *testing.T, dir string) string {
	t.Helper()

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	require.NoError(t, MigrateStorageVersions(cmd, dir, nil, nil))
	return buf.String()
}

func storageNotes(dir string) []string {
	var out []string
	for _, n := range internal.DrainNotes(dir) {
		out = append(out, n.String())
	}
	return out
}

// storageNoProxy makes every lookup on the module proxy fail, as it does offline.
func storageNoProxy(t *testing.T) {
	t.Helper()

	orig := importsLookupModule
	importsLookupModule = func(string, string) (importsModuleInfo, error) { return importsModuleInfo{}, errImportsNoProxy }
	t.Cleanup(func() { importsLookupModule = orig })
}

func Test_MigrateStorageVersions(t *testing.T) {
	// Not parallel: the lookups on the module proxy are replaced.
	storageNoProxy(t)

	dir := storageTree(t, map[string]string{
		"main.go": `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/storage/sqlite3"
	"github.com/gofiber/storage/redis"
	"github.com/gofiber/storage/postgres"
)

func main() {
	_ = fiber.New()
	_ = sqlite3.New()
	_ = redis.New()
	_ = postgres.New()
}
`,
	})

	assert.Equal(t, "Migrated storage package versions\n", storageRun(t, dir))

	result := storageRead(t, dir, "main.go")
	assert.Contains(t, result, `"github.com/gofiber/storage/sqlite3/v2"`)
	assert.Contains(t, result, `"github.com/gofiber/storage/redis/v3"`)
	assert.Contains(t, result, `"github.com/gofiber/storage/postgres/v3"`)
	assert.NotContains(t, result, `"github.com/gofiber/storage/sqlite3"`)
	assert.NotContains(t, result, `"github.com/gofiber/storage/redis"`)
	assert.NotContains(t, result, `"github.com/gofiber/storage/postgres"`)
}

func Test_MigrateStorageVersions_AlreadyOnTheLine(t *testing.T) {
	storageNoProxy(t)

	src := `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/storage/redis/v3"
	"github.com/gofiber/storage/sqlite3/v2"
)

func main() {
	_ = fiber.New()
	_ = sqlite3.New()
	_ = redis.New()
}
`
	dir := storageTree(t, map[string]string{
		"main.go": src,
		"go.mod":  "module example\n\nrequire (\n\tgithub.com/gofiber/storage/redis/v3 v3.6.2\n\tgithub.com/gofiber/storage/sqlite3/v2 v2.3.1\n)\n",
	})

	assert.Empty(t, storageRun(t, dir))
	assert.Equal(t, src, storageRead(t, dir, "main.go"))
	assert.Equal(t, "module example\n\nrequire (\n\tgithub.com/gofiber/storage/redis/v3 v3.6.2\n\tgithub.com/gofiber/storage/sqlite3/v2 v2.3.1\n)\n", storageRead(t, dir, "go.mod"))
}

func Test_MigrateStorageVersions_MixedVersions(t *testing.T) {
	storageNoProxy(t)

	dir := storageTree(t, map[string]string{
		"main.go": `package main

import (
	"github.com/gofiber/storage/sqlite3"
	"github.com/gofiber/storage/redis/v3"
	"github.com/gofiber/storage/postgres"
)

var _, _, _ = sqlite3.New, redis.New, postgres.New
`,
	})

	storageRun(t, dir)

	result := storageRead(t, dir, "main.go")
	assert.Contains(t, result, `"github.com/gofiber/storage/sqlite3/v2"`)
	assert.Contains(t, result, `"github.com/gofiber/storage/postgres/v3"`)
	assert.Contains(t, result, `"github.com/gofiber/storage/redis/v3"`)
	assert.NotContains(t, result, `"github.com/gofiber/storage/sqlite3"`)
	assert.NotContains(t, result, `"github.com/gofiber/storage/postgres"`)
}

func Test_MigrateStorageVersions_V2ToV3(t *testing.T) {
	storageNoProxy(t)

	dir := storageTree(t, map[string]string{
		"main.go": `package main

import (
	"github.com/gofiber/storage/redis/v2"
	"github.com/gofiber/storage/postgres/v2"
	"github.com/gofiber/storage/sqlite3/v2"
)

var _, _, _ = redis.New, postgres.New, sqlite3.New
`,
		"go.mod": "module example\n\nrequire (\n\tgithub.com/gofiber/storage/redis/v2 v2.0.3\n\tgithub.com/gofiber/storage/postgres/v2 v2.0.3\n\tgithub.com/gofiber/storage/sqlite3/v2 v2.0.0\n)\n",
	})

	storageRun(t, dir)

	result := storageRead(t, dir, "main.go")
	assert.Contains(t, result, `"github.com/gofiber/storage/redis/v3"`)
	assert.Contains(t, result, `"github.com/gofiber/storage/postgres/v3"`)
	assert.Contains(t, result, `"github.com/gofiber/storage/sqlite3/v2"`, "sqlite3 stays at v2")
	assert.Equal(t, "module example\n\nrequire (\n\tgithub.com/gofiber/storage/redis/v3 v3.4.1\n\tgithub.com/gofiber/storage/postgres/v3 v3.3.0\n\tgithub.com/gofiber/storage/sqlite3/v2 v2.2.0\n)\n", storageRead(t, dir, "go.mod"))
}

func Test_MigrateStorageVersions_UnversionedModulesStayUnversioned(t *testing.T) {
	storageNoProxy(t)

	src := `package main

import (
	"github.com/gofiber/storage/aerospike"
	"github.com/gofiber/storage/nats"
)

var _, _ = aerospike.New, nats.New
`
	dir := storageTree(t, map[string]string{"main.go": src})

	assert.Empty(t, storageRun(t, dir))
	assert.Equal(t, src, storageRead(t, dir, "main.go"))
}

// Modules that were already on the line Fiber v3 uses in the v2 era, or that
// the table used to skip, still did not implement fiber.Storage after the run.
func Test_MigrateStorageVersions_RaisesReleasesWithoutTheContextMethods(t *testing.T) {
	storageNoProxy(t)

	dir := storageTree(t, map[string]string{
		"go.mod": `module example

go 1.25

require (
	github.com/gofiber/fiber/v3 v3.6.1
	github.com/gofiber/storage/memory/v2 v2.0.0
	github.com/gofiber/storage/nats v1.2.0
	github.com/gofiber/storage/redis/v3 v3.0.0
	github.com/gofiber/storage/sqlite3/v2 v2.0.0
	github.com/gofiber/storage/valkey v0.1.1
	github.com/gofiber/storage/leveldb v0.2.2
	github.com/gofiber/storage/postgres/v3 v3.7.2
	github.com/gofiber/storage/s3/v2 v2.4.0
	github.com/gofiber/storage/etcd/v2 v2.1.0-rc1
)
`,
	})

	assert.Equal(t, "Migrated storage package versions\n", storageRun(t, dir))

	assert.Equal(t, `module example

go 1.25

require (
	github.com/gofiber/fiber/v3 v3.6.1
	github.com/gofiber/storage/memory/v2 v2.1.0
	github.com/gofiber/storage/nats v1.3.0
	github.com/gofiber/storage/redis/v3 v3.4.1
	github.com/gofiber/storage/sqlite3/v2 v2.2.0
	github.com/gofiber/storage/valkey v0.2.1
	github.com/gofiber/storage/leveldb v0.2.3
	github.com/gofiber/storage/postgres/v3 v3.7.2
	github.com/gofiber/storage/s3/v2 v2.4.0
	github.com/gofiber/storage/etcd/v2 v2.1.0
)
`, storageRead(t, dir, "go.mod"), "a newer or equal release is kept, a pre-release of the first release is raised")

	assert.Empty(t, storageRun(t, dir), "idempotent")
}

func Test_MigrateStorageVersions_MovesTheRequirementToTheNewLine(t *testing.T) {
	storageNoProxy(t)

	dir := storageTree(t, map[string]string{
		"main.go": `package main

import (
	"github.com/gofiber/storage/memory"
	"github.com/gofiber/storage/redis/v2"
)

var _, _ = memory.New, redis.New
`,
		"go.mod": `module example

require (
	github.com/gofiber/storage/memory v1.3.4 // indirect
	github.com/gofiber/storage/redis/v2 v2.0.3
	github.com/gofiber/storage/redis v1.3.4
)

replace github.com/gofiber/storage/memory => ../memory

replace (
	github.com/gofiber/storage/redis/v2 v2.0.3 => ../redis
)
`,
	})

	storageRun(t, dir)

	assert.Equal(t, `module example

require (
	github.com/gofiber/storage/memory/v2 v2.1.0 // indirect
	github.com/gofiber/storage/redis/v3 v3.4.1
)

replace github.com/gofiber/storage/memory/v2 => ../memory

replace (
	github.com/gofiber/storage/redis/v3 v3.4.1 => ../redis
)
`, storageRead(t, dir, "go.mod"), "two requirements of one module collapse into one, replace directives follow")
}

// mongodb has a v3 line now. Any major other than the table's was "changed",
// which moved mongodb/v3 back to v2.
func Test_MigrateStorageVersions_NeverMovesToAnOlderLine(t *testing.T) {
	storageNoProxy(t)

	src := `package main

import (
	"github.com/gofiber/storage/mongodb/v3"
	"github.com/gofiber/storage/postgres/v4"
)

var _, _ = mongodb.New, postgres.New
`
	mod := "module example\n\nrequire (\n\tgithub.com/gofiber/storage/mongodb/v3 v3.0.2\n\tgithub.com/gofiber/storage/postgres/v4 v4.0.0\n)\n"
	dir := storageTree(t, map[string]string{"main.go": src, "go.mod": mod})

	assert.Empty(t, storageRun(t, dir))
	assert.Equal(t, src, storageRead(t, dir, "main.go"))
	assert.Equal(t, mod, storageRead(t, dir, "go.mod"))

	// A newer line is still raised when the table knows its first release.
	dir = storageTree(t, map[string]string{"go.mod": "module example\n\nrequire github.com/gofiber/storage/mongodb/v3 v3.0.0-rc.1\n"})
	storageRun(t, dir)
	assert.Equal(t, "module example\n\nrequire github.com/gofiber/storage/mongodb/v3 v3.0.0\n", storageRead(t, dir, "go.mod"))
}

func Test_MigrateStorageVersions_EveryModule(t *testing.T) {
	storageNoProxy(t)

	dir := storageTree(t, map[string]string{
		"go.mod":        "module example\r\n\r\nrequire github.com/gofiber/storage/memory/v2 v2.0.0\r\n",
		"svc/go.mod":    "module example/svc\n\nrequire github.com/gofiber/storage/nats v1.2.0\n",
		"vendor/go.mod": "module vendor\n\nrequire github.com/gofiber/storage/nats v1.0.0\n",
		"tools/go.mod":  "module example/tools\n\nrequire github.com/gofiber/storage/testhelpers/tck v1.0.0\n",
	})

	storageRun(t, dir)

	assert.Equal(t, "module example\r\n\r\nrequire github.com/gofiber/storage/memory/v2 v2.1.0\r\n", storageRead(t, dir, "go.mod"))
	assert.Equal(t, "module example/svc\n\nrequire github.com/gofiber/storage/nats v1.3.0\n", storageRead(t, dir, "svc/go.mod"))
	assert.Equal(t, "module vendor\n\nrequire github.com/gofiber/storage/nats v1.0.0\n", storageRead(t, dir, "vendor/go.mod"))
	assert.Equal(t, "module example/tools\n\nrequire github.com/gofiber/storage/testhelpers/tck v1.0.0\n", storageRead(t, dir, "tools/go.mod"), "a package of a module is not a storage module")
}

func Test_MigrateStorageVersions_OnlyImportDeclarationsAreRewritten(t *testing.T) {
	storageNoProxy(t)

	dir := storageTree(t, map[string]string{
		"main.go": `package main

import "github.com/gofiber/storage/redis"

// see "github.com/gofiber/storage/redis" for the options
const path = "github.com/gofiber/storage/redis"

var _ = redis.New
`,
	})

	storageRun(t, dir)

	assert.Equal(t, `package main

import "github.com/gofiber/storage/redis/v3"

// see "github.com/gofiber/storage/redis" for the options
const path = "github.com/gofiber/storage/redis"

var _ = redis.New
`, storageRead(t, dir, "main.go"))
}

func Test_resolveStorage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, major string
		want        importsDestination
	}{
		{name: "memory", major: "", want: importsDestination{path: "github.com/gofiber/storage/memory/v2", minimum: "v2.1.0", known: true}},
		{name: "memory", major: "v2", want: importsDestination{path: "github.com/gofiber/storage/memory/v2", minimum: "v2.1.0", known: true}},
		{name: "redis", major: "v2", want: importsDestination{path: "github.com/gofiber/storage/redis/v3", minimum: "v3.4.1", known: true}},
		{name: "mongodb", major: "v3", want: importsDestination{path: "github.com/gofiber/storage/mongodb/v3", minimum: "v3.0.0", known: true}},
		{name: "mongodb", major: "v4", want: importsDestination{path: "github.com/gofiber/storage/mongodb/v4", known: true}},
		{name: "nats", major: "", want: importsDestination{path: "github.com/gofiber/storage/nats", minimum: "v1.3.0", known: true}},
		{name: "nats", major: "v2", want: importsDestination{path: "github.com/gofiber/storage/nats/v2", known: true}},
		{name: "somethingnew", major: "", want: importsDestination{}},
	}

	for _, tc := range tests {
		assert.Equal(t, tc.want, resolveStorage(tc.name, tc.major), "%s %s", tc.name, tc.major)
	}
}

// Every module the table moves has a first usable release for the line it moves
// to, and nothing is listed twice.
func Test_storageLines_Consistent(t *testing.T) {
	t.Parallel()

	type key struct{ name, major string }
	seen := map[key]bool{}
	targets := map[string]string{}
	for _, l := range storageLines {
		k := key{l.name, l.major}
		assert.False(t, seen[k], "%s %s is listed twice", l.name, l.major)
		seen[k] = true
		assert.NotEmpty(t, l.minimum, "%s %s", l.name, l.major)
		if l.target {
			_, dup := targets[l.name]
			assert.False(t, dup, "%s has two target lines", l.name)
			targets[l.name] = l.major
		}
	}

	for name, major := range targets {
		dest := resolveStorage(name, "")
		assert.True(t, dest.known, name)
		assert.Equal(t, importsModulePath(storagePrefix, name, major), dest.path, name)
		assert.NotEmpty(t, dest.minimum, name)
	}
}

func Test_MigrateStorageVersions_ModulesTheTableDoesNotList(t *testing.T) {
	// Not parallel: the lookups on the module proxy are replaced.
	cutoff := storageWithContextRelease

	tests := []struct {
		name       string
		info       importsModuleInfo
		err        error
		wantMod    string
		wantNotes  []string
		indirect   bool
		noLookupOK bool
	}{
		{
			name:    "released before the context methods landed",
			info:    importsModuleInfo{published: cutoff.Add(-24 * time.Hour), latest: "v1.4.0"},
			wantMod: "github.com/gofiber/storage/newdb v1.4.0",
		},
		{
			name:    "released after",
			info:    importsModuleInfo{published: cutoff.Add(24 * time.Hour), latest: "v1.4.0"},
			wantMod: "github.com/gofiber/storage/newdb v1.1.0",
		},
		{
			name:    "old and already the newest",
			info:    importsModuleInfo{published: cutoff.Add(-24 * time.Hour), latest: "v1.1.0"},
			wantMod: "github.com/gofiber/storage/newdb v1.1.0",
			wantNotes: []string{
				"go.mod:3: github.com/gofiber/storage/newdb v1.1.0 predates the *WithContext methods of fiber.Storage and the module has no newer release.",
			},
		},
		{
			name:    "the proxy cannot be asked",
			err:     errImportsNoProxy,
			wantMod: "github.com/gofiber/storage/newdb v1.1.0",
			wantNotes: []string{
				"go.mod:3: github.com/gofiber/storage/newdb is not in the CLI's list of storage modules and the module proxy could not be asked (no module proxy is configured (GOPROXY is off)): make sure github.com/gofiber/storage/newdb v1.1.0 implements the *WithContext methods of fiber.Storage, `go get github.com/gofiber/storage/newdb@latest` gets a release that does.",
			},
		},
		{
			name:       "an indirect requirement is not ours",
			indirect:   true,
			noLookupOK: true,
			wantMod:    "github.com/gofiber/storage/newdb v1.1.0 // indirect",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var lookups []string
			orig := importsLookupModule
			importsLookupModule = func(path, vers string) (importsModuleInfo, error) {
				lookups = append(lookups, path+"@"+vers)
				return tc.info, tc.err
			}
			t.Cleanup(func() { importsLookupModule = orig })

			line := "github.com/gofiber/storage/newdb v1.1.0"
			if tc.indirect {
				line += " // indirect"
			}
			dir := storageTree(t, map[string]string{"go.mod": "module example\n\nrequire " + line + "\n"})

			storageRun(t, dir)

			assert.Equal(t, "module example\n\nrequire "+tc.wantMod+"\n", storageRead(t, dir, "go.mod"))
			assert.Equal(t, tc.wantNotes, storageNotes(dir))
			if tc.noLookupOK {
				assert.Empty(t, lookups)
			} else {
				assert.Equal(t, []string{"github.com/gofiber/storage/newdb@v1.1.0"}, lookups)
			}
		})
	}
}

func Test_MigrateStorageVersions_ModulesTheTableListsNeedNoLookup(t *testing.T) {
	orig := importsLookupModule
	importsLookupModule = func(path, vers string) (importsModuleInfo, error) {
		t.Errorf("unexpected lookup of %s@%s", path, vers)
		return importsModuleInfo{}, errors.New("unexpected lookup")
	}
	t.Cleanup(func() { importsLookupModule = orig })

	dir := storageTree(t, map[string]string{
		"go.mod": "module example\n\nrequire (\n\tgithub.com/gofiber/storage/memory/v2 v2.0.0\n\tgithub.com/gofiber/storage/testhelpers/tck v1.0.0\n\tgithub.com/gofiber/storage v1.0.0\n)\n",
	})
	storageRun(t, dir)
	assert.Empty(t, storageNotes(dir))
}

// postgres.Config lost the SslMode spelling when the module got its v2: the field
// is SSLMode, the only field removed from the Config of any storage module.
func Test_MigrateStorageVersions_RenamesPostgresSslMode(t *testing.T) {
	storageNoProxy(t)

	dir := storageTree(t, map[string]string{
		"main.go": `package main

import (
	"github.com/gofiber/storage/mssql"
	pg "github.com/gofiber/storage/postgres"
)

type mine struct{ SslMode string }

func main() {
	_ = pg.New(pg.Config{Host: "localhost", SslMode: "disable"})

	cfg := pg.Config{}
	cfg.SslMode = "disable"

	ptr := &pg.Config{}
	ptr.SslMode = "require"

	var declared pg.Config
	declared.SslMode = "verify-full"

	_ = mssql.New(mssql.Config{SslMode: "disable"})
	_ = mine{SslMode: "x"}
	m := mine{}
	m.SslMode = "y"
}

func shadowed(pg struct{ Config struct{ SslMode string } }) {
	_ = pg.Config.SslMode
}
`,
	})

	storageRun(t, dir)

	assert.Equal(t, `package main

import (
	"github.com/gofiber/storage/mssql/v2"
	pg "github.com/gofiber/storage/postgres/v3"
)

type mine struct{ SslMode string }

func main() {
	_ = pg.New(pg.Config{Host: "localhost", SSLMode: "disable"})

	cfg := pg.Config{}
	cfg.SSLMode = "disable"

	ptr := &pg.Config{}
	ptr.SSLMode = "require"

	var declared pg.Config
	declared.SSLMode = "verify-full"

	_ = mssql.New(mssql.Config{SslMode: "disable"})
	_ = mine{SslMode: "x"}
	m := mine{}
	m.SslMode = "y"
}

func shadowed(pg struct{ Config struct{ SslMode string } }) {
	_ = pg.Config.SslMode
}
`, storageRead(t, dir, "main.go"), "only the postgres Config changes; mssql still has SslMode")
}

func Test_MigrateStorageVersions_NotesConnThatChangedItsType(t *testing.T) {
	storageNoProxy(t)

	dir := storageTree(t, map[string]string{
		"main.go": `package main

import (
	"github.com/gofiber/storage/postgres"
	"github.com/gofiber/storage/redis"
)

type app struct {
	db    *postgres.Storage
	cache *redis.Storage
	other interface{ Conn() int }
}

func (a *app) run() {
	_ = a.db.Conn()
	_ = a.cache.Conn()
	_ = a.other.Conn()
}

func main() {
	store := postgres.New(postgres.Config{})
	_ = store.Conn()
	_ = redis.New(redis.Config{}).Conn()

	var rs *redis.Storage
	_ = rs.Conn()

	redis := struct{ Conn func() int }{}
	_ = redis.Conn()
}
`,
		"already.go": `package main

import "github.com/gofiber/storage/postgres/v3"

func already() {
	_ = postgres.New(postgres.Config{}).Conn()
}
`,
	})

	storageRun(t, dir)

	assert.Equal(t, []string{
		"main.go:15: `Conn()` returns a *pgxpool.Pool in github.com/gofiber/storage/postgres/v3, it returned a *sql.DB in the module without a major version: adapt the code that uses the connection.",
		"main.go:16: `Conn()` returns a redis.UniversalClient in github.com/gofiber/storage/redis/v3, it returned a *redis.Client in the module without a major version: adapt the code that holds it.",
		"main.go:22: `Conn()` returns a *pgxpool.Pool in github.com/gofiber/storage/postgres/v3, it returned a *sql.DB in the module without a major version: adapt the code that uses the connection.",
		"main.go:23: `Conn()` returns a redis.UniversalClient in github.com/gofiber/storage/redis/v3, it returned a *redis.Client in the module without a major version: adapt the code that holds it.",
		"main.go:26: `Conn()` returns a redis.UniversalClient in github.com/gofiber/storage/redis/v3, it returned a *redis.Client in the module without a major version: adapt the code that holds it.",
	}, storageNotes(dir))
}
