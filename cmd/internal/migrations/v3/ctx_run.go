package v3

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/gofiber/cli/cmd/internal"
)

// ctxRun collects what a migration has to say once for the whole project
// rather than at each place: a behavior change that applies to every use of an
// API is reported at its first use, with the number of the others.
type ctxRun struct {
	kinds map[string]*ctxKindNote
	// pkgs are the parsed files of the directories looked at, by directory.
	pkgs map[string][]ctxPackageFile
	// indexes reach the other packages of the module a file is in, and modules
	// are the views of them for the environments (see ctxModule).
	indexes moduleIndexes
	modules map[*moduleIndex]*ctxModule
	order   []string
}

// ctxPackageFile is a parsed file of a directory.
type ctxPackageFile struct {
	file  *ast.File
	path  string
	names []string
}

// ctxKindNote is the first place of a kind of note, and how many places there are.
type ctxKindNote struct {
	file    string
	message string
	line    int
	places  int
}

func newCtxRun() *ctxRun {
	return &ctxRun{kinds: map[string]*ctxKindNote{}, pkgs: map[string][]ctxPackageFile{}, modules: map[*moduleIndex]*ctxModule{}}
}

// module returns the view of the packages of the module the file belongs to, nil
// outside of one.
func (r *ctxRun) module(fi internal.FileInfo) *ctxModule {
	ix := r.indexes.of(fi)
	if ix == nil {
		return nil
	}
	m, ok := r.modules[ix]
	if !ok {
		m = newCtxModule(ix)
		r.modules[ix] = m
	}
	return m
}

// newEnv builds the environment of the file g: its own declarations, those of
// the other files of its package and, in a module, those of the other packages.
func (r *ctxRun) newEnv(fi internal.FileInfo, g *goSource, names []string) *ctxEnv {
	env := newCtxEnv(g, names, r.siblings(fi, g)...)
	env.attach(r.module(fi))
	if len(names) == 0 {
		env.alias = env.freeFiberName()
	}
	return env
}

// open prepares a file for a migration of the methods of the request context: it
// parses it and builds its environment. ok is false for a file that has nothing to
// do with contexts, which is a file that neither imports the fiber package nor
// reaches a package that does: one of the module that it imports, or the package
// it is in.
func (r *ctxRun) open(fi internal.FileInfo, content string) (g *goSource, env *ctxEnv, ok bool) {
	names := fiberNames(content)
	if len(names) == 0 {
		if !r.reachesContexts(fi, ctxImportsOf(content)) {
			return nil, nil, false
		}
		// the file is none of the migration's business when it does not parse
		g, ok = parseSource(content)
	} else {
		g, ok = parseFile(fi, content)
	}
	if !ok {
		return nil, nil, false
	}
	return g, r.newEnv(fi, g, names), true
}

// envFor is open for a file that is parsed already: its environment, nil when the
// file has nothing to do with contexts.
func (r *ctxRun) envFor(fi internal.FileInfo, g *goSource, names []string) *ctxEnv {
	if len(names) == 0 && !r.reachesContexts(fi, g.file) {
		return nil
	}
	return r.newEnv(fi, g, names)
}

// ctxImportsOf parses the package clause and the imports of a source, which is
// all that reachesContexts looks at.
func ctxImportsOf(content string) *ast.File {
	file, err := parser.ParseFile(token.NewFileSet(), "", content, parser.ImportsOnly|parser.SkipObjectResolution)
	if err != nil && file == nil {
		return nil
	}
	return file
}

// reachesContexts reports whether a file that does not import the fiber package
// can still hold a value of a type that embeds the context or an alias of it: it
// imports a package of the module, or the package of the file imports fiber in one
// of its other files. file holds at least its package clause and imports.
func (r *ctxRun) reachesContexts(fi internal.FileInfo, file *ast.File) bool {
	if file == nil {
		return false
	}

	if mod := r.module(fi); mod != nil {
		for _, spec := range file.Imports {
			if importPath, err := strconv.Unquote(spec.Path.Value); err == nil && mod.inModule(importPath) {
				return true
			}
		}
	}

	dir := filepath.Dir(fi.Path)
	files, loaded := r.pkgs[dir]
	if !loaded {
		files = ctxParseDir(dir)
		r.pkgs[dir] = files
	}
	return slices.ContainsFunc(files, func(f ctxPackageFile) bool {
		return f.path != fi.Path && f.file.Name.Name == file.Name.Name && len(f.names) > 0
	})
}

// finish adds the import of the fiber package to the migrated source of a file that
// had none, when the rewrite wrote the qualifier (see ctxEnv.qual).
func (env *ctxEnv) finish(content, out string) string {
	if !env.usedFiber || len(env.names) > 0 || out == content {
		return out
	}

	importPath := ""
	for _, file := range env.files {
		for _, spec := range file.Imports {
			if p, err := strconv.Unquote(spec.Path.Value); err == nil && reFiberCorePath.MatchString(p) {
				importPath = p
			}
		}
	}
	if importPath == "" {
		importPath = env.mod.fiberImportPath()
	}
	if importPath == "" {
		return out
	}
	alias := env.alias
	if alias == ctxFiberDefaultName {
		alias = ""
	}
	return ctxAddImportAs(out, importPath, ctxFiberDefaultName, alias)
}

