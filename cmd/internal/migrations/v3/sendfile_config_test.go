package v3_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateSendFileConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	if err := c.SendFile("./public/index.html", true); err != nil {
		return err
	}
	if err := c.SendFile("./public/off.html", false); err != nil {
		return err
	}
	return c.SendFile("./public/file.txt", compressEnabled)
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateSendFileConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `c.SendFile("./public/index.html", fiber.SendFile{Compress: true, ByteRange: true})`)
	assert.Contains(t, content, `c.SendFile("./public/off.html", fiber.SendFile{ByteRange: true})`)
	assert.Contains(t, content, `c.SendFile("./public/file.txt", fiber.SendFile{Compress: compressEnabled, ByteRange: true})`)
	assert.Contains(t, buf.String(), "Migrating SendFile compress flag")
}

func Test_MigrateSendFileConfig_KeepsByteRanges(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	return c.SendFile("./public/movie.mp4")
}

func notFound(c *fiber.Ctx) error {
	return c.Status(fiber.StatusNotFound).SendFile("./public/404.html")
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateSendFileConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `c.SendFile("./public/movie.mp4", fiber.SendFile{ByteRange: true})`)
	assert.Contains(t, content, `c.Status(fiber.StatusNotFound).SendFile("./public/404.html", fiber.SendFile{ByteRange: true})`)
}

func Test_MigrateSendFileConfig_MultiLineAndAlias(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import f "github.com/gofiber/fiber/v2"

func handler(c *f.Ctx) error {
	if err := c.SendFile(
		"./public/index.html",
		true,
	); err != nil {
		return err
	}
	return c.SendFile(
		"./public/other.html",
	)
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateSendFileConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "f.SendFile{Compress: true, ByteRange: true},")
	assert.Contains(t, content, `"./public/other.html", f.SendFile{ByteRange: true}`)
	assert.NotContains(t, content, "fiber.SendFile")
}

func Test_MigrateSendFileConfig_Closure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import "github.com/gofiber/fiber/v2"

func main() {
	app := fiber.New()
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendFile("./public/index.html", true)
	})
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateSendFileConfig(cmd, dir, nil, nil))

	assert.Contains(t, readFile(t, file), `c.SendFile("./public/index.html", fiber.SendFile{Compress: true, ByteRange: true})`)
}

func Test_MigrateSendFileConfig_IgnoresOtherReceivers(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	input := `package main

import (
	"example.com/mailer"

	"github.com/gofiber/fiber/v2"
)

type uploader struct{}

func (u uploader) SendFile(name string, retry bool) error { return nil }

func handler(c *fiber.Ctx) error {
	m := mailer.New()
	_ = m.SendFile("report.pdf", true)

	u := uploader{}
	_ = u.SendFile("report.pdf", true)

	inner := func(c *mailer.Conn) error {
		return c.SendFile("report.pdf", true)
	}
	_ = inner

	return c.SendFile("./public/index.html", true)
}
`
	file := writeTempFile(t, dir, input)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateSendFileConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, `m.SendFile("report.pdf", true)`)
	assert.Contains(t, content, `u.SendFile("report.pdf", true)`)
	assert.Contains(t, content, `return c.SendFile("report.pdf", true)`)
	assert.Contains(t, content, `c.SendFile("./public/index.html", fiber.SendFile{Compress: true, ByteRange: true})`)

	notes := internal.DrainNotes(dir)
	require.NotEmpty(t, notes)
	for _, n := range notes {
		assert.Contains(t, n.Message, "SendFile")
	}
}

func Test_MigrateSendFileConfig_NoFiberImport(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	input := `package main

func run(c Sender) error {
	return c.SendFile("report.pdf", true)
}
`
	file := writeTempFile(t, dir, input)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateSendFileConfig(cmd, dir, nil, nil))

	assert.Equal(t, input, readFile(t, file))
	assert.Empty(t, buf.String())
}

func Test_MigrateSendFileConfig_Spread(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	input := `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	flags := []bool{true}
	return c.SendFile("./public/index.html", flags...)
}
`
	file := writeTempFile(t, dir, input)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateSendFileConfig(cmd, dir, nil, nil))

	assert.Equal(t, input, readFile(t, file))
	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Equal(t, 7, notes[0].Line)
	assert.Equal(t, "main.go", notes[0].File)
}

