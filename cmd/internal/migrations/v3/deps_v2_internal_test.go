package v3

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
)

// stubDepsGoList replaces the go command with this test binary, which prints the output given in the
// environment (TestDepsGoListProcess), and returns the arguments it was asked with.
func stubDepsGoList(t *testing.T, stdout, stderr string, exitCode int) (asked *[]string) {
	t.Helper()

	asked = new([]string)
	orig := internal.ExecCommand
	internal.ExecCommand = func(name string, args ...string) *exec.Cmd {
		*asked = append(*asked, append([]string{name}, args...)...)
		cmd := exec.CommandContext(context.Background(), os.Args[0], "-test.run=TestDepsGoListProcess", "--") // #nosec G204 -- test helper
		return cmd
	}
	t.Cleanup(func() { internal.ExecCommand = orig })

	t.Setenv("GO_WANT_DEPS_GOLIST", "1")
	t.Setenv("GO_DEPS_GOLIST_STDOUT", stdout)
	t.Setenv("GO_DEPS_GOLIST_STDERR", stderr)
	t.Setenv("GO_DEPS_GOLIST_EXIT", strconv.Itoa(exitCode))
	return asked
}

// TestDepsGoListProcess is the helper process of stubDepsGoList, not a test.
func TestDepsGoListProcess(t *testing.T) {
	if os.Getenv("GO_WANT_DEPS_GOLIST") != "1" {
		return
	}
	if got := os.Getenv("GOWORK"); got != "off" {
		_, _ = fmt.Fprintf(os.Stderr, "helper: GOWORK=%q, want off\n", got)
		os.Exit(91)
	}
	_, _ = fmt.Fprint(os.Stdout, os.Getenv("GO_DEPS_GOLIST_STDOUT"))
	_, _ = fmt.Fprint(os.Stderr, os.Getenv("GO_DEPS_GOLIST_STDERR"))
	code, err := strconv.Atoi(os.Getenv("GO_DEPS_GOLIST_EXIT"))
	if err != nil {
		code = 92
	}
	os.Exit(code) // helper process exits intentionally
}

