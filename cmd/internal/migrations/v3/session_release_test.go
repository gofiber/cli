package v3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_MigrateSessionRelease(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "msessionrelease")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	content := `package main

import (
    "github.com/gofiber/fiber/v3"
    "github.com/gofiber/fiber/v3/middleware/session"
)

func handler(c fiber.Ctx) error {
    store := session.NewStore()
    sess, err := store.Get(c)
    if err != nil {
        return err
    }

    sess.Set("key", "value")
    return sess.Save()
}
`

	err = os.WriteFile(filepath.Join(dir, "main.go"), []byte(content), 0o600)
	require.NoError(t, err)

	cmd := &cobra.Command{}
	err = MigrateSessionRelease(cmd, dir, nil, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "main.go")) // #nosec G304
	require.NoError(t, err)

	result := string(data)
	assert.Contains(t, result, "defer sess.Release() // Important: Manual cleanup required")
}

func Test_MigrateSessionRelease_AlreadyHasDefer(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "msessionrelease")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	content := `package main

import (
    "github.com/gofiber/fiber/v3"
    "github.com/gofiber/fiber/v3/middleware/session"
)

func handler(c fiber.Ctx) error {
    store := session.NewStore()
    sess, err := store.Get(c)
    if err != nil {
        return err
    }
    defer sess.Release()

    sess.Set("key", "value")
    return sess.Save()
}
`

	err = os.WriteFile(filepath.Join(dir, "main.go"), []byte(content), 0o600)
	require.NoError(t, err)

	cmd := &cobra.Command{}
	err = MigrateSessionRelease(cmd, dir, nil, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "main.go")) // #nosec G304
	require.NoError(t, err)

	result := string(data)
	// Should not add another defer
	firstIdx := strings.Index(result, "defer sess.Release()")
	lastIdx := strings.LastIndex(result, "defer sess.Release()")
	assert.Equal(t, firstIdx, lastIdx, "Should only have one defer sess.Release()")
}

func Test_MigrateSessionRelease_GetByID(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "msessionrelease")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	content := `package main

import (
    "context"
    "github.com/gofiber/fiber/v3/middleware/session"
)

func backgroundTask(sessionID string) {
    store := session.NewStore()
    sess, err := store.GetByID(context.Background(), sessionID)
    if err != nil {
        return
    }

    sess.Set("last_task", "value")
    sess.Save()
}
`

	err = os.WriteFile(filepath.Join(dir, "main.go"), []byte(content), 0o600)
	require.NoError(t, err)

	cmd := &cobra.Command{}
	err = MigrateSessionRelease(cmd, dir, nil, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "main.go")) // #nosec G304
	require.NoError(t, err)

	result := string(data)
	assert.Contains(t, result, "defer sess.Release() // Important: Manual cleanup required")
}

func Test_MigrateSessionRelease_MultilineErrorCheck(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "msessionrelease")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	content := `package main

import (
    "github.com/gofiber/fiber/v3"
    "github.com/gofiber/fiber/v3/middleware/session"
)

func handler(c fiber.Ctx) error {
    store := session.NewStore()
    sess, err := store.Get(c)
    if err != nil {
        c.Status(500)
        return err
    }

    sess.Set("key", "value")
    return sess.Save()
}
`

	err = os.WriteFile(filepath.Join(dir, "main.go"), []byte(content), 0o600)
	require.NoError(t, err)

	cmd := &cobra.Command{}
	err = MigrateSessionRelease(cmd, dir, nil, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "main.go")) // #nosec G304
	require.NoError(t, err)

	result := string(data)
	assert.Contains(t, result, "defer sess.Release() // Important: Manual cleanup required")

	// Verify defer comes after the error block
	deferIdx := strings.Index(result, "defer sess.Release()")
	errorBlockEnd := strings.Index(result, "}")
	assert.Greater(t, deferIdx, errorBlockEnd, "defer should come after error block")
}

func Test_MigrateSessionRelease_MiddlewarePattern_NoRelease(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "msessionrelease")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	// Middleware pattern - should NOT get Release() call
	content := `package main

import (
    "github.com/gofiber/fiber/v3"
    "github.com/gofiber/fiber/v3/middleware/session"
)

func main() {
    app := fiber.New()

    store := session.NewStore()
    sessionMiddleware := session.NewMiddleware(store)

    app.Use(sessionMiddleware)

    app.Get("/", func(c fiber.Ctx) error {
        sess := session.FromContext(c)
        sess.Set("key", "value")
        return sess.Save()
    })

    app.Listen(":3000")
}
`

	err = os.WriteFile(filepath.Join(dir, "main.go"), []byte(content), 0o600)
	require.NoError(t, err)

	cmd := &cobra.Command{}
	err = MigrateSessionRelease(cmd, dir, nil, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "main.go")) // #nosec G304
	require.NoError(t, err)

	result := string(data)
	// Should NOT add defer sess.Release() for middleware pattern
	assert.NotContains(t, result, "defer sess.Release()")
}

