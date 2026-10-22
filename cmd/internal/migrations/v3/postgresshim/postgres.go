//go:build ignore

// Package postgresshim is the source of the file that the migration adds to
// <module>/internal/fibermigrate. It is not built with the CLI, which would have
// to depend on pgx for that: the test of the migration builds it in a module of
// its own.
package postgresshim

import (
	"database/sql"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

// pgDatabases holds the *sql.DB that PostgresDB made for each pool, which stay for
// as long as the program runs, like the pool of a storage.
var (
	pgMu        sync.RWMutex
	pgDatabases = map[*pgxpool.Pool]*sql.DB{}
)

// PostgresDB returns the database/sql view of a pgx pool: the *sql.DB that the
// storage in github.com/gofiber/storage/postgres returned from Conn() before its
// v3, which returns the pool. The view is made once for each pool, so that calling
// PostgresDB for every request does not leave a *sql.DB behind each time.
//
// The view takes its connections from the pool and keeps none idle. SetMaxOpenConns
// and the other settings of the *sql.DB limit the view only; size the pool in the
// connection URI of the storage (pool_max_conns) or give the storage a pool of your
// own with Config.DB. Closing the *sql.DB closes the view, not the pool: close the
// storage.
func PostgresDB(pool *pgxpool.Pool) *sql.DB {
	if pool == nil {
		return nil
	}

	pgMu.RLock()
	db := pgDatabases[pool]
	pgMu.RUnlock()
	if db != nil {
		return db
	}

	pgMu.Lock()
	defer pgMu.Unlock()
	if db = pgDatabases[pool]; db == nil {
		db = stdlib.OpenDBFromPool(pool)
		pgDatabases[pool] = db
	}
	return db
}
