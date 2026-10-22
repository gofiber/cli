package v3

import (
	"go/ast"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gofiber/cli/cmd/internal"
)

// appDeadFunc returns the name of the function that creates the app c makes when
// that function can never run: it is unexported and nothing in its package
// refers to it, and nothing outside the package can.
func appDeadFunc(c appNew) (name string, ok bool) {
	f := c.file
	decl := f.enclosingDecl(c.call)
	if decl == nil || decl.Body == nil || !f.walked {
		return "", false
	}

	name = decl.Name.Name
	if appIsExported(name) || name == appMainPackage || name == "init" || name == "_" {
		return "", false
	}
	pkg := f.ix.pkgOf(f)
	if pkg == nil || pkg.hasUnreadFile() || pkg.refersTo(name, decl) {
		return "", false
	}
	return name, true
}

// refersTo reports whether any file of the package uses the name outside of the
// function declaration decl: a call, a method value, a go:linkname.
func (p *appPackage) refersTo(name string, decl *ast.FuncDecl) bool {
	for _, pf := range p.files {
		found := false
		ast.Inspect(pf.g.file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.FuncDecl:
				return x != decl // a function that only calls itself is no more alive
			case *ast.Ident:
				found = found || x.Name == name
			}
			return !found
		})
		if found {
			return true
		}
		for _, group := range pf.g.file.Comments {
			for _, c := range group.List {
				if strings.HasPrefix(c.Text, "//go:linkname") && strings.Contains(c.Text, name) {
					return true
				}
			}
		}
	}
	return false
}

// hasUnreadFile reports whether the directory of the package holds a Go file that
// cannot be parsed: it may use anything.
func (p *appPackage) hasUnreadFile() bool {
	entries, err := os.ReadDir(p.dir)
	if err != nil {
		return true
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		if len(p.files) == 0 || p.files[0].ix.fileAt(filepath.Join(p.dir, e.Name())) == nil {
			return true
		}
	}
	return false
}

// droppableValue reports whether the value of a setting can be removed together
// with it: it uses no variable that only it keeps used.
func (src *appListenSource) droppableValue(e ast.Expr) bool {
	needs := src.file.valueNeeds(e)
	return !needs.unknown && !slices.ContainsFunc(needs.locals, func(b appBinding) bool { return !b.isParam() })
}

// removable reports whether the setting can be taken out of the source, wherever
// it is set.
func (src *appListenSource) removable(fld *appSourceField) bool {
	for _, occ := range fld.occs {
		if occ.file != src.file {
			return false
		}
		switch {
		case occ.elt >= 0:
			kv, ok := occ.node.(*ast.KeyValueExpr)
			if !ok || !src.droppableValue(kv.Value) {
				return false
			}
		case occ.assign == nil || !src.droppableValue(occ.assign.Rhs[0]):
			return false
		}
	}
	return len(fld.occs) > 0
}

// dropDead plans the removal of the settings of a source whose apps are created
// by functions that never run, so that nothing can start them and the settings
// cannot take effect. It returns the settings it removes, and nothing when the
// source is not made of such apps only.
func (pl *appPlanner) dropDead(src *appListenSource) (commit func(), dropped []*appSourceField) {
	if len(src.creations) == 0 || src.escapes || src.retFn != nil || !src.file.walked || (src.node != nil && src.scope == nil) {
		return nil, nil
	}

	var funcs []string
	for _, c := range src.creations {
		name, ok := appDeadFunc(c)
		if !ok {
			return nil, nil
		}
		if !slices.Contains(funcs, name) {
			funcs = append(funcs, name)
		}
	}

	for _, fld := range src.fields {
		if src.removable(fld) {
			dropped = append(dropped, fld)
		}
	}
	if len(dropped) == 0 {
		return nil, nil
	}
	var removal editSet
	if src.planRemoval(dropped, &removal) != "" {
		return nil, nil
	}

	names := make([]string, len(dropped))
	for i, fld := range dropped {
		names[i] = fld.v2
	}
	first := dropped[0].occs[0]
	return func() {
		fp := pl.plan.fileOf(src.file)
		fp.edits.edits = append(fp.edits.edits, removal.edits...)
		for _, fld := range dropped {
			fld.dropped = true
		}
		internal.Notef(first.file.fi, first.file.g.line(first.node.Pos()),
			"fiber.Config has no %s in v3: dropped, because the app this config makes is not started anywhere in the package (%s is unexported and nothing calls it); "+
				"set %s in the fiber.ListenConfig of the call that starts the app if you start it later",
			appFieldList(names), strings.Join(funcs, ", "), appThem(len(names)))
	}, dropped
}

// appThem is "it" for one setting and "them" for more.
func appThem(n int) string {
	if n == 1 {
		return "it"
	}
	return "them"
}

// appFieldList names settings for a sentence: "Network field", "Prefork and
// Network fields".
func appFieldList(names []string) string {
	switch len(names) {
	case 0:
		return "fields"
	case 1:
		return names[0] + " field"
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1] + " fields"
}
