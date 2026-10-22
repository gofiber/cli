package v3

import (
	"fmt"
	"go/ast"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gofiber/cli/cmd/internal"
)

// hcProject is what the healthcheck migration knows about the project before it
// writes a file. A config that is declared in another file than the Use call
// that registers it is split where it is declared, so the declaration and the
// call have to be rewritten together, whatever order the files are visited in:
// a first pass over the files only decides, the second one writes.
type hcProject struct {
	// files are the files that import the healthcheck package and that the
	// migration may rewrite, by path.
	files map[string]hcInput
	// pkgs are the packages that were asked for, read in the first pass, by
	// directory and package clause.
	pkgs map[string]*hcPackage
	// splits are the configs of the package to split, by the file that declares
	// them.
	splits map[string][]hcPackageSplit
	// shims is what the project knows about the module package of the module of
	// each directory, moduleShims the same by the root of the module.
	shims       map[string]*hcShim
	moduleShims map[string]*hcShim
	root        string
	// planning is set during the first pass.
	planning bool
}

// hcInput is a file as the first pass read it.
type hcInput struct {
	content string
	fi      internal.FileInfo
}

// hcPackageSplit is a config of the package that a Use call of another file
// registers: the variable that is split, and the name of the one it gets for the
// readiness probe.
type hcPackageSplit struct {
	name      string
	readiness string
}

// planHealthcheck reads the project and decides which configs are split in a
// file other than the one that registers them.
func planHealthcheck(cwd string) (*hcProject, error) {
	p := &hcProject{
		root:        cwd,
		files:       map[string]hcInput{},
		pkgs:        map[string]*hcPackage{},
		splits:      map[string][]hcPackageSplit{},
		shims:       map[string]*hcShim{},
		moduleShims: map[string]*hcShim{},
	}

	_, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		if len(mw1Qualifiers(mw1Healthcheck.names(content))) > 0 {
			p.files[fi.Path] = hcInput{content: content, fi: fi}
		}
		return content
	})
	if err != nil {
		return nil, fmt.Errorf("read the project: %w", err)
	}

	p.planning = true
	for _, path := range slices.Sorted(maps.Keys(p.files)) {
		in := p.files[path]
		p.migrate(in.content, in.fi)
	}
	p.planning = false
	return p, nil
}

// addSplit records that a Use call, in the planning pass, registers the config
// called name of the file at path.
func (p *hcProject) addSplit(path, name, readiness string) {
	if !p.planning {
		return
	}
	split := hcPackageSplit{name: name, readiness: readiness}
	if !slices.Contains(p.splits[path], split) {
		p.splits[path] = append(p.splits[path], split)
	}
}

// hcPackage is the files of a directory that belong to one package.
type hcPackage struct {
	files []*hcFile
	// complete is false when a file of the directory could not be read or parsed:
	// which package it belongs to, and what it mentions, is not known.
	complete bool
}

// hcFile is a file of a package.
type hcFile struct {
	// h is the rewrite of the file when it imports the healthcheck package.
	h    *hcMigration
	g    *goSource
	path string
}

// packageOf returns the files of the package that the file at path belongs to.
// The directory is read in the planning pass only, so that the second pass sees
// what the first one decided on, however many files it has written by then.
func (p *hcProject) packageOf(path, clause string) *hcPackage {
	dir := filepath.Dir(path)
	key := dir + "\x00" + clause
	if pkg, ok := p.pkgs[key]; ok {
		return pkg
	}
	if !p.planning {
		return &hcPackage{}
	}

	pkg := p.readPackage(dir, clause)
	p.pkgs[key] = pkg
	return pkg
}

func (p *hcProject) readPackage(dir, clause string) *hcPackage {
	pkg := &hcPackage{complete: true}
	entries, err := os.ReadDir(dir)
	if err != nil {
		pkg.complete = false
		return pkg
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		src, err := os.ReadFile(path) // #nosec G304 -- a file of the project being migrated
		if err != nil {
			pkg.complete = false
			continue
		}
		g, ok := parseSource(string(src))
		if !ok {
			pkg.complete = false
			continue
		}
		if g.file.Name.Name != clause {
			continue
		}

		f := &hcFile{g: g, path: path}
		if names := mw1Qualifiers(mw1Healthcheck.names(g.src)); len(names) > 0 {
			f.h = newHCMigration(p, g, internal.FileInfo{Root: p.root, Path: path}, names)
		}
		pkg.files = append(pkg.files, f)
	}
	return pkg
}

// sources returns the files of the package.
func (pkg *hcPackage) sources() []*goSource {
	sources := make([]*goSource, len(pkg.files))
	for i, f := range pkg.files {
		sources[i] = f.g
	}
	return sources
}

// mentions reports whether a file of the package has an identifier called name.
func (pkg *hcPackage) mentions(name string) bool {
	return slices.ContainsFunc(pkg.files, func(f *hcFile) bool { return mw1UsesName(f.g.file, name) })
}