func Test_MigrateSessionRelease_OtherGetMethods_NoRelease(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "msessionrelease")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	// Various Get/GetByID methods that are NOT session stores
	content := `package main

import (
    "github.com/gofiber/fiber/v3"
)

func handler(c fiber.Ctx) error {
    // CSRF session - should NOT get Release()
    session := c.Locals("session")
    if session != nil {
        // use session
    }

    // Ent GetX - should NOT get Release()
    obj, err := client.Book.GetX(ctx, id)
    if err != nil {
        return err
    }

    // Generic Get - should NOT get Release()
    data, err := cache.Get(key)
    if err != nil {
        return err
    }

    return c.JSON(fiber.Map{
        "obj": obj,
        "data": data,
    })
}
`

	err = os.WriteFile(filepath.Join(dir, "main.go"), []byte(content), 0o600)
	require.NoError(t, err)

	cmd := &cobra.Command{}
	err = MigrateSessionRelease(cmd, dir, nil, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "main.go")) // #nosec G304
	require.NoError(t, err)

	result := string(data)
	// Should NOT add defer Release() for non-session Get methods
	assert.NotContains(t, result, "defer obj.Release()")
	assert.NotContains(t, result, "defer data.Release()")
	assert.NotContains(t, result, "defer session.Release()")
}

func Test_MigrateSessionRelease_SessionStoreVariableName(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "msessionrelease")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	// Test various store variable naming patterns
	content := `package main

import (
    "github.com/gofiber/fiber/v3"
    "github.com/gofiber/fiber/v3/middleware/session"
)

func handler(c fiber.Ctx) error {
    // Common store variable names
    store := session.NewStore()
    sess, err := store.Get(c)
    if err != nil {
        return err
    }

    sessionStore := session.NewStore()
    sess2, err2 := sessionStore.Get(c)
    if err2 != nil {
        return err2
    }

    myStore := session.NewStore()
    sess3, err3 := myStore.Get(c)
    if err3 != nil {
        return err3
    }

    return c.SendStatus(200)
}
`

	err = os.WriteFile(filepath.Join(dir, "main.go"), []byte(content), 0o600)
	require.NoError(t, err)

	cmd := &cobra.Command{}
	err = MigrateSessionRelease(cmd, dir, nil, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "main.go")) // #nosec G304
	require.NoError(t, err)

	result := string(data)
	// Should add Release() for all store variable patterns
	assert.Equal(t, 3, strings.Count(result, "defer sess"), "Should add defer for all 3 store.Get() calls")
	assert.Contains(t, result, "defer sess.Release()")
	assert.Contains(t, result, "defer sess2.Release()")
	assert.Contains(t, result, "defer sess3.Release()")
}

func Test_MigrateSessionRelease_V2Import_NoRelease(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "msessionrelease")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	// v2 session import - should NOT get Release() since migration only processes v3 imports
	// This migration runs AFTER MigrateContribPackages which changes v2→v3 imports
	// So if we encounter v2 imports, we skip them (they haven't been migrated yet)
	content := `package main

import (
    "github.com/gofiber/fiber/v2"
    "github.com/gofiber/fiber/v2/middleware/session"
)

func handler(c *fiber.Ctx) error {
    store := session.NewStore()
    sess, err := store.Get(c)
    if err != nil {
        return err
    }

    sess.Set("key", "value")
    return sess.Save()
}
`

	err = os.WriteFile(filepath.Join(dir, "main.go"), []byte(content), 0o600)
	require.NoError(t, err)

	cmd := &cobra.Command{}
	err = MigrateSessionRelease(cmd, dir, nil, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "main.go")) // #nosec G304
	require.NoError(t, err)

	result := string(data)
	// Should NOT add Release() for v2 imports (migration only processes v3)
	assert.NotContains(t, result, "defer sess.Release()")
	assert.Equal(t, content, result, "File should remain unchanged for v2 imports")
}