func depsWrite(t *testing.T, dir, name, content string) {
	t.Helper()

	path := filepath.Join(dir, filepath.FromSlash(name))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

// depsNotes runs the review and returns the notes as "file:line: message".
func depsNotes(t *testing.T, dir string, tidied bool) []string {
	t.Helper()

	ReviewDependencies(dir, tidied)
	var out []string
	for _, n := range internal.DrainNotes(dir) {
		out = append(out, n.String())
	}
	return out
}

// goListLine is a line of the output of the go list the review asks for.
func goListLine(path, module, version string, main bool, imports ...string) string {
	return path + "\t" + module + "\t" + version + "\t" + strconv.FormatBool(main) + "\t" + strings.Join(imports, "|") + "\n"
}

const depsGoMod = `module example.com/app

go 1.26.0

require (
	github.com/ansrivas/fiberprometheus/v2 v2.6.1
	github.com/gofiber/fiber/v3 v3.6.1
	github.com/gofiber/session/v2 v2.0.2
)

require github.com/gofiber/fiber/v2 v2.52.15 // indirect
`

func Test_ReviewDependencies_NamesTheModulesThatAreBuiltForFiberV2(t *testing.T) {
	dir := t.TempDir()
	depsWrite(t, dir, "go.mod", depsGoMod)
	depsWrite(t, dir, "main.go", `package main

import (
	"github.com/ansrivas/fiberprometheus/v2"
	"github.com/gofiber/fiber/v3"
)

var _ = fiberprometheus.New
var _ = fiber.New
`)
	depsWrite(t, dir, "internal/web/web.go", `package web

import (
	sessions "github.com/gofiber/session/v2"
	"github.com/gofiber/fiber/v3"
)

var _ = sessions.New
var _ = fiber.New
`)
	depsWrite(t, dir, "internal/web/web_test.go", `package web

import "github.com/gofiber/session/v2"

var _ = session.New
`)
	// the go command prints the packages in dependency order, the test variants carry a suffix
	asked := stubDepsGoList(t,
		goListLine("github.com/gofiber/fiber/v2", "github.com/gofiber/fiber/v2", "v2.52.15", false, "github.com/valyala/fasthttp")+
			goListLine("github.com/valyala/fasthttp", "github.com/valyala/fasthttp", "v1.75.0", false)+
			goListLine("github.com/ansrivas/fiberprometheus/v2", "github.com/ansrivas/fiberprometheus/v2", "v2.6.1", false, "github.com/gofiber/fiber/v2", "github.com/valyala/fasthttp")+
			goListLine("github.com/fasthttp/session/v2", "github.com/fasthttp/session/v2", "v2.2.4", false, "github.com/valyala/fasthttp")+
			goListLine("github.com/gofiber/session/v2", "github.com/gofiber/session/v2", "v2.0.2", false, "github.com/fasthttp/session/v2", "github.com/gofiber/fiber/v2")+
			goListLine("github.com/gofiber/fiber/v3", "github.com/gofiber/fiber/v3", "v3.6.1", false, "github.com/valyala/fasthttp")+
			goListLine("example.com/app", "example.com/app", "", true, "github.com/ansrivas/fiberprometheus/v2", "github.com/gofiber/fiber/v3")+
			goListLine("example.com/app/internal/web", "example.com/app", "", true, "github.com/gofiber/fiber/v3", "github.com/gofiber/session/v2")+
			goListLine("example.com/app/internal/web [example.com/app/internal/web.test]", "example.com/app", "", true, "github.com/gofiber/fiber/v3", "github.com/gofiber/session/v2"),
		"", 0)

	notes := depsNotes(t, dir, true)

	assert.Equal(t, []string{"go", "list", "-e", "-deps", "-test"}, (*asked)[:5])
	require.Len(t, notes, 2, "%v", notes)
	assert.Contains(t, notes, "main.go:4: github.com/ansrivas/fiberprometheus/v2 v2.6.1 is built for Fiber v2 (its package github.com/ansrivas/fiberprometheus/v2 imports github.com/gofiber/fiber/v2, "+
		"which go.mod keeps as an indirect requirement because of it): what it registers on an app or returns takes the *fiber.Ctx of v2 and does not work with a v3 app, "+
		"a build error or, for a handler registered as an any, a panic when the app starts. Look for a release of it for Fiber v3, replace it, or wrap its handler")
	web := ""
	for _, n := range notes {
		if n[:len("internal/web/web.go")] == "internal/web/web.go" {
			web = n
		}
	}
	assert.Contains(t, web, "internal/web/web.go:4: github.com/gofiber/session/v2 v2.0.2 is built for Fiber v2")
	assert.Contains(t, web, "(also imported at internal/web/web_test.go:3)", "the other file, a test, is named")
}

func Test_ReviewDependencies_NamesTheModuleThatReachesFiberV2ThroughAnother(t *testing.T) {
	dir := t.TempDir()
	depsWrite(t, dir, "go.mod", depsGoMod)
	depsWrite(t, dir, "main.go", "package main\n\nimport _ \"example.org/wrapper\"\n")
	stubDepsGoList(t,
		goListLine("github.com/gofiber/fiber/v2", "github.com/gofiber/fiber/v2", "v2.52.15", false)+
			goListLine("example.org/legacy", "example.org/legacy", "v1.0.0", false, "github.com/gofiber/fiber/v2")+
			goListLine("example.org/wrapper", "example.org/wrapper", "v1.2.0", false, "example.org/legacy")+
			goListLine("example.com/app", "example.com/app", "", true, "example.org/wrapper"),
		"", 0)

	notes := depsNotes(t, dir, true)

	require.Len(t, notes, 1, "%v", notes)
	assert.Contains(t, notes[0], "main.go:3: example.org/wrapper v1.2.0 is built for Fiber v2 (through example.org/legacy, whose package example.org/legacy imports github.com/gofiber/fiber/v2")
}

func Test_ReviewDependencies_TheFirstPlaceIsWhereTheProgramUsesIt(t *testing.T) {
	dir := t.TempDir()
	depsWrite(t, dir, "go.mod", depsGoMod)
	// the walk reaches the test of the a package before the file of the z package
	depsWrite(t, dir, "a/a_test.go", "package a\n\nimport _ \"github.com/gofiber/session/v2\"\n")
	depsWrite(t, dir, "z/z.go", "package z\n\nimport (\n\t\"fmt\"\n\n\t_ \"github.com/gofiber/session/v2\"\n)\n\nvar _ = fmt.Sprint\n")
	stubDepsGoList(t,
		goListLine("github.com/gofiber/fiber/v2", "github.com/gofiber/fiber/v2", "v2.52.15", false)+
			goListLine("github.com/gofiber/session/v2", "github.com/gofiber/session/v2", "v2.0.2", false, "github.com/gofiber/fiber/v2")+
			goListLine("example.com/app/a [example.com/app/a.test]", "example.com/app", "", true, "github.com/gofiber/session/v2")+
			goListLine("example.com/app/z", "example.com/app", "", true, "github.com/gofiber/session/v2"),
		"", 0)

	notes := depsNotes(t, dir, true)

	require.Len(t, notes, 1, "%v", notes)
	assert.Contains(t, notes[0], "z/z.go:6: github.com/gofiber/session/v2 v2.0.2 is built for Fiber v2")
	assert.Contains(t, notes[0], "(also imported at a/a_test.go:3)")
}

func Test_ReviewDependencies_PackagesThatDoNotReachFiberV2AreNoDependenciesOnIt(t *testing.T) {
	dir := t.TempDir()
	depsWrite(t, dir, "go.mod", depsGoMod)
	// the module requires Fiber v2 for its tests, the package the project imports does not use it
	depsWrite(t, dir, "main.go", "package main\n\nimport (\n\t_ \"github.com/gofiber/storage/azureblob\"\n\t_ \"github.com/gofiber/fiber/v2\"\n)\n")
	stubDepsGoList(t,
		goListLine("github.com/gofiber/fiber/v2", "github.com/gofiber/fiber/v2", "v2.52.15", false)+
			goListLine("github.com/gofiber/storage/azureblob", "github.com/gofiber/storage/azureblob", "v1.3.6", false, "github.com/Azure/azure-sdk-for-go/sdk/azcore")+
			goListLine("example.com/app", "example.com/app", "", true, "github.com/gofiber/storage/azureblob", "github.com/gofiber/fiber/v2"),
		"", 0)

	assert.Empty(t, depsNotes(t, dir, true), "azureblob is not built for Fiber v2, and a project file that imports Fiber v2 itself is another note")
}

func Test_ReviewDependencies_NothingToDoWithoutFiberV2InGoMod(t *testing.T) {
	dir := t.TempDir()
	depsWrite(t, dir, "go.mod", "module example.com/app\n\ngo 1.26.0\n\nrequire github.com/gofiber/fiber/v3 v3.6.1\n")
	depsWrite(t, dir, "main.go", "package main\n")
	asked := stubDepsGoList(t, "", "", 0)

	assert.Empty(t, depsNotes(t, dir, true))
	assert.Empty(t, *asked, "the go command is not asked when go.mod does not keep Fiber v2")
}

func Test_ReviewDependencies_WhenTheGoCommandFails(t *testing.T) {
	dir := t.TempDir()
	depsWrite(t, dir, "go.mod", depsGoMod)
	depsWrite(t, dir, "main.go", "package main\n")
	stubDepsGoList(t, "", "go: updates to go.mod needed; to update it:\n\tgo mod tidy\n", 1)

	notes := depsNotes(t, dir, true)

	require.Len(t, notes, 1)
	assert.Contains(t, notes[0], "go.mod:11: go.mod still requires github.com/gofiber/fiber/v2: a dependency of the project is built for Fiber v2")
	assert.Contains(t, notes[0], "go mod why -m github.com/gofiber/fiber/v2")
	assert.Contains(t, notes[0], "the dependencies could not be listed: go list failed: go: updates to go.mod needed; to update it: go mod tidy")
}

func Test_ReviewDependencies_NestedModulesAreLookedAtOneByOne(t *testing.T) {
	dir := t.TempDir()
	depsWrite(t, dir, "go.mod", "module example.com/app\n\ngo 1.26.0\n\nrequire github.com/gofiber/fiber/v3 v3.6.1\n")
	depsWrite(t, dir, "main.go", "package main\n\nimport _ \"github.com/gofiber/session/v2\"\n")
	depsWrite(t, dir, "tools/go.mod", depsGoMod)
	depsWrite(t, dir, "tools/tool.go", "package tools\n\nimport _ \"github.com/gofiber/session/v2\"\n")
	stubDepsGoList(t,
		goListLine("github.com/gofiber/fiber/v2", "github.com/gofiber/fiber/v2", "v2.52.15", false)+
			goListLine("github.com/gofiber/session/v2", "github.com/gofiber/session/v2", "v2.0.2", false, "github.com/gofiber/fiber/v2")+
			goListLine("example.com/app/tools", "example.com/app/tools", "", true, "github.com/gofiber/session/v2"),
		"", 0)

	notes := depsNotes(t, dir, true)

	require.Len(t, notes, 1, "the root module keeps no Fiber v2; the nested one does")
	assert.Contains(t, notes[0], "tools/tool.go:3: github.com/gofiber/session/v2 v2.0.2 is built for Fiber v2")
}

// A fake module cache: the go.mod files of the versions the project requires.
func depsModCache(t *testing.T, files map[string]string) {
	t.Helper()

	cache := t.TempDir()
	for name, content := range files {
		depsWrite(t, cache, "cache/download/"+name, content)
	}
	t.Setenv("GOMODCACHE", cache)
}

func Test_ReviewDependencies_WithoutTheGoCommand(t *testing.T) {
	dir := t.TempDir()
	depsWrite(t, dir, "go.mod", depsGoMod+"\nrequire github.com/gofiber/storage/azureblob v1.3.6\nrequire example.org/unknown v1.0.0\n")
	depsWrite(t, dir, "main.go", `package main

import (
	"github.com/ansrivas/fiberprometheus/v2"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/session/v2"
	_ "example.org/unknown"
)

var _ = fiberprometheus.New
var _ = fiber.New
var _ = session.New
`)
	depsModCache(t, map[string]string{
		"github.com/ansrivas/fiberprometheus/v2/@v/v2.6.1.mod": "module github.com/ansrivas/fiberprometheus/v2\n\nrequire github.com/gofiber/fiber/v2 v2.50.0\n",
		"github.com/gofiber/session/v2/@v/v2.0.2.mod":          "module github.com/gofiber/session/v2\n\nrequire github.com/gofiber/fiber/v2 v2.1.0\n",
		"github.com/gofiber/fiber/v3/@v/v3.6.1.mod":            "module github.com/gofiber/fiber/v3\n\nrequire github.com/valyala/fasthttp v1.75.0\n",
		"github.com/gofiber/storage/azureblob/@v/v1.3.6.mod":   "module github.com/gofiber/storage/azureblob\n\nrequire github.com/gofiber/fiber/v2 v2.52.0 // indirect\n",
	})
	asked := stubDepsGoList(t, "", "", 0)

	notes := depsNotes(t, dir, false)

	assert.Empty(t, *asked, "go.mod was not tidied: the go command is not run")
	require.Len(t, notes, 2, "%v", notes)
	for _, n := range notes {
		assert.Contains(t, n, "requires github.com/gofiber/fiber/v2, so it is probably built for Fiber v2 (go.mod was not tidied by the migration, so it is not known which of its packages reach Fiber v2: this comes from its go.mod in the module cache)")
	}
	assert.Contains(t, notes[0]+notes[1], "main.go:4: github.com/ansrivas/fiberprometheus/v2 v2.6.1 requires")
	assert.Contains(t, notes[0]+notes[1], "main.go:6: github.com/gofiber/session/v2 v2.0.2 requires")
}
