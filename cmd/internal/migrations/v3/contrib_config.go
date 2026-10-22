package v3

import (
	"cmp"
	"go/ast"
	"go/token"
	"slices"
	"strconv"
	"strings"

	"github.com/gofiber/cli/cmd/internal"
)

// Import paths the contrib migrations add to the files they rewrite.
const (
	contribFiberPath      = "github.com/gofiber/fiber/v3"
	contribExtractorsPath = "github.com/gofiber/fiber/v3/extractors"
)

// contribImport is an import of a contrib module, by whatever path.
type contribImport struct {
	spec *ast.ImportSpec
	// name is the identifier the code uses for the package.
	name string
	// legacy is set for the standalone modules (gofiber/jwt, gofiber/websocket)
	// that were folded into contrib.
	legacy bool
}

// contribImportsOf returns the imports of the root package of the contrib module
// short, through its v2 era path as well as through its v3 one. Blank and dot
// imports are left out: they bind no name the migrations could look for.
func contribImportsOf(g *goSource, short string) []contribImport {
	var imports []contribImport
	for _, spec := range g.file.Imports {
		path := contribUnquote(spec.Path.Value)

		var name string
		legacy := false
		if path == contribV3Prefix+short {
			name = internal.ContribV3PackageName(short)
		} else {
			t, ok := contribTargetOf(path)
			if !ok || t.short != short || t.name == "" {
				continue
			}
			name, legacy = t.name, !strings.HasPrefix(path, contribPrefix)
		}

		if spec.Name != nil {
			name = spec.Name.Name
		}
		if name == "_" || name == "." {
			continue
		}
		imports = append(imports, contribImport{spec: spec, name: name, legacy: legacy})
	}
	return imports
}

