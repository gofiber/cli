package v3_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateUtilsImport(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import (
	"fmt"
	"sync/atomic"

	"github.com/gofiber/fiber/v2/utils"
)

func main() {
	b := []byte(" x ")
	fmt.Println(
		utils.TrimBytes(b, ' '),
		utils.TrimLeftBytes(b, ' '),
		utils.TrimRightBytes(b, ' '),
		utils.EqualFoldBytes(b, b),
		utils.Trim(" x ", ' '),
		utils.TrimLeft(" x", ' '),
		utils.TrimRight("x ", ' '),
		utils.EqualFold("A", "a"),
		utils.GetString(b),
		utils.GetBytes("a"),
		utils.ImmutableString("b"),
		utils.AddTrailingSlash("/a"),
		atomic.LoadUint32(&utils.Timestamp),
		utils.UUID(),
	)
}
`)

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateUtilsImport(newCmd(&buf), dir, nil, nil))

	assert.Equal(t, `package main

import (
	"fmt"

	"github.com/gofiber/utils/v2"
)

func main() {
	b := []byte(" x ")
	fmt.Println(
		utils.Trim(b, ' '),
		utils.TrimLeft(b, ' '),
		utils.TrimRight(b, ' '),
		utils.EqualFold(b, b),
		utils.Trim(" x ", ' '),
		utils.TrimLeft(" x", ' '),
		utils.TrimRight("x ", ' '),
		utils.EqualFold("A", "a"),
		utils.UnsafeString(b),
		utils.UnsafeBytes("a"),
		utils.CopyString("b"),
		utils.AddTrailingSlashString("/a"),
		utils.Timestamp(),
		utils.UUID(),
	)
}
`, readFile(t, file))
	assert.Equal(t, "Migrating utils imports\n", buf.String())
	assert.Empty(t, importsNotes(dir))
}

// ImmutableString copies the string, string(x) of a string does not: the value
// would keep pointing into the request buffer fasthttp reuses.
func Test_MigrateUtilsImport_ImmutableStringKeepsTheCopy(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import "github.com/gofiber/fiber/v2/utils"

var saved string

func save(id string) {
	saved = utils.ImmutableString(id)
}
`)

	require.NoError(t, v3.MigrateUtilsImport(newCmd(&bytes.Buffer{}), dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "saved = utils.CopyString(id)")
	assert.NotContains(t, content, "string(id)")
}

func Test_MigrateUtilsImport_TrimHelpersStayUntouched(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := `package main

import "github.com/gofiber/fiber/v2/utils"

func main() {
	a := utils.Trim(" x ", ' ')
	b := utils.TrimLeft(a, '/')
	c := utils.TrimRight(b, '/')
	_ = utils.EqualFold(c, "x")
}
`
	file := writeTempFile(t, dir, src)

	require.NoError(t, v3.MigrateUtilsImport(newCmd(&bytes.Buffer{}), dir, nil, nil))

	assert.Equal(t, `package main

import "github.com/gofiber/utils/v2"

func main() {
	a := utils.Trim(" x ", ' ')
	b := utils.TrimLeft(a, '/')
	c := utils.TrimRight(b, '/')
	_ = utils.EqualFold(c, "x")
}
`, readFile(t, file))
}

