package v3

import (
	"go/ast"
	"slices"
	"strings"
)

// The migration reads one package at a time, but a package hands its agents, its
// clients and its form files over to the packages that import it: a function that
// returns an agent, a variable that holds a client, a struct with a file in a
// field. The code that uses them calls the methods of the Fiber v2 agent on the
// value (Set, Bytes, Get(url).String(), ...), often in files that do not import
// fiber at all, which the migration does not rewrite. The types of the client
// package do not have those methods.
//
// So a package that other packages can import (not main, not an external test
// package) whose exported API uses one of the three types keeps all values of that
// kind on the copy of the Fiber v2 agent, which declares them under the same names
// and is shared by the packages of the module: the code that imports the package
// compiles as it did. A package that imports such a package does the same for the
// values it shares with it (see clientPackages.tainted).

// clientDomainSet is a set of the kinds of value of the v2 client.
type clientDomainSet uint8

// has reports whether d is in the set.
func (s clientDomainSet) has(d clientShimDomain) bool {
	return s&(1<<d) != 0
}

// with returns the set with d added.
func (s clientDomainSet) with(d clientShimDomain) clientDomainSet {
	return s | 1<<d
}

// clientKindWord names a kind of value in a note.
func clientKindWord(d clientShimDomain) string {
	switch d {
	case clientDomainClient:
		return "client"
	case clientDomainFile:
		return "form file"
	default:
		return "agent"
	}
}

// clientKindOrder lists the kinds in the order a note names them.
var clientKindOrder = []clientShimDomain{clientDomainAgent, clientDomainClient, clientDomainFile}

// clientExport is an exported declaration that uses a kind of the v2 client.
type clientExport struct {
	// decl is the function or the variable declaration: the requests inside it are
	// covered by the note.
	decl ast.Node
	// at is where the declaration is reported.
	at *ast.Ident
	// name is how the note calls the declaration: New, API.Client.
	name  string
	kinds clientDomainSet
}

// clientExporter collects the exports of a file.
type clientExporter struct {
	// fiber are the names the file gives the fiber package.
	fiber []string
	// agentFuncs are the functions of the package that return an agent.
	agentFuncs map[string]bool
	out        []clientExport
}

// clientFileExports returns the exported declarations of a file whose type is an
// agent, a client or a form file of the v2 client, or holds one: the parameters and
// results of functions and methods, variables, the exported fields of structs, the
// methods of interfaces and the types themselves. A declaration that is not
// exported cannot be reached from another package, except through the fields and
// the methods of a struct or an interface, which are looked at wherever the type is
// declared. agentFuncs are the functions of the package that return an agent.
func clientFileExports(file *ast.File, agentFuncs map[string]bool) []clientExport {
	names := clientFiberNames(file)
	if len(names) == 0 {
		return nil
	}

	x := &clientExporter{fiber: names, agentFuncs: agentFuncs}
	for _, d := range file.Decls {
		switch decl := d.(type) {
		case *ast.FuncDecl:
			x.funcDecl(decl)
		case *ast.GenDecl:
			x.genDecl(decl)
		}
	}
	return x.out
}

func (x *clientExporter) add(at *ast.Ident, name string, kinds clientDomainSet) {
	x.addDecl(at, name, nil, kinds)
}

func (x *clientExporter) addDecl(at *ast.Ident, name string, decl ast.Node, kinds clientDomainSet) {
	if kinds != 0 {
		x.out = append(x.out, clientExport{at: at, name: name, decl: decl, kinds: kinds})
	}
}

// funcDecl looks at the signature of an exported function or method. A method of a
// type that is not exported counts too: a value of the type can be reached through
// an exported variable or function, or an interface.
func (x *clientExporter) funcDecl(decl *ast.FuncDecl) {
	if !decl.Name.IsExported() {
		return
	}
	name := decl.Name.Name
	if decl.Recv != nil && len(decl.Recv.List) == 1 {
		if recv := clientRecvName(decl.Recv.List[0].Type); recv != "" {
			name = recv + "." + name
		}
	}
	x.addDecl(decl.Name, name, decl, clientMentions(decl.Type, x.fiber))
}

// clientRecvName returns the name of the type a method is declared on.
func clientRecvName(t ast.Expr) string {
	if id := clientTypeIdent(t); id != nil {
		return id.Name
	}
	return ""
}

// clientTypeIdent returns the identifier that names a type: the type of a receiver,
// or of an embedded field (the name of the field), whatever pointer, package name
// and type arguments are around it.
func clientTypeIdent(t ast.Expr) *ast.Ident {
	for {
		switch e := t.(type) {
		case *ast.StarExpr:
			t = e.X
		case *ast.ParenExpr:
			t = e.X
		case *ast.IndexExpr:
			t = e.X
		case *ast.IndexListExpr:
			t = e.X
		case *ast.SelectorExpr:
			return e.Sel
		case *ast.Ident:
			return e
		default:
			return nil
		}
	}
}

func (x *clientExporter) genDecl(decl *ast.GenDecl) {
	for _, spec := range decl.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			x.typeSpec(s)
		case *ast.ValueSpec:
			x.valueSpec(s)
		}
	}
}

