package v3

import (
	"bytes"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/gomod"
)

// The migration changes the Fiber imports of the project, not those of its dependencies. A dependency that is
// built for Fiber v2 (a session store, a metrics middleware, a swagger handler, a flash helper) stays in the
// program, together with github.com/gofiber/fiber/v2, which go.mod keeps as an indirect requirement because
// of it. What it registers on an app or returns takes the *fiber.Ctx of v2: the project does not build
// against it, or, when the handler is registered as an any, panics when the app starts. ReviewDependencies
// finds those dependencies and the first place the code uses each.

const (
	fiberV2Module = "github.com/gofiber/fiber/v2"
	// depsListTimeout bounds the go list of one module.
	depsListTimeout = 2 * time.Minute
	// depsMaxPlaces is how many other places of a dependency a note names.
	depsMaxPlaces = 3
)

// depsPlace is a place in a file of the project.
type depsPlace struct {
	file string
	line int
}

func (p depsPlace) String() string { return p.file + ":" + strconv.Itoa(p.line) }

// depsUse is a dependency that is built for Fiber v2 and the places the project imports it at.
type depsUse struct {
	module  string
	version string
	// via names the module that imports Fiber v2 when it is not the dependency itself.
	via string
	// user is the package that imports Fiber v2.
	user   string
	places []depsPlace
}

// ReviewDependencies leaves a note for each dependency of the project that is built for Fiber v2. It runs
// after the go.mod work: github.com/gofiber/fiber/v2 is still a requirement of go.mod then only because
// something needs it, and the packages of the dependencies are in the module cache. When go.mod was not
// tidied (the work was skipped with --skip_go_mod, or failed) the go command is not asked, so only the
// direct requirements whose go.mod is in the module cache are looked at, and the note says so.
func ReviewDependencies(cwd string, tidied bool) {
	paths, err := gomod.Paths(cwd)
	if err != nil {
		return
	}

	for _, path := range paths {
		b, err := os.ReadFile(path) // #nosec G304 -- reading a module file of the migrated project
		if err != nil {
			continue
		}
		f, err := modfile.ParseLax(path, b, nil)
		if err != nil {
			continue
		}
		dir := filepath.Dir(path)

		var uses []*depsUse
		if tidied {
			uses = depsFromGoList(cwd, dir, f)
		} else {
			uses = depsFromCache(cwd, dir, f)
		}
		for _, use := range uses {
			reportDependency(cwd, use, !tidied)
		}
	}
}

// reportDependency leaves the note of one dependency, at its first import.
func reportDependency(root string, use *depsUse, fromCache bool) {
	if len(use.places) == 0 {
		return
	}

	var why string
	switch {
	case fromCache:
		why = "requires github.com/gofiber/fiber/v2, so it is probably built for Fiber v2 (go.mod was not tidied by the migration, so the go command could not tell which packages reach Fiber v2: this comes from its go.mod in the module cache)"
	case use.via != "":
		why = "is built for Fiber v2 (through " + use.via + ", whose package " + use.user + " imports github.com/gofiber/fiber/v2, which go.mod keeps as an indirect requirement because of it)"
	default:
		why = "is built for Fiber v2 (its package " + use.user + " imports github.com/gofiber/fiber/v2, which go.mod keeps as an indirect requirement because of it)"
	}
	message := fmt.Sprintf("%s %s %s: what it registers on an app or returns takes the *fiber.Ctx of v2 and does not work with a v3 app, a build error or, "+
		"for a handler registered as an any, a panic when the app starts. Look for a release of it for Fiber v3, replace it, or wrap its handler", use.module, use.version, why)
	if others := use.places[1:]; len(others) > 0 {
		var named []string
		for _, place := range others[:min(len(others), depsMaxPlaces)] {
			named = append(named, place.String())
		}
		if len(others) > depsMaxPlaces {
			named = append(named, fmt.Sprintf("and %d more", len(others)-depsMaxPlaces))
		}
		message += " (also imported at " + strings.Join(named, ", ") + ")"
	}

	first := use.places[0]
	internal.AddNote(root, internal.Note{File: first.file, Line: first.line, Message: message})
}

// projectImport is an import of a Go file of the module.
type projectImport struct {
	path string
	file string
	line int
}