func Test_MigrateUtilsImport_ImportForms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "single line",
			src:  "package main\n\nimport \"github.com/gofiber/fiber/v2/utils\"\n\nvar _ = utils.UUID\n",
			want: "package main\n\nimport \"github.com/gofiber/utils/v2\"\n\nvar _ = utils.UUID\n",
		},
		{
			name: "single line with an alias",
			src:  "package main\n\nimport u \"github.com/gofiber/fiber/v2/utils\"\n\nvar _ = u.GetString\n",
			want: "package main\n\nimport u \"github.com/gofiber/utils/v2\"\n\nvar _ = u.UnsafeString\n",
		},
		{
			name: "trailing comment",
			src:  "package main\n\nimport (\n\t\"github.com/gofiber/fiber/v2/utils\" // fiber helpers\n)\n\nvar _ = utils.UUID\n",
			want: "package main\n\nimport (\n\t\"github.com/gofiber/utils/v2\" // fiber helpers\n)\n\nvar _ = utils.UUID\n",
		},
		{
			name: "after the path was moved to v3",
			src:  "package main\n\nimport \"github.com/gofiber/fiber/v3/utils\"\n\nvar _ = utils.GetBytes\n",
			want: "package main\n\nimport \"github.com/gofiber/utils/v2\"\n\nvar _ = utils.UnsafeBytes\n",
		},
		{
			name: "alias that ends in utils",
			src:  "package main\n\nimport fiberutils \"github.com/gofiber/fiber/v2/utils\"\n\nvar _ = []any{fiberutils.TrimRight(\"/a/\", '/'), fiberutils.EqualFold, fiberutils.ImmutableString}\n",
			want: "package main\n\nimport fiberutils \"github.com/gofiber/utils/v2\"\n\nvar _ = []any{fiberutils.TrimRight(\"/a/\", '/'), fiberutils.EqualFold, fiberutils.CopyString}\n",
		},
		{
			name: "short alias",
			src:  "package main\n\nimport fu \"github.com/gofiber/fiber/v2/utils\"\n\nvar _ = fu.GetString\n",
			want: "package main\n\nimport fu \"github.com/gofiber/utils/v2\"\n\nvar _ = fu.UnsafeString\n",
		},
		{
			name: "dot import",
			src:  "package main\n\nimport . \"github.com/gofiber/fiber/v2/utils\"\n\nvar _ = GetString([]byte(\"a\"))\nvar _ = UUID()\nvar _ = Trim(\"/a\", '/')\n",
			want: "package main\n\nimport . \"github.com/gofiber/utils/v2\"\n\nvar _ = UnsafeString([]byte(\"a\"))\nvar _ = UUID()\nvar _ = Trim(\"/a\", '/')\n",
		},
		{
			name: "blank import",
			src:  "package main\n\nimport _ \"github.com/gofiber/fiber/v2/utils\"\n",
			want: "package main\n\nimport _ \"github.com/gofiber/utils/v2\"\n",
		},
		{
			name: "raw string path",
			src:  "package main\n\nimport utils `github.com/gofiber/fiber/v2/utils`\n\nvar _ = utils.GetString\n",
			want: "package main\n\nimport utils \"github.com/gofiber/utils/v2\"\n\nvar _ = utils.UnsafeString\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			file := writeTempFile(t, dir, tc.src)

			require.NoError(t, v3.MigrateUtilsImport(newCmd(&bytes.Buffer{}), dir, nil, nil))
			assert.Equal(t, tc.want, readFile(t, file))
		})
	}
}

func Test_MigrateUtilsImport_OnlyTouchesTheFiberUtilsPackage(t *testing.T) {
	t.Parallel()

	own := `package main

import "example.com/app/utils"

func main() {
	_ = utils.GetString(nil, "name")
	_ = utils.GetBytes("/etc/hostname")
	_ = utils.Trim("x")
	_ = utils.EqualFold("a", "b")
	_ = utils.ImmutableString("s")
	_ = utils.AssertEqual
	_ = utils.Timestamp
}
`
	dir := importsTree(t, map[string]string{
		"own.go": own,
		"both.go": `package main

import (
	"example.com/app/utils"
	fu "github.com/gofiber/fiber/v2/utils"
)

func both() {
	_ = utils.GetString(nil, "name")
	_ = fu.GetString(nil)
}
`,
		"comments.go": `package main

import "github.com/gofiber/fiber/v2/utils"

// utils.GetString is not used any more, see utils.ImmutableString.
var msg = "utils.GetBytes(s) and utils.AssertEqual are gone"

var _ = utils.UUID
`,
	})

	require.NoError(t, v3.MigrateUtilsImport(newCmd(&bytes.Buffer{}), dir, nil, nil))

	assert.Equal(t, own, importsRead(t, dir, "own.go"), "a project package named utils is not Fiber's")
	assert.Equal(t, `package main

import (
	"example.com/app/utils"
	fu "github.com/gofiber/utils/v2"
)

func both() {
	_ = utils.GetString(nil, "name")
	_ = fu.UnsafeString(nil)
}
`, importsRead(t, dir, "both.go"))
	assert.Equal(t, `package main

import "github.com/gofiber/utils/v2"

// utils.GetString is not used any more, see utils.ImmutableString.
var msg = "utils.GetBytes(s) and utils.AssertEqual are gone"

var _ = utils.UUID
`, importsRead(t, dir, "comments.go"), "comments and strings are not code")
}

