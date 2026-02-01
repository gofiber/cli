package v3

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_MigrateStorageVersions(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mstorageversions")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	content := `package main

import (
    "github.com/gofiber/fiber/v3"
    "github.com/gofiber/storage/sqlite3"
    "github.com/gofiber/storage/redis"
    "github.com/gofiber/storage/postgres"
)

func main() {
    store := sqlite3.New()
    cache := redis.New()
    pg := postgres.New()
}
`

	err = os.WriteFile(filepath.Join(dir, "main.go"), []byte(content), 0o600)
	require.NoError(t, err)

	cmd := &cobra.Command{}
	err = MigrateStorageVersions(cmd, dir, nil, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "main.go")) // #nosec G304
	require.NoError(t, err)

	result := string(data)
	assert.Contains(t, result, `"github.com/gofiber/storage/sqlite3/v2"`)
	assert.Contains(t, result, `"github.com/gofiber/storage/redis/v3"`)
	assert.Contains(t, result, `"github.com/gofiber/storage/postgres/v3"`)
	assert.NotContains(t, result, `"github.com/gofiber/storage/sqlite3"`)
	assert.NotContains(t, result, `"github.com/gofiber/storage/redis"`)
	assert.NotContains(t, result, `"github.com/gofiber/storage/postgres"`)
}

func Test_MigrateStorageVersions_AlreadyV2(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mstorageversions")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	content := `package main

import (
    "github.com/gofiber/fiber/v3"
    "github.com/gofiber/storage/sqlite3/v2"
    "github.com/gofiber/storage/redis/v3"
)

func main() {
    store := sqlite3.New()
    cache := redis.New()
}
`

	err = os.WriteFile(filepath.Join(dir, "main.go"), []byte(content), 0o600)
	require.NoError(t, err)

	cmd := &cobra.Command{}
	err = MigrateStorageVersions(cmd, dir, nil, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "main.go")) // #nosec G304
	require.NoError(t, err)

	result := string(data)
	// Should remain unchanged
	assert.Contains(t, result, `"github.com/gofiber/storage/sqlite3/v2"`)
	assert.Contains(t, result, `"github.com/gofiber/storage/redis/v3"`)
	// Should not have duplicate version suffixes
	assert.NotContains(t, result, `"github.com/gofiber/storage/sqlite3/v2/v2"`)
	assert.NotContains(t, result, `"github.com/gofiber/storage/redis/v3/v3"`)
}

func Test_MigrateStorageVersions_MixedVersions(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mstorageversions")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	content := `package main

import (
    "github.com/gofiber/storage/sqlite3"
    "github.com/gofiber/storage/redis/v3"
    "github.com/gofiber/storage/postgres"
)

func main() {
    store := sqlite3.New()
    cache := redis.New()
    pg := postgres.New()
}
`

	err = os.WriteFile(filepath.Join(dir, "main.go"), []byte(content), 0o600)
	require.NoError(t, err)

	cmd := &cobra.Command{}
	err = MigrateStorageVersions(cmd, dir, nil, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "main.go")) // #nosec G304
	require.NoError(t, err)

	result := string(data)
	// Only the non-versioned imports should be updated
	assert.Contains(t, result, `"github.com/gofiber/storage/sqlite3/v2"`)
	assert.Contains(t, result, `"github.com/gofiber/storage/postgres/v3"`)
	// Already versioned should remain unchanged
	assert.Contains(t, result, `"github.com/gofiber/storage/redis/v3"`)
	// Should not have old versions
	assert.NotContains(t, result, `"github.com/gofiber/storage/sqlite3"`)
	assert.NotContains(t, result, `"github.com/gofiber/storage/postgres"`)
}

func Test_MigrateStorageVersions_V2ToV3(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mstorageversions")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	content := `package main

import (
    "github.com/gofiber/storage/redis/v2"
    "github.com/gofiber/storage/postgres/v2"
    "github.com/gofiber/storage/sqlite3/v2"
)

func main() {
    cache := redis.New()
    pg := postgres.New()
    store := sqlite3.New()
}
`

	err = os.WriteFile(filepath.Join(dir, "main.go"), []byte(content), 0o600)
	require.NoError(t, err)

	cmd := &cobra.Command{}
	err = MigrateStorageVersions(cmd, dir, nil, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "main.go")) // #nosec G304
	require.NoError(t, err)

	result := string(data)
	// redis and postgres should upgrade from v2 to v3
	assert.Contains(t, result, `"github.com/gofiber/storage/redis/v3"`)
	assert.Contains(t, result, `"github.com/gofiber/storage/postgres/v3"`)
	// sqlite3 stays at v2 (no v3 available)
	assert.Contains(t, result, `"github.com/gofiber/storage/sqlite3/v2"`)
	// Should not have old v2 versions for redis/postgres
	assert.NotContains(t, result, `"github.com/gofiber/storage/redis/v2"`)
	assert.NotContains(t, result, `"github.com/gofiber/storage/postgres/v2"`)
}

func Test_MigrateStorageVersions_UnknownPackages(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mstorageversions")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	content := `package main

import (
    "github.com/gofiber/storage/aerospike"
    "github.com/gofiber/storage/nats"
    "github.com/gofiber/storage/sqlite3"
)

func main() {
    aero := aerospike.New()
    n := nats.New()
    store := sqlite3.New()
}
`

	err = os.WriteFile(filepath.Join(dir, "main.go"), []byte(content), 0o600)
	require.NoError(t, err)

	cmd := &cobra.Command{}
	err = MigrateStorageVersions(cmd, dir, nil, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "main.go")) // #nosec G304
	require.NoError(t, err)

	result := string(data)
	// Known package should be updated
	assert.Contains(t, result, `"github.com/gofiber/storage/sqlite3/v2"`)
	// Unknown packages should remain unchanged (they're v1/unversioned)
	assert.Contains(t, result, `"github.com/gofiber/storage/aerospike"`)
	assert.Contains(t, result, `"github.com/gofiber/storage/nats"`)
}
