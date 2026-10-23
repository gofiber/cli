package v3

import (
	"go/ast"
	"strings"
)

// appSiteAt is a node of a file of the project.
type appSiteAt struct {
	file *appFile
	node ast.Node
}

// appSites indexes the calls, selectors and composite literals of the project by
// name, so that following a config does not read every file again for each
// function and field it meets.
type appSites struct {
	calls     map[string][]appSiteAt // *ast.CallExpr, by the name of what is called
	selectors map[string][]appSiteAt // *ast.SelectorExpr, by the name selected
	literals  map[string][]appSiteAt // *ast.CompositeLit, by the name of its type
	// ifaces are the signatures of the methods interfaces declare, by name.
	ifaces map[string][]appSiteAt // *ast.FuncType
}

// sites returns the index of the project, built when it is first asked for.
func (pl *appPlanner) sites() *appSites {
	if pl.siteIndex != nil {
		return pl.siteIndex
	}

	s := &appSites{
		calls:     map[string][]appSiteAt{},
		selectors: map[string][]appSiteAt{},
		literals:  map[string][]appSiteAt{},
		ifaces:    map[string][]appSiteAt{},
	}
	for _, f := range pl.ix.moduleFiles() {
		if isShimFile(f.path, f.g.src) {
			continue
		}
		ast.Inspect(f.g.file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				if name := appCalledName(x.Fun); name != "" {
					s.calls[name] = append(s.calls[name], appSiteAt{f, x})
				}
			case *ast.SelectorExpr:
				s.selectors[x.Sel.Name] = append(s.selectors[x.Sel.Name], appSiteAt{f, x})
			case *ast.CompositeLit:
				if x.Type != nil {
					if name := appBaseTypeName(appSelectorTail(x.Type)); name != "" {
						s.literals[name] = append(s.literals[name], appSiteAt{f, x})
					}
				}
			case *ast.InterfaceType:
				for _, m := range x.Methods.List {
					if ft, ok := m.Type.(*ast.FuncType); ok {
						for _, id := range m.Names {
							s.ifaces[id.Name] = append(s.ifaces[id.Name], appSiteAt{f, ft})
						}
					}
				}
			}
			return true
		})
	}
	pl.siteIndex = s
	return s
}

// appCalledName returns the name of the function or method an expression that is
// called names.
func appCalledName(fun ast.Expr) string {
	switch x := ast.Unparen(fun).(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return x.Sel.Name
	case *ast.IndexExpr:
		return appCalledName(x.X) // an instantiated generic function
	case *ast.IndexListExpr:
		return appCalledName(x.X)
	}
	return ""
}

// appFlowCall is a call of a function of the project.
type appFlowCall struct {
	file *appFile
	call *ast.CallExpr
}

// appFlowRef is a use of a function of the project that is not a call: the
// function is a value.
type appFlowRef struct {
	file *appFile
	expr ast.Expr
}

// callersOf finds the calls of the function declaration fn, and the places that
// use it as a value. A call that may be one of them and cannot be traced fails the
// set.
func (fl *appFlow) callersOf(fnFile *appFile, fn *ast.FuncDecl) (calls []appFlowCall, refs []appFlowRef) {
	if fl.why != "" {
		return nil, nil
	}

	name := fn.Name.Name
	sites := fl.pl.sites()
	pkg := fnFile.ix.pkgOf(fnFile)
	label := fnFile.funcLabel(fn)

	if fn.Recv != nil {
		for _, s := range sites.ifaces[name] {
			ft, ok := s.node.(*ast.FuncType)
			if ok && s.file.holdsConfig(ft) {
				fl.failf(s.file, ft, "%s is also a method of an interface, whose callers the migration cannot follow", label)
				return nil, nil
			}
		}
	}

	for _, s := range sites.calls[name] {
		call, ok := s.node.(*ast.CallExpr)
		if !ok {
			continue
		}
		callee, found := s.file.calleeOf(call, 0)
		switch {
		case found && callee.decl == fn:
			calls = append(calls, appFlowCall{file: s.file, call: call})
		case found:
			// a function of the same name that is not it
		case fn.Recv == nil:
			// a function is found wherever the project declares it
		case s.file.foreignReceiver(call):
		case !appIsExported(name) && pkg != nil && s.file.ix.pkgOf(s.file) == pkg && pkg.methodOwners(name) == 1:
			// only the methods of the package can have the name
			calls = append(calls, appFlowCall{file: s.file, call: call})
		default:
			fl.failf(s.file, call, "the call of %s may be a call of %s, but the type of its receiver could not be traced", s.file.g.text(call.Fun), label)
			return nil, nil
		}
	}

	for _, s := range sites.selectors[name] {
		if sel, ok := s.node.(*ast.SelectorExpr); ok && s.file.referencesFunc(sel, fn) {
			refs = append(refs, appFlowRef{file: s.file, expr: sel})
		}
	}
	if fn.Recv == nil && pkg != nil {
		for _, pf := range pkg.files {
			ast.Inspect(pf.g.file, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && id.Name == name && pf.referencesFunc(id, fn) {
					refs = append(refs, appFlowRef{file: pf, expr: id})
				}
				return true
			})
		}
	}
	return calls, refs
}

// holdsConfig reports whether a parameter or result of the signature is a
// fiber.Config.
func (f *appFile) holdsConfig(ft *ast.FuncType) bool {
	for _, list := range []*ast.FieldList{ft.Params, ft.Results} {
		if list == nil {
			continue
		}
		for _, fld := range list.List {
			if f.typeKind(fld.Type, 0) == appKindConfig {
				return true
			}
		}
	}
	return false
}

// methodOwners counts the types of the package that have a method called name.
func (p *appPackage) methodOwners(name string) int {
	n := 0
	for key := range p.methods {
		if strings.HasSuffix(key, "."+name) {
			n++
		}
	}
	return n
}