// Test_MigrateSessionRelease_CSRFWithSession tests real-world code from gofiber/recipes
// https://github.com/gofiber/recipes/blob/master/csrf-with-session/main.go
func Test_MigrateSessionRelease_CSRFWithSession(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "msessionrelease")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	// Simplified version of csrf-with-session from recipes
	// This has store.Get() which SHOULD get Release() added
	content := `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/csrf"
	"github.com/gofiber/fiber/v3/middleware/session"
)

func main() {
	app := fiber.New()

	store := session.NewStore()

	app.Post("/login", func(c fiber.Ctx) error {
		session, err := store.Get(c)
		if err != nil {
			return c.SendStatus(fiber.StatusInternalServerError)
		}
		if err := session.Reset(); err != nil {
			return c.SendStatus(fiber.StatusInternalServerError)
		}
		session.Set("loggedIn", true)
		if err := session.Save(); err != nil {
			return c.SendStatus(fiber.StatusInternalServerError)
		}
		return c.Redirect("/protected")
	})

	app.Get("/logout", func(c fiber.Ctx) error {
		session, err := store.Get(c)
		if err != nil {
			return c.SendStatus(fiber.StatusInternalServerError)
		}
		if err := session.Destroy(); err != nil {
			return c.SendStatus(fiber.StatusInternalServerError)
		}
		return c.Redirect("/")
	})

	app.Listen(":3000")
}
`

	err = os.WriteFile(filepath.Join(dir, "main.go"), []byte(content), 0o600)
	require.NoError(t, err)

	cmd := &cobra.Command{}
	err = MigrateSessionRelease(cmd, dir, nil, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "main.go")) // #nosec G304
	require.NoError(t, err)

	result := string(data)

	// Should add Release() for both store.Get() calls
	assert.Equal(t, 2, strings.Count(result, "defer session.Release()"), "Should add defer for both store.Get() calls")

	// Verify the Release() calls are placed correctly
	assert.Contains(t, result, "defer session.Release() // Important: Manual cleanup required")
}

// Test_MigrateSessionRelease_EntMySQL tests real-world code from gofiber/recipes
// https://github.com/gofiber/recipes/blob/master/ent-mysql/ent/client.go
func Test_MigrateSessionRelease_EntMySQL(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "msessionrelease")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	// Simplified version of ent-mysql client.go from recipes
	// This has NO session imports, so should NOT get any Release() calls
	content := `// Code generated by ent, DO NOT EDIT.

package ent

import (
	"context"
	"errors"
	"fmt"
	"log"

	"ent-mysql/ent/migrate"
	"ent-mysql/ent/book"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
)

// Client is the client that holds all ent builders.
type Client struct {
	config
	Schema *migrate.Schema
	Book   *BookClient
}

// NewClient creates a new client configured with the given options.
func NewClient(opts ...Option) *Client {
	cfg := config{log: log.Println, hooks: &hooks{}, inters: &inters{}}
	cfg.options(opts...)
	client := &Client{config: cfg}
	client.init()
	return client
}

func (c *Client) init() {
	c.Schema = migrate.NewSchema(c.driver)
	c.Book = NewBookClient(c.config)
}

// BookClient is a client for the Book schema.
type BookClient struct {
	config
}

// NewBookClient returns a client for the Book from the given config.
func NewBookClient(c config) *BookClient {
	return &BookClient{config: c}
}

// Get returns a Book entity by its id.
func (c *BookClient) Get(ctx context.Context, id int) (*Book, error) {
	return c.Query().Where(book.ID(id)).Only(ctx)
}

// GetX is like Get, but panics if an error occurs.
func (c *BookClient) GetX(ctx context.Context, id int) *Book {
	obj, err := c.Get(ctx, id)
	if err != nil {
		panic(err)
	}
	return obj
}
`

	err = os.WriteFile(filepath.Join(dir, "client.go"), []byte(content), 0o600)
	require.NoError(t, err)

	cmd := &cobra.Command{}
	err = MigrateSessionRelease(cmd, dir, nil, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "client.go")) // #nosec G304
	require.NoError(t, err)

	result := string(data)

	// Should NOT add any Release() calls since there's no session import
	assert.NotContains(t, result, "defer")
	assert.NotContains(t, result, "Release()")
	assert.Equal(t, content, result, "File should remain unchanged")
}