// ctxFiberDefaultName is the name of the fiber package.
const ctxFiberDefaultName = "fiber"

// freeFiberName returns the name the fiber package gets in a file that does not
// import it: fiber, unless that name means something in the file or in its package
// (a package of the project called fiber, a variable called like it), and then
// fiberv3, with a number after it to make it free.
func (env *ctxEnv) freeFiberName() string {
	name := ctxFiberDefaultName
	for i := 1; env.nameTaken(name); i++ {
		name = ctxFiberDefaultName + "v3"
		if i > 1 {
			name += "_" + strconv.Itoa(i)
		}
	}
	return name
}

// nameTaken reports whether name means something in the file or at the package
// level, so that an import under that name would clash with it or shadow it.
func (env *ctxEnv) nameTaken(name string) bool {
	if env.typeDecl[name] != nil || env.funcDecl[name] != nil || env.pkgVars[name].spec != nil {
		return true
	}
	for _, file := range append([]*ast.File{env.g.file}, env.files...) {
		for _, decl := range file.Decls {
			if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.CONST {
				for _, spec := range gd.Specs {
					if vs, ok := spec.(*ast.ValueSpec); ok && slices.ContainsFunc(vs.Names, func(id *ast.Ident) bool { return id.Name == name }) {
						return true
					}
				}
			}
		}
	}
	for _, spec := range env.g.file.Imports {
		bound := ""
		switch {
		case spec.Name != nil:
			bound = spec.Name.Name
		default:
			if importPath, err := strconv.Unquote(spec.Path.Value); err == nil {
				bound = env.mod.bindName(importPath)
			}
		}
		if bound == name {
			return true
		}
	}

	taken := false
	ast.Inspect(env.g.file, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || id.Name != name || taken {
			return !taken
		}
		taken = true
		return false
	})
	return taken
}

// ctxIsIgnored reports whether file is excluded from the build with a
// "//go:build ignore" line, which makes it no part of its package.
func ctxIsIgnored(file *ast.File) bool {
	for _, group := range file.Comments {
		if group.End() > file.Package {
			break
		}
		for _, c := range group.List {
			if constraint, ok := strings.CutPrefix(c.Text, "//go:build "); ok && strings.TrimSpace(constraint) == "ignore" {
				return true
			}
		}
	}
	return false
}

// siblings returns the other files of the package of the file fi, g, which
// declare the types, functions and methods the file may use. The files of a
// directory are read once per run.
func (r *ctxRun) siblings(fi internal.FileInfo, g *goSource) []ctxSibling {
	dir := filepath.Dir(fi.Path)
	files, loaded := r.pkgs[dir]
	if !loaded {
		files = ctxParseDir(dir)
		r.pkgs[dir] = files
	}

	var siblings []ctxSibling
	for _, f := range files {
		if f.path != fi.Path && f.file.Name.Name == g.file.Name.Name {
			siblings = append(siblings, ctxSibling{file: f.file, names: f.names})
		}
	}
	return siblings
}

// ctxParseDir parses the Go files of dir, without those that do not parse.
func ctxParseDir(dir string) []ctxPackageFile {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var files []ctxPackageFile
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		src, err := os.ReadFile(path) // #nosec G304 -- a file of the project that is migrated
		if err != nil {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, src, parser.SkipObjectResolution|parser.ParseComments)
		if err != nil || ctxIsIgnored(file) {
			continue
		}
		files = append(files, ctxPackageFile{file: file, path: path, names: fiberNames(string(src))})
	}
	return files
}

// report records the collected notes for the project in root.
func (r *ctxRun) report(root string) {
	for _, kind := range r.order {
		k := r.kinds[kind]
		msg := k.message
		if k.places > 1 {
			msg += fmt.Sprintf(" (%d more in this project)", k.places-1)
		}
		internal.AddNote(root, internal.Note{File: k.file, Line: k.line, Message: msg})
	}
}

// ctxNoter hands the notes of one file to the internal package.
type ctxNoter struct {
	g   *goSource
	run *ctxRun
	fi  internal.FileInfo
}

func newCtxNoter(fi internal.FileInfo, g *goSource, run *ctxRun) *ctxNoter {
	return &ctxNoter{fi: fi, g: g, run: run}
}

// notef records a note for the construct at pos.
func (n *ctxNoter) notef(pos token.Pos, format string, args ...any) {
	internal.Notef(n.fi, n.g.line(pos), format, args...)
}

// oncef records a note of the given kind for the construct at pos. Only the first
// place of a kind in the project is reported, with the number of the others.
func (n *ctxNoter) oncef(kind string, pos token.Pos, format string, args ...any) {
	if k, seen := n.run.kinds[kind]; seen {
		k.places++
		return
	}
	n.run.kinds[kind] = &ctxKindNote{file: n.fi.Rel, line: n.g.line(pos), message: fmt.Sprintf(format, args...), places: 1}
	n.run.order = append(n.run.order, kind)
}
