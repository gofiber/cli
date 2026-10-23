package v3

import (
	"go/ast"
	"go/token"
	"slices"
	"strconv"
)

// pkgSpeller decides what the code that a rewrite writes into one file calls the
// packages it needs. The usual answer is the name the file gives the package
// (strconv.Atoi), or the package's own name for a file that imports none. It is
// another name when a declaration inside a function hides the usual one where the
// code goes in:
//
//	func start(fiber *fiber.App) { fiber.Listen(":3000", fiber.ListenConfig{...}) }
//
// the package cannot be named there, so the file gets a second import of it under
// a name that nothing in the file uses (fiberv3.ListenConfig). An import is only
// added when the migrated source uses its name, so asking for a name that the code
// then does not write costs nothing.
type pkgSpeller struct {
	g *goSource
	// taken reports whether a name means something at the level of the file or of
	// its package, so that an import under it would clash or shadow.
	taken func(name string) bool
	added []pkgImport
}

// pkgImport is an import that the code of a rewrite may need.
type pkgImport struct {
	path string
	name string
	// plain is set for an import under the package's own name, without an alias.
	plain bool
}

// newPkgSpeller returns the speller of the file g. taken tells what the names of
// the package are; the names that functions of the file declare are known to g.
func newPkgSpeller(g *goSource, taken func(name string) bool) *pkgSpeller {
	return &pkgSpeller{g: g, taken: taken}
}

// at returns the name that code written at pos uses for the package at path. def is
// the name of the package, aliases the names a second import can take, the first
// free one is used (with a number after the last when all are taken).
func (p *pkgSpeller) at(pos token.Pos, path, def string, aliases ...string) string {
	own := pkgImportNames(p.g.file, path)
	for _, name := range own {
		if !p.g.hiddenAt(name, pos) {
			return name
		}
	}
	// a name the speller chose before is free everywhere in the file
	for _, imp := range p.added {
		if imp.path == path {
			return imp.name
		}
	}

	imp := pkgImport{path: path, name: def, plain: true}
	if len(own) > 0 || p.unavailable(def) {
		imp.plain = false
		imp.name = p.freeName(aliases, def)
	}
	p.added = append(p.added, imp)
	return imp.name
}

// unavailable reports whether name cannot be the name of a new import: something
// means it at the level of the file or the package, a function declares it
// somewhere, or an import chosen before has it.
func (p *pkgSpeller) unavailable(name string) bool {
	return p.taken(name) || p.g.declaresLocally(name) ||
		slices.ContainsFunc(p.added, func(imp pkgImport) bool { return imp.name == name })
}

// freeName returns the first of the candidates that is available; when none is, the
// last one with a number after it.
func (p *pkgSpeller) freeName(candidates []string, def string) string {
	if len(candidates) == 0 {
		candidates = []string{"std" + def}
	}
	for _, name := range candidates {
		if !p.unavailable(name) {
			return name
		}
	}
	base := candidates[len(candidates)-1]
	for i := 2; ; i++ {
		if name := base + strconv.Itoa(i); !p.unavailable(name) {
			return name
		}
	}
}

// apply adds the imports the migrated source uses to it.
func (p *pkgSpeller) apply(out string) string {
	for _, imp := range p.added {
		if !ctxUsesName(out, imp.name) {
			continue
		}
		if imp.plain {
			out = ctxAddImport(out, imp.path, imp.name)
			continue
		}
		out = ctxAddNamedImport(out, imp.path, imp.name)
	}
	return out
}

// pkgImportNames returns the names the file gives the package at path. A blank
// import names nothing, and a dot import spells no qualifier.
func pkgImportNames(file *ast.File, path string) []string {
	var names []string
	for _, spec := range file.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil || p != path {
			continue
		}
		switch {
		case spec.Name == nil:
			names = append(names, defaultImportName(path))
		case spec.Name.Name != "_" && spec.Name.Name != ".":
			names = append(names, spec.Name.Name)
		}
	}
	return names
}