// Test_MigrateSessionRelease_NoErrorCheck tests when error is ignored or not checked.
// Note: sess.Release() has a nil check, so it's safe to defer even if store.Get() returns nil.
func Test_MigrateSessionRelease_NoErrorCheck(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "msessionrelease")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	content := `package main

import (
    "github.com/gofiber/fiber/v3"
    "github.com/gofiber/fiber/v3/middleware/session"
)

func handler1(c fiber.Ctx) error {
    store := session.NewStore()
    sess, _ := store.Get(c)

    sess.Set("key", "value")
    return sess.Save()
}

func handler2(c fiber.Ctx) error {
    store := session.NewStore()
    sess, err := store.Get(c)
    // No error check!

    sess.Set("key", "value")
    return sess.Save()
}
`

	err = os.WriteFile(filepath.Join(dir, "main.go"), []byte(content), 0o600)
	require.NoError(t, err)

	cmd := &cobra.Command{}
	err = MigrateSessionRelease(cmd, dir, nil, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "main.go")) // #nosec G304
	require.NoError(t, err)

	result := string(data)

	// Should add Release() even without error checking
	// This is safe because sess.Release() has a nil check and returns early if nil
	assert.Equal(t, 2, strings.Count(result, "defer sess.Release()"), "Should add defer for both store.Get() calls even without error checks")
	assert.Contains(t, result, "defer sess.Release() // Important: Manual cleanup required")
}

// Test_MigrateSessionRelease_OtherPackagesWithNew tests that packages with
// New, NewStore, Get, GetByID methods don't trigger false positives
func Test_MigrateSessionRelease_OtherPackagesWithNew(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "msessionrelease")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	// Various packages with similar method names but NOT session
	content := `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/some/cache"
	"github.com/other/database"
)

func handler(c fiber.Ctx) error {
	// Cache with NewStore and Get - should NOT add Release
	cacheStore := cache.NewStore()
	data, err := cacheStore.Get("key")
	if err != nil {
		return err
	}

	// Database with New and GetByID - should NOT add Release
	db := database.New()
	record, err := db.GetByID(context.Background(), "123")
	if err != nil {
		return err
	}

	// Generic object with Get - should NOT add Release
	obj := myObject.New()
	value, err := obj.Get(c)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"data": data,
		"record": record,
		"value": value,
	})
}
`

	err = os.WriteFile(filepath.Join(dir, "main.go"), []byte(content), 0o600)
	require.NoError(t, err)

	cmd := &cobra.Command{}
	err = MigrateSessionRelease(cmd, dir, nil, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "main.go")) // #nosec G304
	require.NoError(t, err)

	result := string(data)

	// Should NOT add any Release() calls
	assert.NotContains(t, result, "defer data.Release()")
	assert.NotContains(t, result, "defer record.Release()")
	assert.NotContains(t, result, "defer value.Release()")
	assert.Equal(t, content, result, "File should remain unchanged")
}

// Test_MigrateSessionRelease_SameVarNameDifferentFunctions tests the critical edge case
// where the same variable name "store" is used in different functions for different types
func Test_MigrateSessionRelease_SameVarNameDifferentFunctions(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "msessionrelease")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	// CRITICAL: "store" variable reused in different contexts
	content := `package main

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/session"
	"github.com/some/cache"
)

func sessionHandler(c fiber.Ctx) error {
	// This is a SESSION store - SHOULD add Release
	store := session.NewStore()
	sess, err := store.Get(c)
	if err != nil {
		return err
	}

	sess.Set("key", "value")
	return sess.Save()
}

func cacheHandler(c fiber.Ctx) error {
	// This is a CACHE store - should NOT add Release
	store := cache.NewStore()
	data, err := store.Get("key")
	if err != nil {
		return err
	}

	return c.SendString(data)
}
`

	err = os.WriteFile(filepath.Join(dir, "main.go"), []byte(content), 0o600)
	require.NoError(t, err)

	cmd := &cobra.Command{}
	err = MigrateSessionRelease(cmd, dir, nil, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "main.go")) // #nosec G304
	require.NoError(t, err)

	result := string(data)

	// CRITICAL: Should only add Release() in sessionHandler, NOT in cacheHandler
	assert.Equal(t, 1, strings.Count(result, "defer sess.Release()"), "Should only add defer for session store, not cache store")
	assert.NotContains(t, result, "defer data.Release()", "Should NOT add Release() for cache store")

	// Verify it was added in the right function
	lines := strings.Split(result, "\n")
	inSessionHandler := false
	for _, line := range lines {
		if strings.Contains(line, "func sessionHandler") {
			inSessionHandler = true
		} else if strings.Contains(line, "func cacheHandler") {
			inSessionHandler = false
		}

		if strings.Contains(line, "defer sess.Release()") {
			assert.True(t, inSessionHandler, "defer sess.Release() should only be in sessionHandler")
		}
		if strings.Contains(line, "defer data.Release()") {
			t.Error("Should NOT add defer data.Release() for cache store")
		}
	}
}