// pkg returns the package of the file being rewritten.
func (h *hcMigration) pkg() *hcPackage {
	return h.proj.packageOf(h.fi.Path, h.g.file.Name.Name)
}

// relPath writes the path of a file of the project the way the notes do.
func (h *hcMigration) relPath(path string) string {
	rel, err := filepath.Rel(h.fi.Root, path)
	if err != nil {
		rel = path
	}
	return filepath.ToSlash(rel)
}

// hcPackageVar returns the declaration of the package level variable called
// name.
func hcPackageVar(file *ast.File, name string) *ast.ValueSpec {
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, isValue := spec.(*ast.ValueSpec)
			if isValue && slices.ContainsFunc(vs.Names, func(id *ast.Ident) bool { return id.Name == name }) {
				return vs
			}
		}
	}
	return nil
}

// hcPackageDecl is what packageDeclaration found.
type hcPackageDecl struct {
	// host is the rewrite of the file that declares the variable, lit its literal.
	// lit is nil when the package does not have exactly one such variable, or when
	// it cannot be followed, and why says so.
	host *hcMigration
	lit  *ast.CompositeLit
	why  string
	// unreadable is set when the reason is a file of the package that cannot be read:
	// what the variable is cannot be told then.
	unreadable bool
}

// packageDeclaration finds the variable called name that another file of the
// package declares with a literal.
func (h *hcMigration) packageDeclaration(name string) hcPackageDecl {
	const notHere = "is not declared next to it"

	pkg := h.pkg()
	if !pkg.complete {
		return hcPackageDecl{why: notHere + " and a file of its package cannot be read, so it cannot be followed", unreadable: true}
	}

	var found *hcFile
	var spec *ast.ValueSpec
	for _, f := range pkg.files {
		if s := hcPackageVar(f.g.file, name); s != nil {
			if found != nil {
				return hcPackageDecl{why: "is declared more than once in its package, as build constraints can have it"}
			}
			found, spec = f, s
		}
	}
	if found == nil || found.h == nil || found.path == h.fi.Path || len(spec.Names) != 1 || len(spec.Values) != 1 {
		return hcPackageDecl{why: notHere}
	}

	value, isLit := spec.Values[0].(*ast.CompositeLit)
	if !isLit {
		return hcPackageDecl{why: "is declared in " + h.relPath(found.path) + ", but not with a literal"}
	}
	return hcPackageDecl{host: found.h, lit: value}
}

// splitPackageConfigs splits the variables of the file that a Use call of
// another file of the package registers, as the planning pass decided.
func (h *hcMigration) splitPackageConfigs() {
	if h.proj.planning {
		return
	}

	for _, s := range h.proj.splits[h.fi.Path] {
		spec := hcPackageVar(h.g.file, s.name)
		if spec == nil || len(spec.Values) != 1 {
			continue
		}
		lit, isLit := spec.Values[0].(*ast.CompositeLit)
		if !isLit {
			continue
		}
		cfg, ok := h.parseConfig(lit)
		if !ok {
			continue
		}
		if apply, splittable := h.splitDeclaration(lit, cfg, s.readiness); splittable {
			apply()
		}
	}
}

// portableEndpoint writes the endpoint of a config that another file declares so
// that this file can register the route with it. A path is portable when it is
// made of string literals and names of the package, which this file can write as
// they are unless a declaration of the code around the call hides one, and of the
// constants of the healthcheck package, which are written with the name this file
// gives to it. stack is the chain of ancestors of the call.
func portableEndpoint(e hcEndpoint, dh *hcMigration, pkgName string, stack []ast.Node) (hcEndpoint, bool) {
	if e.expr == nil {
		return e, true
	}

	var scope ast.Node
	if len(stack) > 1 {
		scope = stack[1]
	}
	text, ok := portableEndpointText(e.expr, dh.names, pkgName, scope)
	if !ok {
		return hcEndpoint{}, false
	}
	return hcEndpoint{text: text, literal: e.literal, isLiteral: e.isLiteral}, true
}

func portableEndpointText(e ast.Expr, declNames []string, pkgName string, scope ast.Node) (string, bool) {
	switch x := e.(type) {
	case *ast.BasicLit:
		return x.Value, x.Kind == token.STRING
	case *ast.ParenExpr:
		inner, ok := portableEndpointText(x.X, declNames, pkgName, scope)
		return "(" + inner + ")", ok
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return "", false
		}
		left, lok := portableEndpointText(x.X, declNames, pkgName, scope)
		right, rok := portableEndpointText(x.Y, declNames, pkgName, scope)
		return left + " + " + right, lok && rok
	case *ast.Ident:
		return x.Name, scope != nil && !mw1UsesName(scope, x.Name)
	case *ast.SelectorExpr:
		for member, renamed := range hcRenamedMembers {
			if mw1IsMember(x, declNames, member) {
				return pkgName + "." + renamed, true
			}
		}
	}
	return "", false
}
