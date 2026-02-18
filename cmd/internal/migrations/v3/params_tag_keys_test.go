package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateParamsTagKeys_RewriteWithParserCalls(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mptagparser")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2"

type params struct {
    ID string `+"`params:\"id\" json:\"id\"`"+`
}

func handler(c fiber.Ctx) error {
    var p params
    if err := c.ParamsParser(&p); err != nil {
        return err
    }
    return nil
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateParamsTagKeys(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "c.ParamsParser(&p)")
	assert.Contains(t, content, "`uri:\"id\" json:\"id\"`")
	assert.NotContains(t, content, "`params:\"id\" json:\"id\"`")
	assert.Contains(t, buf.String(), "Migrating params tag keys")
}

func Test_MigrateParamsTagKeys_RewriteForNonFiber(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mptagnonfiber")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
type params struct {
    ID string `+"`params:\"id\"`"+`
}

type ctx struct{}
func (ctx) ParamsParser(any) {}
func handler(c ctx) {
    var p params
    c.ParamsParser(&p)
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateParamsTagKeys(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "c.ParamsParser(&p)")
	assert.NotContains(t, content, "`params:\"id\"`")
	assert.Contains(t, content, "`uri:\"id\"`")
	assert.Contains(t, buf.String(), "Migrating params tag keys")
}

func Test_MigrateParamsTagKeys_RewriteWithoutParserCalls(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mptagonly")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
type params struct {
    ID string `+"`params:\"id\" json:\"id\"`"+`
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateParamsTagKeys(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "`params:\"id\" json:\"id\"`")
	assert.Contains(t, content, "`uri:\"id\" json:\"id\"`")
	assert.Contains(t, buf.String(), "Migrating params tag keys")
}

func Test_MigrateParamsTagKeys_NoChanges(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mptagnochange")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
type params struct {
    ID string `+"`uri:\"id\" json:\"id\"`"+`
}`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateParamsTagKeys(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "`uri:\"id\" json:\"id\"`")
	assert.NotContains(t, buf.String(), "Migrating params tag keys")
}
