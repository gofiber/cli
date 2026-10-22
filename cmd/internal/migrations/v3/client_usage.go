package v3

import (
	"fmt"
	"go/ast"
	"go/token"
	"regexp"
	"slices"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// MigrateClientUsage moves the v2 HTTP client of the fiber package (fiber.Get,
// fiber.Post, fiber.AcquireAgent, fiber.Client, ...) to the client package of
// Fiber v3.
//
// It understands whole statements. A request that is built and sent by
// statements it recognizes is rewritten. Every other request keeps running on
// a copy of the v2 agent, which is added to the package, so that no setting of a
// request is ever dropped silently; where the package cannot have the copy the
// request is left as it is and reported.
func MigrateClientUsage(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	pkgs := clientNewPackages()

	// The first pass changes nothing. It finds out which packages hand agents and
	// clients from one function to another in ways the migration does not follow:
	// the types of those cannot be the ones of the client package for some of the
	// code and the copy's for the rest.
	if _, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		clientMigrateContent(content, fi, pkgs, true)
		return content
	}); err != nil {
		return fmt.Errorf("failed to scan client usage: %w", err)
	}

	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return clientMigrateContent(content, fi, pkgs, false)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate client usage: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating client usage")
	return nil
}

// clientErrorsPath and clientLogPaths match the packages whose functions only
// format the errors they are given.
var (
	clientReErrors = regexp.MustCompile(`^errors$`)
	clientReFmt    = regexp.MustCompile(`^fmt$`)
	clientReLog    = regexp.MustCompile(`^log$`)
)

// clientFile is the file being migrated.
type clientFile struct {
	g        *goSource
	pkgs     *clientPackages
	parents  map[ast.Node]ast.Node
	pkgNames map[string]string
	idents   map[string]bool
	handled  map[ast.Node]bool
	reported map[ast.Node]bool
	intro    map[ast.Node]map[string]bool
	errsGone map[ast.Node]map[string]bool
	// copied are the fiber members of the requests that stay on the copy of the v2
	// agent.
	copied map[*ast.SelectorExpr]bool
	// helperCalls are the calls of functions that return an agent, which a
	// request was made of.
	helperCalls map[*ast.CallExpr]bool
	// viaClient are the calls on a fiber.Client that a request was made of.
	viaClient map[*ast.SelectorExpr]bool
	shim      clientShimState
	fi        internal.FileInfo
	fiber     []string
	errors    []string
	fmt       []string
	log       []string
	edits     clientEdits
	// dry says the file is only scanned: nothing is reported or written.
	dry bool
	// shimNoted says a request of the file was reported as running on the copy.
	shimNoted bool
	// copyAgents and copyClients say that the agents or the clients of the
	// package are the copy's.
	copyAgents  bool
	copyClients bool
}

func clientMigrateContent(content string, fi internal.FileInfo, pkgs *clientPackages, dry bool) string {
	names := fiberNames(content)
	names = slices.DeleteFunc(names, func(n string) bool { return n == "." })
	if len(names) == 0 {
		return content
	}
	var g *goSource
	var ok bool
	if dry {
		g, ok = parseSource(content)
	} else {
		g, ok = parseFile(fi, content)
	}
	if !ok {
		return content
	}

	f := &clientFile{
		g:           g,
		pkgs:        pkgs,
		fi:          fi,
		fiber:       names,
		dry:         dry,
		errors:      importNames(content, clientReErrors, "errors"),
		fmt:         importNames(content, clientReFmt, "fmt"),
		log:         importNames(content, clientReLog, "log"),
		pkgNames:    map[string]string{},
		handled:     map[ast.Node]bool{},
		reported:    map[ast.Node]bool{},
		intro:       map[ast.Node]map[string]bool{},
		errsGone:    map[ast.Node]map[string]bool{},
		copied:      map[*ast.SelectorExpr]bool{},
		helperCalls: map[*ast.CallExpr]bool{},
		viaClient:   map[*ast.SelectorExpr]bool{},
	}
	f.run()
	if dry {
		return content
	}
	return f.finish(content)
}

