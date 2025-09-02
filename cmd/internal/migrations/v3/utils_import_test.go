package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateUtilsImport(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mutils")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v3/utils"
var (
    _ = utils.TrimBytes([]byte("x"), " ")
    _ = utils.TrimRightBytes([]byte("x"), " ")
    _ = utils.TrimLeftBytes([]byte("x"), " ")
    _ = utils.EqualFoldBytes([]byte("A"), []byte("a"))
    _ = utils.Trim(" x ", " ")
    _ = utils.TrimRight("x ", " ")
    _ = utils.TrimLeft(" x", " ")
    _ = utils.EqualFold("A", "a")
    _ = utils.GetString([]byte("a"))
    _ = utils.GetBytes("a")
    _ = utils.ImmutableString([]byte("b"))
)`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateUtilsImport(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "github.com/gofiber/utils/v2\"")
	assert.NotContains(t, content, "fiber/v3/utils")
	assert.NotContains(t, content, "TrimBytes(")
	assert.NotContains(t, content, "TrimRightBytes(")
	assert.NotContains(t, content, "TrimLeftBytes(")
	assert.NotContains(t, content, "EqualFoldBytes(")
	assert.NotContains(t, content, "GetString(")
	assert.NotContains(t, content, "GetBytes(")
	assert.NotContains(t, content, "ImmutableString(")
	assert.Contains(t, content, "utils.Trim(")
	assert.Contains(t, content, "utils.TrimRight(")
	assert.Contains(t, content, "utils.TrimLeft(")
	assert.Contains(t, content, "utils.EqualFold(")
	assert.Contains(t, content, "strings.Trim(")
	assert.Contains(t, content, "strings.TrimRight(")
	assert.Contains(t, content, "strings.TrimLeft(")
	assert.Contains(t, content, "strings.EqualFold(")
	assert.Contains(t, content, "utils.ToString(")
	assert.Contains(t, content, "utils.CopyBytes(")
	assert.Contains(t, content, "string([]byte(\"b\"))")
	assert.Contains(t, buf.String(), "Migrating utils imports")
}

func Test_MigrateUtilsImport_RemovesUnusedUtilsImport(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mutils")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "testing"
    "github.com/gofiber/fiber/v3/utils"
)

func foo(t *testing.T) {
    utils.AssertEqual(t, 1, 1)
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateUtilsImport(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "github.com/gofiber/utils/v2\"")
	assert.Contains(t, content, "github.com/stretchr/testify/assert")
	assert.Contains(t, content, "assert.Equal(")
	assert.NotContains(t, content, "utils.")
}