func Test_MigrateSendFileConfig_Idempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	if err := c.SendFile("./public/a.html", true); err != nil {
		return err
	}
	return c.SendFile("./public/b.html")
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateSendFileConfig(cmd, dir, nil, nil))

	migrated := readFile(t, file)
	assert.Contains(t, migrated, `c.SendFile("./public/a.html", fiber.SendFile{Compress: true, ByteRange: true})`)
	assert.Contains(t, buf.String(), "Migrating SendFile compress flag")

	var second bytes.Buffer
	secondCmd := newCmd(&second)
	require.NoError(t, v3.MigrateSendFileConfig(secondCmd, dir, nil, nil))

	assert.Equal(t, migrated, readFile(t, file))
	assert.Empty(t, second.String())
}

func Test_MigrateSendFileConfig_LeavesV3ConfigAlone(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	input := `package main

import "github.com/gofiber/fiber/v3"

func handler(c fiber.Ctx) error {
	cfg := fiber.SendFile{Compress: true}
	var other fiber.SendFile
	if err := c.SendFile("a", cfg); err != nil {
		return err
	}
	if err := c.SendFile("b", other); err != nil {
		return err
	}
	if err := c.SendFile("c", &fiber.SendFile{ByteRange: true}); err != nil {
		return err
	}
	return c.SendFile("d", fiber.SendFile{Download: true})
}
`
	file := writeTempFile(t, dir, input)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateSendFileConfig(cmd, dir, nil, nil))

	assert.Equal(t, input, readFile(t, file))
	assert.Empty(t, buf.String())
}

func Test_MigrateSendFileConfig_SkipsBrokenFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	broken := writeTempFile(t, dir, `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	return c.SendFile("./public/index.html", true
}
`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateSendFileConfig(cmd, dir, nil, nil))

	assert.Contains(t, readFile(t, broken), `c.SendFile("./public/index.html", true`)
	assert.NotContains(t, readFile(t, broken), "fiber.SendFile")
	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0].Message, "does not parse")
}

func Test_MigrateSendFileConfig_Receivers(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateSendFileConfig, []ctxCase{
		{
			name: "contexts held in fields and acquired",
			in: `package main

import (
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"
)

type files struct{ c *fiber.Ctx }

func (f files) index() error { return f.c.SendFile("./index.html", true) }

func (f files) movie() error { return f.c.SendFile("./movie.mp4") }

func TestSend(t *testing.T) {
	app := fiber.New()
	ctx := app.AcquireCtx(&fasthttp.RequestCtx{})
	_ = ctx.SendFile("./a.txt", false)
}
`,
			want: `package main

import (
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"
)

type files struct{ c *fiber.Ctx }

func (f files) index() error {
	return f.c.SendFile("./index.html", fiber.SendFile{Compress: true, ByteRange: true})
}

func (f files) movie() error { return f.c.SendFile("./movie.mp4", fiber.SendFile{ByteRange: true}) }

func TestSend(t *testing.T) {
	app := fiber.New()
	ctx := app.AcquireCtx(&fasthttp.RequestCtx{})
	_ = ctx.SendFile("./a.txt", fiber.SendFile{ByteRange: true})
}
`,
			msg: "Migrating SendFile compress flag",
		},
		{
			name: "Download is reported once per file",
			in: `package main

import "github.com/gofiber/fiber/v2"

func report(c *fiber.Ctx) error {
	return c.Download("./report.pdf")
}

func export(c *fiber.Ctx) error {
	return c.Status(fiber.StatusOK).Download("./export.csv", "data.csv")
}
`,
			notes: []string{"main.go:6: c.Download no longer serves byte ranges"},
		},
		{
			name: "Download of another receiver is not reported",
			in: `package main

import "github.com/gofiber/fiber/v2"

type store struct{}

func (store) Download(name string) error { return nil }

func fetch(c *fiber.Ctx) error {
	var s store
	return s.Download("x")
}
`,
		},
		{
			name: "aliased import",
			in: `package main

import f "github.com/gofiber/fiber/v2"

func report(c *f.Ctx) error {
	_ = c.Download("./report.pdf")
	return c.SendFile("./report.pdf", true)
}
`,
			want: `package main

import f "github.com/gofiber/fiber/v2"

func report(c *f.Ctx) error {
	_ = c.Download("./report.pdf")
	return c.SendFile("./report.pdf", f.SendFile{Compress: true, ByteRange: true})
}
`,
			msg:   "Migrating SendFile compress flag",
			notes: []string{"main.go:6: c.Download no longer serves byte ranges"},
		},
	})
}