// moduleImports returns the imports of the Go files of the module in dir, in the order of the walk, those of
// the files of tests after those of the other files: the first import of a dependency is where the program uses
// it. The files of the modules below it, of vendor and of the directories the go command ignores are not part of it.
func moduleImports(root, dir string) []projectImport {
	var out []projectImport
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error { //nolint:errcheck // a directory that cannot be read has no imports to report
		if err != nil {
			return nil //nolint:nilerr // an entry that cannot be read has no imports to report
		}
		if d.IsDir() {
			name := d.Name()
			if path != dir && (name == "vendor" || name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			if path != dir {
				if _, statErr := os.Stat(filepath.Join(path, "go.mod")); statErr == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}

		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil && file == nil {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		for _, spec := range file.Imports {
			if importPath, unquoteErr := strconv.Unquote(spec.Path.Value); unquoteErr == nil {
				out = append(out, projectImport{path: importPath, file: filepath.ToSlash(rel), line: fset.Position(spec.Pos()).Line})
			}
		}
		return nil
	})
	slices.SortStableFunc(out, func(a, b projectImport) int {
		return boolOrder(strings.HasSuffix(a.file, "_test.go")) - boolOrder(strings.HasSuffix(b.file, "_test.go"))
	})
	return out
}

// boolOrder orders false before true.
func boolOrder(b bool) int {
	if b {
		return 1
	}
	return 0
}

// depsPackage is a package that go list printed.
type depsPackage struct {
	module  string
	version string
	imports []string
	main    bool
}

// depsFromGoList asks the go command for the packages the module builds and finds the dependencies that
// reach Fiber v2 through their imports. A go.mod that does not require Fiber v2 has none. When the go
// command cannot answer, a note on go.mod says that Fiber v2 stays and that the dependencies could not be named.
func depsFromGoList(root, dir string, f *modfile.File) []*depsUse {
	var v2Line int
	for _, req := range f.Require {
		if req.Mod.Path == fiberV2Module {
			v2Line = req.Syntax.Start.Line
		}
	}
	if v2Line == 0 {
		return nil
	}

	packages, err := goListPackages(dir)
	if err != nil {
		rel, relErr := filepath.Rel(root, filepath.Join(dir, gomod.FileName))
		if relErr != nil {
			rel = gomod.FileName
		}
		internal.AddNote(root, internal.Note{File: filepath.ToSlash(rel), Line: v2Line, Message: "go.mod still requires github.com/gofiber/fiber/v2: a dependency of the project is built for Fiber v2, " +
			"and what it registers on an app or returns does not work with a v3 app (go mod why -m github.com/gofiber/fiber/v2 names one); the dependencies could not be listed: " + err.Error()})
		return nil
	}

	finder := &depsFinder{packages: packages, state: map[string]depsState{}, users: map[string]string{}}
	var uses []*depsUse
	byModule := map[string]*depsUse{}
	for _, imp := range moduleImports(root, dir) {
		pkg, ok := packages[imp.path]
		if !ok || pkg.main || pkg.module == fiberV2Module || pkg.module == "" || !finder.reachesFiberV2(imp.path) {
			continue
		}

		use := byModule[pkg.module]
		if use == nil {
			user := finder.users[imp.path]
			use = &depsUse{module: pkg.module, version: pkg.version, user: user}
			if userModule := packages[user].module; userModule != pkg.module {
				use.via = userModule
			}
			byModule[pkg.module] = use
			uses = append(uses, use)
		}
		if place := (depsPlace{file: imp.file, line: imp.line}); !slices.Contains(use.places, place) {
			use.places = append(use.places, place)
		}
	}
	return uses
}

// errGoList is returned when the go command cannot list the packages of a module.
var errGoList = errors.New("go list failed")

// goListPackages runs go list over the module in dir and returns its packages, the test ones and the
// dependencies, by import path (without the suffix the go command gives a package that it compiles for a test).
func goListPackages(dir string) (map[string]depsPackage, error) {
	const format = `{{.ImportPath}}{{"\t"}}{{if .Module}}{{.Module.Path}}{{"\t"}}{{.Module.Version}}{{"\t"}}{{.Module.Main}}{{else}}{{"\t\t"}}{{end}}{{"\t"}}{{join .Imports "|"}}`
	cmd := internal.ExecCommand("go", "list", "-e", "-deps", "-test", "-f", format, "./...") // #nosec G204 -- fixed arguments
	cmd.Dir = dir
	cmd.Env = append(slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "GOWORK=") }), "GOWORK=off")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%w: %w", errGoList, err)
	}
	timer := time.AfterFunc(depsListTimeout, func() {
		_ = cmd.Process.Kill() //nolint:errcheck // the process may have exited already
	})
	err := cmd.Wait()
	timer.Stop()
	if err != nil {
		msg := strings.Join(strings.Fields(stderr.String()), " ")
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("%w: %s", errGoList, msg)
	}

	packages := map[string]depsPackage{}
	for line := range strings.Lines(stdout.String()) {
		fields := strings.Split(strings.TrimRight(line, "\r\n"), "\t")
		if len(fields) != 5 {
			continue
		}
		path := trimVariant(fields[0])
		pkg := packages[path]
		pkg.module, pkg.version, pkg.main = fields[1], fields[2], fields[3] == appLitTrue
		for imp := range strings.SplitSeq(fields[4], "|") {
			if imp = trimVariant(imp); imp != "" && !slices.Contains(pkg.imports, imp) {
				pkg.imports = append(pkg.imports, imp)
			}
		}
		packages[path] = pkg
	}
	return packages, nil
}