func Test_MigrateUtilsImport_LocalNameShadowsThePackage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import "github.com/gofiber/fiber/v2/utils"

type helper struct{}

func (helper) GetString([]byte) string { return "" }

func run() {
	_ = utils.GetString(nil)
	for _, utils := range []helper{{}} {
		_ = utils.GetString(nil)
	}
}

func param(utils helper) string { return utils.GetString(nil) }
`)

	require.NoError(t, v3.MigrateUtilsImport(newCmd(&bytes.Buffer{}), dir, nil, nil))

	content := readFile(t, file)
	assert.Equal(t, 1, strings.Count(content, "utils.UnsafeString(nil)"))
	assert.Equal(t, 2, strings.Count(content, "utils.GetString(nil)"), "the range variable and the parameter are not the package")
}

func Test_MigrateUtilsImport_AssertEqual(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "adds testify",
			src: `package main

import (
	"testing"

	"github.com/gofiber/fiber/v2/utils"
)

func Test_A(t *testing.T) {
	utils.AssertEqual(t, 1, 1, "same")
}

func Benchmark_A(b *testing.B) {
	utils.AssertEqual(b, "a", "a")
}
`,
			want: `package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_A(t *testing.T) {
	require.Equal(t, 1, 1, "same")
}

func Benchmark_A(b *testing.B) {
	require.Equal(b, "a", "a")
}
`,
		},
		{
			name: "testify is already imported",
			src: `package main

import (
	"testing"

	"github.com/gofiber/fiber/v2/utils"
	"github.com/stretchr/testify/require"
)

func Test_A(t *testing.T) {
	require.NotNil(t, t)
	utils.AssertEqual(t, 1, 1)
}
`,
			want: `package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_A(t *testing.T) {
	require.NotNil(t, t)
	require.Equal(t, 1, 1)
}
`,
		},
		{
			name: "testify under another name",
			src: `package main

import (
	"testing"

	"github.com/gofiber/fiber/v2/utils"
	req "github.com/stretchr/testify/require"
)

func Test_A(t *testing.T) {
	req.NotNil(t, t)
	utils.AssertEqual(t, 1, 1)
}
`,
			want: `package main

import (
	"testing"

	req "github.com/stretchr/testify/require"
)

func Test_A(t *testing.T) {
	req.NotNil(t, t)
	req.Equal(t, 1, 1)
}
`,
		},
		{
			name: "the name require is taken by another package",
			src: `package main

import (
	"testing"

	"example.com/require"
	"github.com/gofiber/fiber/v2/utils"
)

func Test_A(t *testing.T) {
	require.That(t)
	utils.AssertEqual(t, 1, 1)
}
`,
			want: `package main

import (
	"testing"

	"example.com/require"
	testifyrequire "github.com/stretchr/testify/require"
)

func Test_A(t *testing.T) {
	require.That(t)
	testifyrequire.Equal(t, 1, 1)
}
`,
		},
		{
			name: "utils is still used",
			src: `package main

import (
	"testing"

	"github.com/gofiber/fiber/v2/utils"
)

func Test_A(t *testing.T) {
	utils.AssertEqual(t, 1, 1)
	_ = utils.UUID()
}
`,
			want: `package main

import (
	"testing"

	"github.com/gofiber/utils/v2"
	"github.com/stretchr/testify/require"
)

func Test_A(t *testing.T) {
	require.Equal(t, 1, 1)
	_ = utils.UUID()
}
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			file := writeTempFile(t, dir, tc.src)

			require.NoError(t, v3.MigrateUtilsImport(newCmd(&bytes.Buffer{}), dir, nil, nil))
			assert.Equal(t, tc.want, readFile(t, file))
		})
	}
}