// contribNames returns the identifiers the file uses for the contrib module short.
func contribNames(g *goSource, short string) []string {
	var names []string
	for _, imp := range contribImportsOf(g, short) {
		names = append(names, imp.name)
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// contribFile is a Go file being rewritten by one of the contrib migrations.
type contribFile struct {
	fi     internal.FileInfo
	g      *goSource
	idents map[string]bool
	// extractors and fiber are the names of the fiber extractors package and of
	// the fiber package, filled on first use.
	extractors string
	fiber      string
	edits      editSet
}

func newContribFile(g *goSource, fi internal.FileInfo) *contribFile {
	return &contribFile{g: g, fi: fi, idents: contribIdentNames(g.file)}
}

// apply returns the rewritten source.
func (f *contribFile) apply() string {
	return f.edits.apply(f.g.src)
}

// notef tells the user about the construct at pos.
func (f *contribFile) notef(pos token.Pos, format string, args ...any) {
	internal.Notef(f.fi, f.g.line(pos), format, args...)
}

// extractorsName returns the name of the fiber extractors package in this file,
// importing the package when it is not imported yet.
func (f *contribFile) extractorsName() string {
	if f.extractors == "" {
		f.extractors = f.importName(contribExtractorsPath, "extractors", "fiberextractors")
	}
	return f.extractors
}

// fiberName returns the name of the fiber package in this file, importing it when
// it is not imported yet.
func (f *contribFile) fiberName() string {
	if f.fiber == "" {
		if names := fiberNames(f.g.src); len(names) > 0 && names[0] != "." {
			f.fiber = names[0]
		} else {
			f.fiber = f.importName(contribFiberPath, "fiber", "fiberv3")
		}
	}
	return f.fiber
}

// importName returns the name the file has for path, adding the import when it
// is missing. The new import carries the explicit name alt when the file uses
// the default name (def) for something else.
func (f *contribFile) importName(path, def, alt string) string {
	name := ""
	if f.idents[def] && !f.imports(path) {
		name = alt
	}
	return contribAddImport(f.g, &f.edits, path, name)
}

// imports reports whether the file imports path.
func (f *contribFile) imports(path string) bool {
	want := strconv.Quote(path)
	return slices.ContainsFunc(f.g.file.Imports, func(spec *ast.ImportSpec) bool { return spec.Path.Value == want })
}

// hasStd reports whether stdName can give the file the standard library package
// path, which it cannot when the file uses the name of the package for something
// else.
func (f *contribFile) hasStd(path string) bool {
	return f.imports(path) || !f.idents[defaultImportName(path)]
}

// stdName returns the name of a standard library package in this file, adding the
// import when it is missing. ok is false when the file uses the name for
// something else.
func (f *contribFile) stdName(path string) (name string, ok bool) {
	name = defaultImportName(path)
	if f.imports(path) {
		for _, spec := range f.g.file.Imports {
			if spec.Path.Value == strconv.Quote(path) && spec.Name != nil && spec.Name.Name != "_" && spec.Name.Name != "." {
				return spec.Name.Name, true
			}
		}
		return name, true
	}
	if f.idents[name] {
		return "", false
	}
	return contribAddImport(f.g, &f.edits, path, ""), true
}

// contribSlot is one place that sets a field of a config: an element of its
// literal, or an assignment to the variable the literal was bound to.
type contribSlot struct {
	value  ast.Expr
	assign *ast.AssignStmt
	field  string
	index  int
	// direct is set when the slot runs whenever the literal does, which is how
	// the migrations combine the fields of a config. An assignment inside an if
	// or a loop does not.
	direct bool
}

// pos returns the position of the slot.
func (s *contribSlot) pos() token.Pos {
	return s.value.Pos()
}

// contribConfig is a value of the Config type of a contrib package: a composite
// literal and the assignments that follow it when it is bound to a variable.
type contribConfig struct {
	f *contribFile
	// lit is nil for a variable that is declared without a value.
	lit *ast.CompositeLit
	// decl is the statement that declares the variable.
	decl ast.Stmt
	// pkg is the name the file has for the package of the config.
	pkg     string
	varName string
	slots   []*contribSlot
	added   []string
	removed []int
}

// findConfigs finds the values of the type Config of the packages names in the
// file: composite literals, and variables declared with the type. The
// assignments to the fields of the variable a value is bound to belong to it:
// cfg := jwtware.Config{...} followed by cfg.TokenLookup = "...".
func (f *contribFile) findConfigs(names []string) []*contribConfig {
	var configs []*contribConfig
	f.g.walk(func(n ast.Node, stack []ast.Node) bool {
		switch x := n.(type) {
		case *ast.CompositeLit:
			if x.Type == nil || !contribIsPkgType(x.Type, names, "Config") {
				return true
			}
			c := &contribConfig{f: f, lit: x, pkg: contribPkgOf(x.Type)}
			for i, elt := range x.Elts {
				if kv, ok := elt.(*ast.KeyValueExpr); ok && contribKeyName(elt) != "" {
					c.slots = append(c.slots, &contribSlot{field: contribKeyName(elt), value: kv.Value, index: i, direct: true})
				}
			}
			c.bind(x, stack)
			configs = append(configs, c)
		case *ast.ValueSpec:
			sel, ok := x.Type.(*ast.SelectorExpr)
			if !ok || len(x.Values) > 0 || !contribIsPkgType(sel, names, "Config") {
				return true
			}
			for _, id := range x.Names {
				c := &contribConfig{f: f, varName: id.Name, pkg: contribPkgOf(sel)}
				if c.bindDeclared(stack) {
					configs = append(configs, c)
				}
			}
		}
		return true
	})
	return configs
}

// contribPkgOf returns the name of the package of the type expression pkg.Type.
func contribPkgOf(typ ast.Expr) string {
	for {
		star, ok := typ.(*ast.StarExpr)
		if !ok {
			break
		}
		typ = star.X
	}
	if sel, ok := typ.(*ast.SelectorExpr); ok {
		if id, ok := sel.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

// bind looks for the variable the literal is assigned to and collects the
// assignments to its fields that follow in the same block.
func (c *contribConfig) bind(lit *ast.CompositeLit, parents []ast.Node) {
	var expr ast.Expr = lit
	if len(parents) > 0 {
		if u, ok := parents[len(parents)-1].(*ast.UnaryExpr); ok && u.Op == token.AND {
			expr, parents = u, parents[:len(parents)-1]
		}
	}

	// declAt is the index of the statement that declares the variable.
	var decl ast.Stmt
	declAt := -1
	if len(parents) > 0 {
		switch p := parents[len(parents)-1].(type) {
		case *ast.AssignStmt:
			if i := slices.Index(p.Rhs, expr); i >= 0 && len(p.Lhs) == len(p.Rhs) {
				if id, ok := p.Lhs[i].(*ast.Ident); ok {
					c.varName, decl, declAt = id.Name, p, len(parents)-1
				}
			}
		case *ast.ValueSpec:
			i := slices.Index(p.Values, expr)
			if i >= 0 && i < len(p.Names) && len(parents) >= 3 {
				if ds, ok := parents[len(parents)-3].(*ast.DeclStmt); ok {
					c.varName, decl, declAt = p.Names[i].Name, ds, len(parents)-3
				}
			}
		}
	}
	if decl == nil || declAt < 1 {
		return
	}
	c.collect(decl, parents[declAt-1])
}

// bindDeclared binds the variable of a declaration without a value, whose
// ancestors are the declaration statement and the block it is in.
func (c *contribConfig) bindDeclared(parents []ast.Node) bool {
	if len(parents) < 3 {
		return false
	}
	decl, ok := parents[len(parents)-2].(*ast.DeclStmt)
	if !ok {
		return false
	}
	c.collect(decl, parents[len(parents)-3])
	return true
}

// collect adds the assignments to fields of the variable that come after its
// declaration in the statements of owner, the block the declaration is in.
func (c *contribConfig) collect(decl ast.Stmt, owner ast.Node) {
	c.decl = decl

	var list []ast.Stmt
	switch b := owner.(type) {
	case *ast.BlockStmt:
		list = b.List
	case *ast.CaseClause:
		list = b.Body
	case *ast.CommClause:
		list = b.Body
	}
	for _, stmt := range list {
		if stmt.Pos() >= decl.End() {
			c.collectAssignments(stmt)
		}
	}
}

// collectAssignments adds the assignments to fields of the bound variable that
// stmt contains. The statement itself runs whenever the block does, whatever is
// nested in it does not.
func (c *contribConfig) collectAssignments(stmt ast.Stmt) {
	ast.Inspect(stmt, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || as.Tok != token.ASSIGN || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
			return true
		}
		sel, ok := as.Lhs[0].(*ast.SelectorExpr)
		if !ok || !isIdentNamed(sel.X, c.varName) {
			return true
		}
		c.slots = append(c.slots, &contribSlot{field: sel.Sel.Name, value: as.Rhs[0], assign: as, index: -1, direct: ast.Stmt(as) == stmt})
		return true
	})
}

// slotsOf returns the slots that set field, in source order.
func (c *contribConfig) slotsOf(field string) []*contribSlot {
	var slots []*contribSlot
	for _, s := range c.slots {
		if s.field == field {
			slots = append(slots, s)
		}
	}
	slices.SortFunc(slots, func(a, b *contribSlot) int { return cmp.Compare(a.pos(), b.pos()) })
	return slots
}

// get returns the slot that decides the value of field, nil when it is not set.
func (c *contribConfig) get(field string) *contribSlot {
	slots := c.slotsOf(field)
	if len(slots) == 0 {
		return nil
	}
	return slots[len(slots)-1]
}

// settled reports whether every slot of the fields runs unconditionally.
func (c *contribConfig) settled(fields ...string) bool {
	for _, s := range c.slots {
		if slices.Contains(fields, s.field) && !s.direct {
			return false
		}
	}
	return true
}

// remove deletes every slot of field.
func (c *contribConfig) remove(field string) {
	for _, s := range c.slotsOf(field) {
		c.removeSlot(s)
	}
}

func (c *contribConfig) removeSlot(s *contribSlot) {
	if s.assign != nil {
		contribRemoveStmt(c.f.g, &c.f.edits, s.assign)
		return
	}
	c.removed = append(c.removed, s.index)
}

// replace sets newField to value where the last slot of field is, and deletes
// the other slots of field.
func (c *contribConfig) replace(field, newField, value string) {
	slots := c.slotsOf(field)
	for i, s := range slots {
		switch {
		case i < len(slots)-1:
			c.removeSlot(s)
		case s.assign != nil:
			c.f.edits.replaceNode(c.f.g, s.assign, c.varName+"."+newField+" = "+value)
		default:
			c.f.edits.replaceNode(c.f.g, c.lit.Elts[s.index], newField+": "+value)
		}
	}
}

// rename gives every slot of field the name newField.
func (c *contribConfig) rename(field, newField string) {
	for _, s := range c.slotsOf(field) {
		if s.assign != nil {
			if sel, ok := s.assign.Lhs[0].(*ast.SelectorExpr); ok {
				c.f.edits.replaceNode(c.f.g, sel.Sel, newField)
			}
			continue
		}
		if kv, ok := c.lit.Elts[s.index].(*ast.KeyValueExpr); ok {
			c.f.edits.replaceNode(c.f.g, kv.Key, newField)
		}
	}
}

// add appends newField: value to the literal when finish is called.
func (c *contribConfig) add(newField, value string) {
	c.added = append(c.added, newField+": "+value)
}

// finish writes what was added and removed. A new element takes the place of a
// removed one when there is one, and removals of literal elements are combined
// so that neighbors go together with their commas.
func (c *contribConfig) finish() {
	for _, text := range c.added {
		if c.lit == nil {
			// A variable without a literal gets a statement after its declaration.
			at := c.f.g.offset(c.decl.End())
			indent := contribLineIndent(c.f.g.src, c.f.g.offset(c.decl.Pos()))
			c.f.edits.replace(at, at, "\n"+indent+c.varName+"."+strings.Replace(text, ": ", " = ", 1))
			continue
		}
		if len(c.removed) > 0 {
			c.f.edits.replaceNode(c.f.g, c.lit.Elts[c.removed[0]], text)
			c.removed = c.removed[1:]
			continue
		}
		contribAppendElement(c.f.g, &c.f.edits, c.lit, text)
	}
	c.added = nil

	if len(c.removed) > 0 {
		contribRemoveElements(c.f.g, &c.f.edits, c.lit, c.removed...)
		c.removed = nil
	}
}

// contribSelectorBases returns the identifiers that are the left side of a
// selector somewhere in the file: the names a package is used by.
func contribSelectorBases(g *goSource) map[string]bool {
	used := map[string]bool{}
	ast.Inspect(g.file, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok {
				used[id.Name] = true
			}
		}
		return true
	})
	return used
}

