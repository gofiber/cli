package v3

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// pgShimTest is a test of the file in the module package, which runs in a module of
// its own: the CLI does not depend on pgx, so the source is not built with it.
const pgShimTest = `package fibermigrate

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// newPool makes a pool that has not connected to anything.
func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	pool, err := pgxpool.New(context.Background(), "postgres://user:secret@127.0.0.1:1/db?connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestPostgresDBIsMadeOnceForEachPool(t *testing.T) {
	first, second := newPool(t), newPool(t)

	db := PostgresDB(first)
	if db == nil {
		t.Fatal("no database for a pool")
	}
	if PostgresDB(first) != db {
		t.Fatal("a second call made another database")
	}
	if PostgresDB(second) == db {
		t.Fatal("two pools share a database")
	}
	if PostgresDB(nil) != nil {
		t.Fatal("a database for no pool")
	}
	if db.Stats().MaxOpenConnections != 0 {
		t.Fatal("the view limits its connections by itself")
	}
}

func TestPostgresDBIsSafeForConcurrentCalls(t *testing.T) {
	pool := newPool(t)

	got := make(chan any, 64)
	var wg sync.WaitGroup
	for range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got <- PostgresDB(pool)
		}()
	}
	wg.Wait()
	close(got)

	var first any
	for db := range got {
		if first == nil {
			first = db
		}
		if db != first {
			t.Fatal("concurrent calls made several databases")
		}
	}
}

func TestPostgresDBViewDoesNotKeepIdleConnections(t *testing.T) {
	db := PostgresDB(newPool(t))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// nothing listens on the port, so the connection fails; the view stays usable
	if err := db.PingContext(ctx); err == nil {
		t.Fatal("a ping reached a server")
	}
	if idle := db.Stats().Idle; idle != 0 {
		t.Fatalf("the view keeps %d idle connections", idle)
	}
}
`

// pgShimVersions are the versions of pgx the file is built with: the one that the
// first release of the v3 line of the storage asks for, and a recent one.
var pgShimVersions = []string{"v5.7.5", "v5.11.0"}

// pgShimUnresolved are the texts of the output of go that say the module could not
// be fetched, which skips the test where there is no network and no module cache.
var pgShimUnresolved = []string{
	"dial tcp", "no such host", "connection refused", "i/o timeout", "TLS handshake", "no required module provides",
	"cannot find module providing", "module lookup disabled", "reading https://", "unrecognized import path",
	"verifying module", "GOPROXY list is not the empty string", "proxyconnect", "403 Forbidden", "502 Bad Gateway",
}

func Test_PostgresShimBuildsAgainstPgx(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("builds pgx, which takes a while")
	}
	for _, version := range pgShimVersions {
		t.Run(version, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			pkg := filepath.Join(dir, "internal", "fibermigrate")
			require.NoError(t, os.MkdirAll(pkg, 0o750))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"),
				[]byte("module example.com/shimcheck\n\ngo 1.25\n\nrequire github.com/jackc/pgx/v5 "+version+"\n"), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(pkg, pgShimFile), []byte(pgShimModuleFile().source), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(pkg, "postgres_test.go"), []byte(pgShimTest), 0o600))

			shimBuild(t, dir, "pgx "+version)
		})
	}
}

// shimBuild runs the tests of the module in dir with the go tool, vet included. A
// module that cannot be fetched here (no network, no module cache) skips the test.
func shimBuild(t *testing.T, dir, what string) {
	t.Helper()

	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go tool")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, goTool, "test", "-count=1", "-vet=all", "./...") // #nosec G204 -- the go tool, with arguments of this test
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = &out, &out
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off")

	if runErr := cmd.Run(); runErr != nil {
		for _, text := range pgShimUnresolved {
			if strings.Contains(out.String(), text) {
				t.Skipf("%s cannot be fetched here: %s", what, firstLine(out.String()))
			}
		}
		t.Fatalf("the file does not build or its test fails with %s: %v\n%s", what, runErr, out.String())
	}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}