func Test_MigrateUtilsImport_Timestamp(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import (
	"fmt"
	"sync/atomic"

	"github.com/gofiber/fiber/v2/utils"
)

func main() {
	utils.StartTimeStampUpdater()
	now := atomic.LoadUint32(&utils.Timestamp)
	racy := utils.Timestamp
	age := uint32(10) - utils.Timestamp
	fmt.Println(now, racy, age)
}
`)

	require.NoError(t, v3.MigrateUtilsImport(newCmd(&bytes.Buffer{}), dir, nil, nil))

	assert.Equal(t, `package main

import (
	"fmt"

	"github.com/gofiber/utils/v2"
)

func main() {
	utils.StartTimeStampUpdater()
	now := utils.Timestamp()
	racy := utils.Timestamp()
	age := uint32(10) - utils.Timestamp()
	fmt.Println(now, racy, age)
}
`, readFile(t, file))
	assert.Empty(t, importsNotes(dir))
}

func Test_MigrateUtilsImport_TimestampThatCannotBeMigrated(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import (
	"sync/atomic"

	"github.com/gofiber/fiber/v2/utils"
)

func fake() {
	atomic.StoreUint32(&utils.Timestamp, 42)
	utils.Timestamp = 7
	utils.Timestamp++
	_ = utils.MaxStringLen
}
`)

	require.NoError(t, v3.MigrateUtilsImport(newCmd(&bytes.Buffer{}), dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "atomic.StoreUint32(&utils.Timestamp, 42)", "left as it was so that the compiler points at it")
	assert.Contains(t, content, "utils.Timestamp = 7")

	notes := importsNotes(dir)
	require.Len(t, notes, 4)
	assert.Contains(t, notes[0], "main.go:10: ")
	assert.Contains(t, notes[0], "`utils.Timestamp` is the function `utils.Timestamp()`")
	assert.Contains(t, notes[3], "main.go:13: ")
	assert.Contains(t, notes[3], "`utils.MaxStringLen` does not exist")
}

func Test_MigrateUtilsImport_Idempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import (
	"testing"

	"github.com/gofiber/fiber/v2/utils"
)

func Test_A(t *testing.T) {
	utils.AssertEqual(t, utils.GetString(nil), utils.ImmutableString(""))
}
`)

	first := importsOutput(t, func(buf *bytes.Buffer) error {
		return v3.MigrateUtilsImport(newCmd(buf), dir, nil, nil)
	})
	assert.Equal(t, "Migrating utils imports\n", first)
	after := readFile(t, file)

	second := importsOutput(t, func(buf *bytes.Buffer) error {
		return v3.MigrateUtilsImport(newCmd(buf), dir, nil, nil)
	})
	assert.Empty(t, second)
	assert.Equal(t, after, readFile(t, file))
}

func Test_MigrateUtilsImport_ReportsAFileThatDoesNotParse(t *testing.T) {
	t.Parallel()

	dir := importsTree(t, map[string]string{
		"a_good.go":   "package a\n\nimport \"github.com/gofiber/fiber/v2/utils\"\n\nvar _ = utils.GetString\n",
		"b_broken.go": "package a\n\nimport \"github.com/gofiber/fiber/v2/utils\"\n\nvar _ = (utils.GetString\n",
		"c_good.go":   "package a\n\nimport \"github.com/gofiber/fiber/v2/utils\"\n\nvar _ = utils.GetBytes\n",
	})

	require.NoError(t, v3.MigrateUtilsImport(newCmd(&bytes.Buffer{}), dir, nil, nil))

	assert.Contains(t, importsRead(t, dir, "a_good.go"), "utils.UnsafeString")
	assert.Contains(t, importsRead(t, dir, "c_good.go"), "utils.UnsafeBytes")
	assert.Contains(t, importsRead(t, dir, "b_broken.go"), "fiber/v2/utils")

	notes := importsNotes(dir)
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0], "b_broken.go")
	assert.Contains(t, notes[0], "does not parse")
}

func Test_MigrateUtilsImport_RemovesUnusedUtilsImport(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import (
	"testing"

	"github.com/gofiber/fiber/v3/utils"
)

func foo(t *testing.T) {
	utils.AssertEqual(t, 1, 1)
}
`)

	require.NoError(t, v3.MigrateUtilsImport(newCmd(&bytes.Buffer{}), dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "github.com/gofiber/utils/v2")
	assert.Contains(t, content, "github.com/stretchr/testify/require")
	assert.Contains(t, content, "require.Equal(")
	assert.NotContains(t, content, "utils.")
}
