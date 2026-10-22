package v3

import (
	"go/ast"
	"strings"
)

// The migration reads one package at a time. What a package exports reaches code
// that it does not read: the agent an exported function returns, the client an
// exported variable or field holds. That code calls the methods of the Fiber v2
// agent or client on the value (Set, Bytes, Get(url).String(), ...), the types of
// the client package do not have them, and nothing in the files it is in tells the
// migration to look at them: the build fails and nothing says why. The
// declaration of such a value says it.

// noteExports reports the exported declarations whose agent or client moved to the
// types of the client package of Fiber v3. A value that stays on the copy of the v2
// agent keeps its methods, so it needs no note.
func (f *clientFile) noteExports() {
	pkg := f.pkgName()
	if f.dry || pkg == "main" || strings.HasSuffix(pkg, "_test") {
		// Nothing imports a main package or an external test package.
		return
	}

	for _, d := range f.g.file.Decls {
		switch decl := d.(type) {
		case *ast.FuncDecl:
			f.noteFuncExport(decl)
		case *ast.GenDecl:
			f.noteGenExport(decl)
		}
	}
}

// noteFuncExport reports an exported function or method that returns an agent or a
// client.
func (f *clientFile) noteFuncExport(decl *ast.FuncDecl) {
	if !decl.Name.IsExported() || decl.Type.Results == nil || (decl.Recv != nil && !clientExportedRecv(decl.Recv)) {
		return
	}

	// The agent a function returns is only followed (and moved) for the functions
	// of the package with a single result. Every other result stays on the copy.
	if decl.Recv == nil && !f.copyAgents && clientReturnsAgent(decl.Type, f.fiber) {
		f.noteExport(decl.Name, decl.Name.Name, "client.Request")
	}
	for _, field := range decl.Type.Results.List {
		if f.typeKind(field.Type) == clientDomainClient && !f.copyClients {
			f.noteExport(decl.Name, decl.Name.Name, "client.Client")
		}
	}
}

// noteGenExport reports the exported variables, and the exported fields of exported
// types, that hold a client.
func (f *clientFile) noteGenExport(decl *ast.GenDecl) {
	if f.copyClients {
		return
	}

	for _, spec := range decl.Specs {
		switch s := spec.(type) {
		case *ast.ValueSpec:
			for i, id := range s.Names {
				holds := s.Type != nil && f.typeKind(s.Type) == clientDomainClient
				holds = holds || (s.Type == nil && i < len(s.Values) && clientIsInit(s.Values[i], f.fiber))
				if id.IsExported() && holds {
					f.noteExport(id, id.Name, "client.Client")
				}
			}
		case *ast.TypeSpec:
			st, ok := s.Type.(*ast.StructType)
			if !ok || !s.Name.IsExported() {
				continue
			}
			for _, field := range st.Fields.List {
				if f.typeKind(field.Type) != clientDomainClient {
					continue
				}
				for _, id := range field.Names {
					if id.IsExported() {
						f.noteExport(id, s.Name.Name+"."+id.Name, "client.Client")
					}
				}
			}
		}
	}
}

// noteExport reports one exported declaration. typ is the type the value has now.
func (f *clientFile) noteExport(at *ast.Ident, name, typ string) {
	f.notef(f.line(at), "%s is exported and is now a *%s: code in other packages that calls the methods of the Fiber v2 agent or client on it "+
		"(Set, Bytes, String, Get(url).Bytes(), ...) was not migrated, because the migration reads one package at a time", name, typ)
}

// typeKind says which type of the v2 client an expression names: the agent
// (*fiber.Agent) or the client (*fiber.Client, fiber.Client). It is zero for any
// other type.
func (f *clientFile) typeKind(t ast.Expr) clientShimDomain {
	switch x := t.(type) {
	case *ast.ParenExpr:
		return f.typeKind(x.X)
	case *ast.StarExpr:
		if sel, ok := f.fiberSel(x.X); ok {
			switch sel.Sel.Name {
			case nameAgent:
				return clientDomainAgent
			case nameClient:
				return clientDomainClient
			}
		}
	case *ast.SelectorExpr:
		if sel, ok := f.fiberSel(x); ok && sel.Sel.Name == nameClient {
			return clientDomainClient
		}
	}
	return 0
}

// clientExportedRecv reports whether the receiver of a method is a type that other
// packages can name.
func clientExportedRecv(recv *ast.FieldList) bool {
	if len(recv.List) != 1 {
		return false
	}
	t := recv.List[0].Type
	for {
		switch x := t.(type) {
		case *ast.StarExpr:
			t = x.X
		case *ast.ParenExpr:
			t = x.X
		case *ast.IndexExpr:
			t = x.X
		case *ast.IndexListExpr:
			t = x.X
		case *ast.Ident:
			return x.IsExported()
		default:
			return false
		}
	}
}
