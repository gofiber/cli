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

// ReqHeaderParser read the reqHeader tag and the binder of v3 reads header, so a struct
// whose tags kept the old key bound nothing: the values came out empty and nothing failed.
func Test_MigrateParamsTagKeys_ReqHeader(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, "package main\n\n"+
		"type headers struct {\n"+
		"\tToken string `reqHeader:\"X-Token\" json:\"token\"`\n"+
		"\tAgent string `json:\"agent\" reqHeader:\"User-Agent\"`\n"+
		"\tBoth  string `params:\"id\" reqHeader:\"X-Id\"`\n"+
		"\tKept  string `reqHeader:\"A\" header:\"B\"`\n"+
		"\tOther string `myreqHeader:\"x\" xparams:\"y\"`\n"+
		"}\n")

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateParamsTagKeys(newCmd(&buf), dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "`header:\"X-Token\" json:\"token\"`")
	assert.Contains(t, content, "`json:\"agent\" header:\"User-Agent\"`")
	assert.Contains(t, content, "`uri:\"id\" header:\"X-Id\"`")
	assert.Contains(t, content, "`reqHeader:\"A\" header:\"B\"`", "a field that has a header key already keeps its tag")
	assert.Contains(t, content, "`myreqHeader:\"x\" xparams:\"y\"`", "only whole keys are changed")
	assert.Contains(t, buf.String(), "Migrating params tag keys")

	// a second run has nothing left to change
	var second bytes.Buffer
	require.NoError(t, v3.MigrateParamsTagKeys(newCmd(&second), dir, nil, nil))
	assert.Equal(t, content, readFile(t, file))
	assert.Empty(t, second.String())
}

func Test_MigrateParamsTagKeys_WithReqHeaderParser(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, "package main\n\n"+
		"import \"github.com/gofiber/fiber/v2\"\n\n"+
		"type auth struct {\n\tToken string `reqHeader:\"X-Token\"`\n}\n\n"+
		"func handler(c *fiber.Ctx) error {\n\tvar a auth\n\tif err := c.ReqHeaderParser(&a); err != nil {\n\t\treturn err\n\t}\n\treturn c.SendString(a.Token)\n}\n")

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateParamsTagKeys(newCmd(&buf), dir, nil, nil))
	require.NoError(t, v3.MigrateReqHeaderParser(newCmd(&buf), dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "Token string `header:\"X-Token\"`")
	assert.Contains(t, content, "c.Bind().Header(&a)")
	assert.NotContains(t, content, "reqHeader")
}