// typeSpec looks at a type declaration. The fields of a struct and the methods of an
// interface are reachable whatever the type is called; any other type is reachable
// when its name is exported.
func (x *clientExporter) typeSpec(s *ast.TypeSpec) {
	switch t := s.Type.(type) {
	case *ast.StructType:
		for _, field := range t.Fields.List {
			kinds := clientMentions(field.Type, x.fiber)
			if len(field.Names) == 0 {
				// An embedded field is named after its type.
				if id := clientTypeIdent(field.Type); id != nil && id.IsExported() {
					x.add(id, s.Name.Name+"."+id.Name, kinds)
				}
				continue
			}
			for _, id := range field.Names {
				if id.IsExported() {
					x.add(id, s.Name.Name+"."+id.Name, kinds)
				}
			}
		}
	case *ast.InterfaceType:
		for _, method := range t.Methods.List {
			for _, id := range method.Names {
				if id.IsExported() {
					x.add(id, s.Name.Name+"."+id.Name, clientMentions(method.Type, x.fiber))
				}
			}
		}
	default:
		if s.Name.IsExported() {
			x.add(s.Name, s.Name.Name, clientMentions(s.Type, x.fiber))
		}
	}
}

// valueSpec looks at the exported variables of a declaration: the type they are
// declared with, and what they are set to.
func (x *clientExporter) valueSpec(s *ast.ValueSpec) {
	for i, id := range s.Names {
		if !id.IsExported() {
			continue
		}
		var kinds clientDomainSet
		if s.Type != nil {
			kinds |= clientMentions(s.Type, x.fiber)
		}
		if len(s.Values) == len(s.Names) {
			value := s.Values[i]
			kinds |= clientMentions(value, x.fiber) | clientConstructs(value, x.fiber)
			// var New = newRequest, where newRequest returns an agent.
			if ref, ok := value.(*ast.Ident); ok && x.agentFuncs[ref.Name] {
				kinds = kinds.with(clientDomainAgent)
			}
		}
		x.addDecl(id, id.Name, s, kinds)
	}
}

// clientMentions returns the kinds of the v2 client that the types in n name
// (fiber.Agent, fiber.Client, fiber.FormFile). The body of a function literal is not
// part of its type.
func clientMentions(n ast.Node, fiberNames []string) clientDomainSet {
	var kinds clientDomainSet
	ast.Inspect(n, func(c ast.Node) bool {
		switch x := c.(type) {
		case *ast.FuncLit:
			kinds |= clientMentions(x.Type, fiberNames)
			return false
		case *ast.SelectorExpr:
			id, ok := x.X.(*ast.Ident)
			if !ok || !slices.Contains(fiberNames, id.Name) {
				return true
			}
			switch x.Sel.Name {
			case nameAgent:
				kinds = kinds.with(clientDomainAgent)
			case nameClient:
				kinds = kinds.with(clientDomainClient)
			case clientMemberFormFile:
				kinds = kinds.with(clientDomainFile)
			}
		}
		return true
	})
	return kinds
}

// clientConstructs returns the kind of value an expression makes when it is a call of
// a function of the fiber package that makes one, possibly followed by calls of the
// methods of the agent that return it again: fiber.Get(url).Set(k, v).
func clientConstructs(e ast.Expr, fiberNames []string) clientDomainSet {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return clientConstructs(x.X, fiberNames)
	case *ast.CallExpr:
		sel, ok := x.Fun.(*ast.SelectorExpr)
		if !ok {
			return 0
		}
		if id, isIdent := sel.X.(*ast.Ident); isIdent && slices.Contains(fiberNames, id.Name) {
			switch sel.Sel.Name {
			case clientMethodGet, nameHead, namePost, namePut, namePatch, nameDelete, nameAcquireAgent:
				return clientDomainSet(0).with(clientDomainAgent)
			case nameAcquireClient:
				return clientDomainSet(0).with(clientDomainClient)
			case nameAcquireFormFile:
				return clientDomainSet(0).with(clientDomainFile)
			}
			return 0
		}
		if clientTerminalKind(sel.Sel.Name) != clientTermNone {
			return 0
		}
		return clientConstructs(sel.X, fiberNames) & (1 << clientDomainAgent)
	}
	return 0
}

// noteExports reports the exported declarations that use the v2 client and now use
// the copy of the Fiber v2 agent, with every other value of the kind of the package.
func (f *clientFile) noteExports() {
	pkg := f.pkgName()
	if f.dry || pkg == appMainPackage || strings.HasSuffix(pkg, "_test") {
		// Nothing imports a main package or an external test package.
		return
	}

	agentFuncs := f.pkgs.get(f.dir(), pkg).agentFuncs
	for _, e := range clientFileExports(f.g.file, agentFuncs) {
		if !f.shimOK() {
			return
		}
		f.shimNoted = true
		if e.decl != nil {
			f.exported = append(f.exported, e.decl)
		}
		f.notef(f.line(e.at), "%s is exported and uses the Fiber v2 %s: the %s of this package stay on the copy of the Fiber v2 agent in %s, "+
			"so that the packages that import this one keep compiling", e.name, clientKindWords(e.kinds, ""), clientKindWords(e.kinds, "s"), clientShimWhere)
	}
}

// clientKindWords names the kinds in the set: "agent", "agent and client".
func clientKindWords(kinds clientDomainSet, plural string) string {
	var words []string
	for _, d := range clientKindOrder {
		if kinds.has(d) {
			words = append(words, clientKindWord(d)+plural)
		}
	}
	return strings.Join(words, " and ")
}