// run migrates the requests of the file.
func (f *clientFile) run() {
	if f.dry {
		// The files of the package that do not import fiber are read with it.
		f.pkgs.get(f.dir(), f.pkgName())
	}
	f.copyAgents = f.shimmed(clientDomainAgent)
	f.copyClients = f.shimmed(clientDomainClient)
	f.renameAliases()
	f.rewriteClients()
	for _, decl := range f.g.file.Decls {
		f.walkDecl(decl, clientNewFunc(decl))
	}
	f.leftovers()
	if f.dry {
		f.scanReferences()
	}
}

// finish applies the edits and fixes the imports.
func (f *clientFile) finish(content string) string {
	if len(f.edits.list) == 0 {
		return content
	}
	out, ok := f.edits.apply(content)
	if !ok {
		f.notef(0, "client calls left unchanged: two rewrites touch the same code (please report this)")
		return content
	}
	return f.fixImports(out)
}

// walkDecl visits the statement lists of a top level declaration, inner lists
// before the lists that contain them.
func (f *clientFile) walkDecl(decl ast.Decl, top *clientFunc) {
	var stack []ast.Node
	fns := []*clientFunc{top}

	ast.Inspect(decl, func(n ast.Node) bool {
		if n == nil {
			node := stack[len(stack)-1]
			f.leave(node, stack[:len(stack)-1], fns[len(fns)-1])
			stack = stack[:len(stack)-1]
			switch node.(type) {
			case *ast.FuncDecl, *ast.FuncLit:
				fns = fns[:len(fns)-1]
			}
			return true
		}
		switch x := n.(type) {
		case *ast.FuncDecl:
			fns = append(fns, top.forFunction(x.Type, x.Body))
		case *ast.FuncLit:
			fns = append(fns, top.forFunction(x.Type, x.Body))
		}
		stack = append(stack, n)
		return true
	})
}

// leave is called when the traversal leaves node, whose ancestors are parents.
func (f *clientFile) leave(node ast.Node, parents []ast.Node, fn *clientFunc) {
	var parent ast.Node
	if len(parents) > 0 {
		parent = parents[len(parents)-1]
	}
	l := clientList{owner: node, fn: fn}
	switch x := node.(type) {
	case *ast.BlockStmt:
		l.stmts = x.List
		switch p := parent.(type) {
		case *ast.FuncDecl:
			l.bound = clientFuncNames(p.Recv, p.Type)
			f.agentParams(p.Type, l)
		case *ast.FuncLit:
			l.bound = clientFuncNames(nil, p.Type)
			f.agentParams(p.Type, l)
		}
	case *ast.CaseClause:
		l.stmts = x.Body
		if len(parents) >= 2 {
			if ts, ok := parents[len(parents)-2].(*ast.TypeSwitchStmt); ok {
				l.bound = clientDefinedNames(ts.Assign)
			}
		}
	case *ast.CommClause:
		l.stmts = x.Body
		if x.Comm != nil {
			l.bound = clientDefinedNames(x.Comm)
		}
	default:
		return
	}
	f.processList(l)
}

// clientFuncNames returns the names a function binds in the scope of its body.
func clientFuncNames(recv *ast.FieldList, ft *ast.FuncType) []string {
	var names []string
	add := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, field := range fl.List {
			for _, id := range field.Names {
				names = append(names, id.Name)
			}
		}
	}
	add(recv)
	if ft != nil {
		add(ft.Params)
		add(ft.Results)
	}
	return names
}

// processList migrates the requests that start in a statement list.
func (f *clientFile) processList(l clientList) {
	for idx, s := range l.stmts {
		if !f.copyClients && f.clientStatement(s) {
			continue
		}
		if ret, ok := s.(*ast.ReturnStmt); ok {
			f.returnedAgents(l, ret)
			continue
		}
		u := f.creationAt(l, idx)
		if u == nil {
			u = f.agentFromHelper(l, idx)
		}
		if u != nil {
			f.migrate(u)
		}
	}
}