// contribDropOrphans removes the imports of the contrib module short that the
// rewrite left unused: the names the module was used by in before and that
// out does not use any more. goimports would do the same, but not on a machine
// whose module cache is empty, where it keeps every import of a file that
// refers to a name it cannot place (a variable of another file of the package).
func contribDropOrphans(before *goSource, out, short string) string {
	after, ok := parseSource(out)
	if !ok {
		return out
	}
	usedBefore, usedAfter := contribSelectorBases(before), contribSelectorBases(after)

	var edits editSet
	for _, imp := range contribImportsOf(after, short) {
		if usedBefore[imp.name] && !usedAfter[imp.name] {
			contribRemoveImport(after, &edits, imp.spec)
		}
	}
	return edits.apply(out)
}

// contribRemoveImport schedules the removal of an import. The declaration goes
// with it when it was the only one.
func contribRemoveImport(g *goSource, edits *editSet, spec *ast.ImportSpec) {
	for _, d := range g.file.Decls {
		decl, ok := d.(*ast.GenDecl)
		if !ok || decl.Tok != token.IMPORT || !slices.Contains(decl.Specs, ast.Spec(spec)) {
			continue
		}

		var n ast.Node = spec
		if len(decl.Specs) == 1 {
			n = decl
		}
		start, end := g.offset(n.Pos()), g.offset(n.End())
		if contribOwnsLine(g.src, start, end) {
			edits.replace(contribLineStart(g.src, start), contribLineEnd(g.src, end), "")
		} else {
			edits.replace(start, end, "")
		}
		return
	}
}
