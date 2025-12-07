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

func Test_MigrateSessionRelease_IgnoresNonSessionStores(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "msessionrelease")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	content := `package main

import (
    "github.com/other/module/session"
)

func handler() {
    store := session.New()
    obj, err := store.Get()
    if err != nil {
        panic(err)
    }

    _ = obj.Release
}`

	err = os.WriteFile(filepath.Join(dir, "main.go"), []byte(content), 0o600)
	require.NoError(t, err)

	cmd := &cobra.Command{}
	err = MigrateSessionRelease(cmd, dir, nil, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "main.go")) // #nosec G304
	require.NoError(t, err)

	result := string(data)
	assert.NotContains(t, result, "defer obj.Release()")
}

func Test_MigrateSessionRelease_HandlesAliasedImports(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "msessionrelease")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	content := `package main

import (
    "github.com/gofiber/fiber/v3"
    alias "github.com/gofiber/fiber/v3/middleware/session"
)

func handler(c fiber.Ctx) error {
    store := alias.NewStore()
    session, err := store.Get(c)
    if err != nil {
        return err
    }

    session.Set("key", "value")
    return session.Save()
}`

	err = os.WriteFile(filepath.Join(dir, "main.go"), []byte(content), 0o600)
	require.NoError(t, err)

	cmd := &cobra.Command{}
	err = MigrateSessionRelease(cmd, dir, nil, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "main.go")) // #nosec G304
	require.NoError(t, err)

	result := string(data)
	assert.Contains(t, result, "defer session.Release() // Important: Manual cleanup required")
}
