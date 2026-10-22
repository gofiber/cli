package v3_test

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
)

// mw1Migration is the signature of the exported migrations.
type mw1Migration func(cmd *cobra.Command, cwd string, curr, target *semver.Version) error

// mw1Result is what a migration did to a project.
type mw1Result struct {
	files  map[string]string
	stdout string
	notes  []string
}

// file returns the migrated main.go.
func (r mw1Result) file() string { return r.files["main.go"] }

// mw1RunFiles runs the migration over a project made of the given files
// (name to content) and returns the migrated files and what was printed.
func mw1RunFiles(t *testing.T, migrate mw1Migration, files map[string]string) mw1Result {
	t.Helper()

	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}

	var buf bytes.Buffer
	require.NoError(t, migrate(mw1Cmd(&buf), dir, nil, nil))

	res := mw1Result{files: map[string]string{}, stdout: buf.String()}
	for name := range files {
		data, err := os.ReadFile(filepath.Join(dir, name)) // #nosec G304 -- a test fixture
		require.NoError(t, err)
		res.files[name] = string(data)
		assert.Empty(t, mw1MissingImports(string(data)), "%s uses a package that it does not import", name)
	}
	for _, n := range internal.DrainNotes(dir) {
		res.notes = append(res.notes, n.String())
	}
	return res
}

// mw1Module is what a migration did to a project that has a go.mod.
type mw1Module struct {
	// added are the Go files the migration wrote that the project did not have, by
	// path below the project.
	added map[string]string
	mw1Result
}

// mw1RunModule is mw1RunFiles for a project in the module example.com/app, which
// the migrations that add a file to the module package need. The files may bring
// their own go.mod.
func mw1RunModule(t *testing.T, migrate mw1Migration, files map[string]string) mw1Module {
	t.Helper()

	withMod := map[string]string{"go.mod": "module example.com/app\n\ngo 1.25\n"}
	maps.Copy(withMod, files)

	dir := t.TempDir()
	for name, content := range withMod {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}

	var buf bytes.Buffer
	require.NoError(t, migrate(mw1Cmd(&buf), dir, nil, nil))

	res := mw1Module{mw1Result: mw1Result{files: map[string]string{}, stdout: buf.String()}, added: map[string]string{}}
	require.NoError(t, filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, relErr := filepath.Rel(dir, path)
		require.NoError(t, relErr)
		rel = filepath.ToSlash(rel)
		data, readErr := os.ReadFile(path) // #nosec G304 -- a test fixture
		require.NoError(t, readErr)
		if _, given := files[rel]; given {
			res.files[rel] = string(data)
			assert.Empty(t, mw1MissingImports(string(data)), "%s uses a package that it does not import", rel)
		} else if strings.HasSuffix(rel, ".go") {
			res.added[rel] = string(data)
		}
		return nil
	}))
	for _, n := range internal.DrainNotes(dir) {
		res.notes = append(res.notes, n.String())
	}
	return res
}

// mw1Run runs the migration over a single main.go.
func mw1Run(t *testing.T, migrate mw1Migration, src string) mw1Result {
	t.Helper()
	return mw1RunFiles(t, migrate, map[string]string{"main.go": src})
}

func mw1Cmd(buf *bytes.Buffer) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	return cmd
}

// mw1LineOf returns the 1-based number of the first line of src that contains
// needle.
func mw1LineOf(t *testing.T, src, needle string) int {
	t.Helper()
	for i, line := range strings.Split(src, "\n") {
		if strings.Contains(line, needle) {
			return i + 1
		}
	}
	require.FailNow(t, "line not found", needle)
	return 0
}

// mw1Packages are the packages whose qualifiers the migrations write into the
// code they produce.
var mw1Packages = strings.Fields("extractors fiber strings time os fs sha256 base64 healthcheck csrf basicauth cors cache encryptcookie envvar favicon fibermigrate attribute")

var mw1VersionElement = regexp.MustCompile(`^v[0-9]+$`)

// mw1MissingImports returns the qualifiers of a migrated file that name one of
// mw1Packages although the file does not import it. A file that goes through
// goimports gets such an import when the module cache holds the package, which
// hides the mistake from a test that runs on a developer machine, and a user
// whose cache is empty gets code that does not build.
func mw1MissingImports(src string) []string {
	file, err := parser.ParseFile(token.NewFileSet(), "", src, 0)
	if err != nil {
		return nil
	}

	imported := map[string]bool{}
	for _, spec := range file.Imports {
		if spec.Name != nil {
			imported[spec.Name.Name] = true
			continue
		}
		importPath, unquoteErr := strconv.Unquote(spec.Path.Value)
		if unquoteErr != nil {
			continue
		}
		name := path.Base(importPath)
		if mw1VersionElement.MatchString(name) {
			name = path.Base(path.Dir(importPath))
		}
		imported[name] = true
	}

	var missing []string
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, isIdent := sel.X.(*ast.Ident)
		if isIdent && id.Obj == nil && !imported[id.Name] && slices.Contains(mw1Packages, id.Name) && !slices.Contains(missing, id.Name) {
			missing = append(missing, id.Name)
		}
		return true
	})
	return missing
}