// clientStatement handles the statements about fiber.Client values: releasing
// one is not needed any more, and its fields have setters now.
func (f *clientFile) clientStatement(s ast.Stmt) bool {
	switch x := s.(type) {
	case *ast.ExprStmt:
		return f.releasedClient(s, x.X)
	case *ast.DeferStmt:
		return f.releasedClient(s, x.Call)
	case *ast.AssignStmt:
		return f.clientFieldAssign(x)
	}
	return false
}

// releasedClient removes fiber.ReleaseClient(c).
func (f *clientFile) releasedClient(s ast.Stmt, e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := f.fiberSel(call.Fun)
	if !ok || sel.Sel.Name != nameReleaseClient || len(call.Args) != 1 {
		return false
	}
	f.deleteStmt(f.edits.begin(), s)
	f.handled[sel] = true
	return true
}

// clientFieldAssign rewrites c.UserAgent = "x" on a fiber.Client to its setter.
func (f *clientFile) clientFieldAssign(s *ast.AssignStmt) bool {
	if len(s.Lhs) != 1 || len(s.Rhs) != 1 || s.Tok != token.ASSIGN {
		return false
	}
	sel, ok := s.Lhs[0].(*ast.SelectorExpr)
	if !ok {
		return false
	}
	setter, known := clientFieldSetters[sel.Sel.Name]
	if !known || !f.isFiberClientExpr(sel.X) {
		return false
	}
	f.replaceStmt(f.edits.begin(), s, f.text(sel.X)+"."+setter+"("+f.text(s.Rhs[0])+")")
	return true
}

// text returns the source text of n, with the edits inside it applied.
func (f *clientFile) text(n ast.Node) string {
	return f.edits.textOf(f.g.src, f.g.offset(n.Pos()), f.g.offset(n.End()))
}

// line returns the line of n.
func (f *clientFile) line(n ast.Node) int {
	return f.g.line(n.Pos())
}

// notef reports something the user has to look at.
func (f *clientFile) notef(line int, format string, args ...any) {
	if f.dry {
		return
	}
	internal.Notef(f.fi, line, format, args...)
}

// isFiberName reports whether e is the identifier of the fiber package: it has
// the name the file imports the package under and no variable, parameter or
// declaration of the file hides the package.
func (f *clientFile) isFiberName(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	if !ok || !slices.Contains(f.fiber, id.Name) {
		return false
	}
	_, hidden := f.resolve(id)
	return !hidden
}

// fiberSel returns the selector if e is fiber.<name>.
func (f *clientFile) fiberSel(e ast.Expr) (*ast.SelectorExpr, bool) {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || !f.isFiberName(sel.X) {
		return nil, false
	}
	return sel, true
}

// ancestors returns the ancestors of n, outermost first.
func (f *clientFile) ancestors(n ast.Node) []ast.Node {
	if f.parents == nil {
		f.parents = map[ast.Node]ast.Node{}
		var stack []ast.Node
		ast.Inspect(f.g.file, func(c ast.Node) bool {
			if c == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			if len(stack) > 0 {
				f.parents[c] = stack[len(stack)-1]
			}
			stack = append(stack, c)
			return true
		})
	}
	var chain []ast.Node
	for p := f.parents[n]; p != nil; p = f.parents[p] {
		chain = append(chain, p)
	}
	slices.Reverse(chain)
	return chain
}

// resolve finds the declaration of the variable id refers to.
func (f *clientFile) resolve(id *ast.Ident) (decl, bool) {
	return resolveIdent(id.Name, id.Pos(), f.ancestors(id))
}

// clientAPIMembers lists the members of the fiber package that belong to the v2
// client.
var clientAPIMembers = []string{
	"Get", nameHead, namePost, namePut, namePatch, nameDelete,
	nameAcquireAgent, "ReleaseAgent", "AcquireClient", "ReleaseClient",
	clientMemberAcquireArgs, nameReleaseArgs, nameAcquireFormFile, "ReleaseFormFile",
	nameAcquireResponse, nameReleaseResponse,
	nameAgent, "Client", "Args", clientMemberFormFile, clientMethodRequest, nameResponse, "RetryIfFunc",
}