// trimVariant drops the suffix the go command adds to a package it compiles for a test: "p [p.test]".
func trimVariant(path string) string {
	base, _, _ := strings.Cut(path, " [")
	return base
}

// depsState is what depsFinder knows about a package.
type depsState int

const (
	depsUnknown depsState = iota
	depsVisiting
	depsReaches
	depsFree
)

// depsFinder finds the packages that reach a package of Fiber v2 through their imports.
type depsFinder struct {
	packages map[string]depsPackage
	state    map[string]depsState
	// users maps a package that reaches Fiber v2 to the package it reaches it through that imports it.
	users map[string]string
}

// reachesFiberV2 reports whether the package imports a package of Fiber v2, directly or through others.
func (d *depsFinder) reachesFiberV2(path string) bool {
	switch d.state[path] {
	case depsReaches:
		return true
	case depsFree, depsVisiting:
		return false
	case depsUnknown:
	}

	d.state[path] = depsVisiting
	for _, imp := range d.packages[path].imports {
		if d.packages[imp].module == fiberV2Module {
			d.state[path], d.users[path] = depsReaches, path
			return true
		}
		if d.reachesFiberV2(imp) {
			d.state[path], d.users[path] = depsReaches, d.users[imp]
			return true
		}
	}
	d.state[path] = depsFree
	return false
}

// depsFromCache looks, without the go command, at the direct requirements of the module that the code imports:
// one whose go.mod in the module cache requires Fiber v2 is probably built for it.
func depsFromCache(root, dir string, f *modfile.File) []*depsUse {
	cache := goModCache()
	if cache == "" {
		return nil
	}

	var uses []*depsUse
	imports := moduleImports(root, dir)
	for _, req := range f.Require {
		if req.Indirect || isFiberProject(req.Mod.Path) {
			continue
		}
		if !cachedModRequires(cache, req.Mod, fiberV2Module) {
			continue
		}

		use := &depsUse{module: req.Mod.Path, version: req.Mod.Version}
		for _, imp := range imports {
			if imp.path == req.Mod.Path || strings.HasPrefix(imp.path, req.Mod.Path+"/") {
				if place := (depsPlace{file: imp.file, line: imp.line}); !slices.Contains(use.places, place) {
					use.places = append(use.places, place)
				}
			}
		}
		if len(use.places) > 0 {
			uses = append(uses, use)
		}
	}
	return uses
}

// isFiberProject reports whether the module is the core module of Fiber at any major version or a contrib module,
// which this migration moves.
func isFiberProject(path string) bool {
	return strings.HasPrefix(path, "github.com/gofiber/fiber/") || strings.HasPrefix(path, "github.com/gofiber/contrib/")
}

// goModCache returns the directory of the module cache, "" when it cannot be told.
func goModCache() string {
	if dir := os.Getenv("GOMODCACHE"); dir != "" {
		return dir
	}
	if gopath := os.Getenv("GOPATH"); gopath != "" {
		return filepath.Join(filepath.SplitList(gopath)[0], "pkg", "mod")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "go", "pkg", "mod")
}

// cachedModRequires reports whether the go.mod of the module version in the module cache requires path.
func cachedModRequires(cache string, mod module.Version, path string) bool {
	escaped, err := module.EscapePath(mod.Path)
	if err != nil {
		return false
	}
	escapedVersion, err := module.EscapeVersion(mod.Version)
	if err != nil {
		return false
	}
	b, err := os.ReadFile(filepath.Join(cache, "cache", "download", filepath.FromSlash(escaped), "@v", escapedVersion+".mod")) // #nosec G304 -- a file of the module cache
	if err != nil {
		return false
	}
	f, err := modfile.ParseLax("go.mod", b, nil)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(f.Require, func(req *modfile.Require) bool { return req.Mod.Path == path })
}
