package v3

import (
	"go/format"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const importsPostgresGoMod = "module example.com/app\n\ngo 1.25\n"

const importsPostgresShimPath = "example.com/app/internal/fibermigrate"

func Test_MigrateStorageVersions_WrapsConnOfThePostgresStorage(t *testing.T) {
	importsStorageNoProxy(t)

	dir := importsStorageTree(t, map[string]string{
		"go.mod": importsPostgresGoMod,
		"main.go": `package main

import (
	"database/sql"

	pg "github.com/gofiber/storage/postgres"
	"github.com/gofiber/storage/redis"
)

type server struct {
	store *pg.Storage
	cache *redis.Storage
}

var global = pg.New(pg.Config{})

func (s *server) db() *sql.DB { return s.store.Conn() }

func newStore() *pg.Storage { return pg.New(pg.Config{}) }

func main() {
	store := pg.New(pg.Config{Host: "127.0.0.1"})
	db := store.Conn()
	_ = db.Ping()
	_ = global.Conn().Ping()
	_ = newStore().Conn().Ping()
	_ = (&server{store: store}).db()
	var typed *pg.Storage = store
	_ = typed.Conn()
	_ = (&server{}).cache.Conn()
}
`,
		"other.go": `package main

func (s *server) ping() error { return s.store.Conn().Ping() }

func ping() error { return global.Conn().Ping() }
`,
		"v3/v3.go": `package v3

import "github.com/gofiber/storage/postgres/v3"

func use() { _ = postgres.New(postgres.Config{}).Conn() }
`,
	})

	out := importsStorageRun(t, dir)

	main := importsStorageRead(t, dir, "main.go")
	assert.Contains(t, main, `"`+importsPostgresShimPath+`"`)
	assert.Contains(t, main, "func (s *server) db() *sql.DB { return fibermigrate.PostgresDB(s.store.Conn()) }")
	assert.Contains(t, main, "db := fibermigrate.PostgresDB(store.Conn())")
	assert.Contains(t, main, "_ = fibermigrate.PostgresDB(global.Conn()).Ping()")
	assert.Contains(t, main, "_ = fibermigrate.PostgresDB(newStore().Conn()).Ping()")
	assert.Contains(t, main, "_ = fibermigrate.PostgresDB(typed.Conn())")
	// the other storage, and the one that is on the v3 line already, keep their calls
	assert.Contains(t, main, "(&server{}).cache.Conn()")
	assert.Contains(t, main, "github.com/gofiber/storage/postgres/v3")

	// a file that does not import the storage reaches it through the types of its package
	other := importsStorageRead(t, dir, "other.go")
	assert.Contains(t, other, "return fibermigrate.PostgresDB(s.store.Conn()).Ping()")
	assert.Contains(t, other, "return fibermigrate.PostgresDB(global.Conn()).Ping()")
	assert.Contains(t, other, `"`+importsPostgresShimPath+`"`)

	assert.Contains(t, importsStorageRead(t, dir, "v3/v3.go"), "postgres.New(postgres.Config{}).Conn()")
	assert.NotContains(t, importsStorageRead(t, dir, "v3/v3.go"), "fibermigrate")

	// the helper is in the module package, once
	shim := importsStorageRead(t, dir, "internal/fibermigrate/postgres.go")
	assert.Contains(t, shim, "package fibermigrate\n")
	assert.Contains(t, shim, "func PostgresDB(pool *pgxpool.Pool) *sql.DB")
	assert.Contains(t, out, "Migrated storage package versions")

	// only the note about the other storage is left
	assert.Equal(t, []string{
		"main.go:30: `Conn()` returns a redis.UniversalClient in github.com/gofiber/storage/redis/v3, it returned a *redis.Client in the module without a major version: adapt the code that holds it.",
	}, importsStorageNotes(dir))

	// the second run changes nothing
	before := map[string]string{"main.go": main, "other.go": other, "shim": shim}
	again := importsStorageRun(t, dir)
	assert.Empty(t, again)
	assert.Equal(t, before["main.go"], importsStorageRead(t, dir, "main.go"))
	assert.Equal(t, before["other.go"], importsStorageRead(t, dir, "other.go"))
	assert.Equal(t, before["shim"], importsStorageRead(t, dir, "internal/fibermigrate/postgres.go"))
}

func Test_MigrateStorageVersions_WrapsConnOfAStorageOfAnotherPackage(t *testing.T) {
	importsStorageNoProxy(t)

	dir := importsStorageTree(t, map[string]string{
		"go.mod": importsPostgresGoMod,
		"database/database.go": `package database

import (
	"github.com/gofiber/storage/postgres"
)

// Store is the storage of the service.
var Store = postgres.New(postgres.Config{})

// Holder keeps a storage.
type Holder struct {
	Storage *postgres.Storage
}

// Open returns a storage.
func Open() *postgres.Storage { return postgres.New(postgres.Config{}) }

// Sessions returns the holder of the service.
func (h *Holder) Sessions() *postgres.Storage { return h.Storage }
`,
		"cmd/api/main.go": `package main

import (
	"example.com/app/database"
)

type api struct {
	holder *database.Holder
}

func (a *api) run() {
	_ = database.Store.Conn().Ping()
	_ = database.Open().Conn().Ping()
	_ = a.holder.Storage.Conn().Ping()
	_ = a.holder.Sessions().Conn().Ping()
}

func main() {
	(&api{holder: &database.Holder{}}).run()
}
`,
		"cmd/api/unrelated.go": `package main

type other struct{}

func (other) Conn() int { return 1 }

func unrelated() int { return other{}.Conn() }
`,
	})

	importsStorageRun(t, dir)

	main := importsStorageRead(t, dir, "cmd/api/main.go")
	assert.Equal(t, 4, strings.Count(main, "fibermigrate.PostgresDB("), main)
	assert.Contains(t, main, "_ = fibermigrate.PostgresDB(database.Store.Conn()).Ping()")
	assert.Contains(t, main, "_ = fibermigrate.PostgresDB(a.holder.Sessions().Conn()).Ping()")
	assert.Contains(t, importsStorageRead(t, dir, "cmd/api/unrelated.go"), "return other{}.Conn() }")
	assert.NotContains(t, importsStorageRead(t, dir, "cmd/api/unrelated.go"), "fibermigrate")
	assert.Empty(t, importsStorageNotes(dir))
}

func Test_MigrateStorageVersions_ConnPoolSettingsAreReported(t *testing.T) {
	importsStorageNoProxy(t)

	dir := importsStorageTree(t, map[string]string{
		"go.mod": importsPostgresGoMod,
		"main.go": `package main

import "github.com/gofiber/storage/postgres"

func main() {
	store := postgres.New(postgres.Config{})
	store.Conn().SetMaxOpenConns(10)
	store.Conn().SetConnMaxLifetime(0)
	defer store.Conn().Close()
}
`,
	})

	importsStorageRun(t, dir)

	notes := importsStorageNotes(dir)
	if assert.Len(t, notes, 3, "%v", notes) {
		assert.Contains(t, notes[0], "main.go:7: SetMaxOpenConns of the storage's *sql.DB limits the database/sql view of its pool")
		assert.Contains(t, notes[0], "pool_max_conns")
		assert.Contains(t, notes[1], "main.go:8: SetConnMaxLifetime of the storage's *sql.DB")
		assert.Contains(t, notes[2], "main.go:9: Close of the storage's *sql.DB closes the database/sql view of its pool")
	}
	assert.Contains(t, importsStorageRead(t, dir, "main.go"), "fibermigrate.PostgresDB(store.Conn()).SetMaxOpenConns(10)")
}

func Test_MigrateStorageVersions_ConnWithoutAModuleIsReported(t *testing.T) {
	importsStorageNoProxy(t)

	dir := importsStorageTree(t, map[string]string{
		"main.go": `package main

import "github.com/gofiber/storage/postgres"

func main() { _ = postgres.New(postgres.Config{}).Conn() }
`,
	})

	importsStorageRun(t, dir)

	assert.Contains(t, importsStorageRead(t, dir, "main.go"), "postgres.New(postgres.Config{}).Conn()")
	assert.NotContains(t, importsStorageRead(t, dir, "main.go"), "fibermigrate")
	assert.Equal(t, []string{
		"main.go:5: `Conn()` returns a *pgxpool.Pool in github.com/gofiber/storage/postgres/v3, it returned a *sql.DB in the module without a major version: adapt the code that uses the connection.",
	}, importsStorageNotes(dir))
}

func Test_MigrateStorageVersions_ConnWhenTheNameOfTheShimIsTaken(t *testing.T) {
	importsStorageNoProxy(t)

	dir := importsStorageTree(t, map[string]string{
		"go.mod": importsPostgresGoMod,
		"main.go": `package main

import "github.com/gofiber/storage/postgres"

func main() {
	fibermigrate := 1
	_ = fibermigrate
	_ = postgres.New(postgres.Config{}).Conn()
}
`,
		"internal/fibermigrate/theirs.go": "package fibermigrate\n",
	})

	importsStorageRun(t, dir)

	// the package holds a file the migrations did not write: nothing is added
	assert.NoFileExists(t, dir+"/internal/fibermigrate/postgres.go")
	assert.Contains(t, importsStorageRead(t, dir, "main.go"), "postgres.New(postgres.Config{}).Conn()")
	assert.Len(t, importsStorageNotes(dir), 1)
}

func Test_PostgresShimFile(t *testing.T) {
	t.Parallel()

	f := pgShimModuleFile()
	assert.Equal(t, "postgres.go", f.name)
	assert.Contains(t, f.source, moduleShimMarker)
	assert.Contains(t, f.source, f.marker)
	assert.True(t, strings.HasPrefix(f.source, `// Code generated by "fiber migrate"; DO NOT EDIT.`))
	assert.NotContains(t, f.source, "go:build")
	assert.NotContains(t, f.source, "postgresshim")

	// the file is a package of the module and keeps its form when it is formatted
	parsed, err := parser.ParseFile(token.NewFileSet(), f.name, f.source, parser.ParseComments)
	require.NoError(t, err)
	assert.Equal(t, moduleShimPkg, parsed.Name.Name)
	assert.Nil(t, parsed.Doc, "the comments above the package clause are not its documentation")

	formatted, err := format.Source([]byte(f.source))
	require.NoError(t, err)
	assert.Equal(t, f.source, string(formatted))
}
