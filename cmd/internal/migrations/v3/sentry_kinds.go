package v3

import (
	"go/ast"
	"go/token"
	"regexp"
	"slices"
	"strconv"

	"github.com/gofiber/cli/cmd/internal"
)

// What a value of sentry-go is, as far as the declarations around it say: the
// migration of the sentry API rewrites a call only on a value it knows to be a
// scope, an event, a hub or a client. There is no type checker (the
// dependencies of the project are not downloaded when the migrations run), so
// the answer comes from the same places a reader looks at: the parameter of the
// callback of WithScope, the variable that holds the result of
// GetHubFromContext, the field of a struct, the function of another package that
// returns the hub.

var (
	// sentryRootPath is the import path of sentry-go.
	sentryRootPath = regexp.MustCompile(`^github\.com/getsentry/sentry-go$`)
	// sentryContribPath is the import path of the Fiber middleware for sentry,
	// before and after it moved below contrib/v3.
	sentryContribPath = reviewContribPath("sentry|fibersentry")
)

// sentryContribV3Import is the import path of the middleware for Fiber v3. The
// module of it requires sentry-go 0.50, so a module that imports it is on that
// version once the migration is done (see sentryFile.onCurrentSDK).
const sentryContribV3Import = contribV3Prefix + "sentry"

// sentryKind is what a value of sentry-go is.
type sentryKind uint8

const (
	sentryUnknown sentryKind = iota
	sentryScope
	sentryHub
	sentryEvent
	sentryClient
	sentryOptions
	sentryUser
	// sentryOther is a value that is known not to be one of those of sentry-go.
	sentryOther
)

// sentryTypeKinds are the types of sentry-go the migration looks for.
var sentryTypeKinds = map[string]sentryKind{
	"Scope":         sentryScope,
	"Hub":           sentryHub,
	"Event":         sentryEvent,
	nameClient:      sentryClient,
	"ClientOptions": sentryOptions,
	"User":          sentryUser,
}

// sentryFuncKinds are the functions of sentry-go that return one of the values,
// and sentryContribFuncKinds those of the Fiber middleware.
var (
	sentryFuncKinds = map[string]sentryKind{
		"CurrentHub":          sentryHub,
		"NewHub":              sentryHub,
		nameGetHubFromContext: sentryHub,
		"NewScope":            sentryScope,
		"PushScope":           sentryScope,
		"NewEvent":            sentryEvent,
	}
	sentryContribFuncKinds = map[string]sentryKind{
		nameGetHubFromContext:   sentryHub,
		"MustGetHubFromContext": sentryHub,
	}
)

// sentryMethodKinds are the methods of the values that return another value.
var sentryMethodKinds = map[sentryKind]map[string]sentryKind{
	sentryHub:    {"Scope": sentryScope, "PushScope": sentryScope, "Clone": sentryHub, nameClient: sentryClient},
	sentryScope:  {"Clone": sentryScope},
	sentryClient: {"EventFromMessage": sentryEvent, "EventFromException": sentryEvent},
}

// sentryMaxDepth bounds how far the declarations of variables are followed.
const sentryMaxDepth = 8

// sentryNames are the identifiers a file binds to the packages of sentry.
type sentryNames struct {
	// root are the identifiers of sentry-go, contrib those of the middleware.
	root, contrib []string
	// v3 is set when the file imports the middleware of Fiber v3.
	v3 bool
}

// sentryNamesOf reads the imports of a file.
func sentryNamesOf(file *ast.File) sentryNames {
	var n sentryNames
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil || (spec.Name != nil && (spec.Name.Name == "_" || spec.Name.Name == ".")) {
			continue
		}
		var bound []string
		if spec.Name != nil {
			bound = []string{spec.Name.Name}
		}

		switch {
		case sentryRootPath.MatchString(importPath):
			if bound == nil {
				bound = []string{"sentry"}
			}
			n.root = append(n.root, bound...)
		case sentryContribPath.MatchString(importPath):
			if bound == nil {
				// the package of the middleware is called fibersentry at v2 and sentry at v3
				bound = []string{"fibersentry", "sentry"}
			}
			n.contrib = append(n.contrib, bound...)
			n.v3 = n.v3 || importPath == sentryContribV3Import
		}
	}
	return n
}

// imported reports whether the file imports sentry-go or the middleware.
func (n sentryNames) imported() bool { return len(n.root) > 0 || len(n.contrib) > 0 }

// sentryFile answers the questions about the values of sentry-go in one file, and
// holds the rewrite of the file being migrated.
type sentryFile struct {
	run *sentryRun
	g   *goSource
	// mf is the file as the module index sees it, nil outside of a module.
	mf *modFile
	// done holds the nodes a rewrite has taken care of, so that the walk does not
	// look at them again.
	done   map[ast.Node]bool
	rep    *mw1Reporter
	places []sentryPlace
	edits  editSet
	fi     internal.FileInfo
	names  sentryNames
}

// isPackage reports whether the identifier is one of names and not a variable
// that has the same name.
func isPackageName(id *ast.Ident, names []string, stack []ast.Node) bool {
	if !slices.Contains(names, id.Name) {
		return false
	}
	if stack == nil {
		return true
	}
	_, hidden := resolveIdent(id.Name, id.Pos(), stack)
	return !hidden && !modDeclaredLocally(id.Name, id.Pos(), stack)
}

// isRoot reports whether the identifier names sentry-go here.
func (f *sentryFile) isRoot(id *ast.Ident, stack []ast.Node) bool {
	return isPackageName(id, f.names.root, stack)
}

// isContrib reports whether the identifier names the Fiber middleware here.
func (f *sentryFile) isContrib(id *ast.Ident, stack []ast.Node) bool {
	return isPackageName(id, f.names.contrib, stack)
}

// typeKind returns the kind of a value of the type, behind pointers.
func (f *sentryFile) typeKind(t ast.Expr, stack []ast.Node) sentryKind {
	for {
		switch x := t.(type) {
		case *ast.StarExpr:
			t = x.X
			continue
		case *ast.ParenExpr:
			t = x.X
			continue
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok && f.isRoot(id, stack) {
				return sentryTypeKinds[x.Sel.Name]
			}
		}
		return sentryUnknown
	}
}

// declaredKind is typeKind for a type that a declaration names: one that is not
// of sentry-go is another kind of value, not an unknown one.
func (f *sentryFile) declaredKind(t ast.Expr, stack []ast.Node) sentryKind {
	if k := f.typeKind(t, stack); k != sentryUnknown {
		return k
	}
	return sentryOther
}

// kindOf returns the kind of the value of e, where the ancestors of e are stack.
func (f *sentryFile) kindOf(e ast.Expr, stack []ast.Node) sentryKind {
	return f.kindAt(e, stack, 0)
}

func (f *sentryFile) kindAt(e ast.Expr, stack []ast.Node, depth int) sentryKind {
	if depth > sentryMaxDepth {
		return sentryUnknown
	}

	switch x := e.(type) {
	case *ast.ParenExpr:
		return f.kindAt(x.X, stack, depth+1)
	case *ast.StarExpr:
		return f.kindAt(x.X, stack, depth+1)
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			return f.kindAt(x.X, stack, depth+1)
		}
	case *ast.CompositeLit:
		if x.Type != nil {
			return f.declaredKind(x.Type, stack)
		}
	case *ast.TypeAssertExpr:
		if x.Type != nil {
			return f.declaredKind(x.Type, stack)
		}
	case *ast.CallExpr:
		return f.callKind(x, stack, depth)
	case *ast.Ident:
		return f.identKind(x, stack, depth)
	case *ast.SelectorExpr:
		return f.selectorKind(x, stack, depth)
	}
	return sentryUnknown
}

// callKind is the kind of what a call returns.
func (f *sentryFile) callKind(call *ast.CallExpr, stack []ast.Node, depth int) sentryKind {
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.SelectorExpr:
		if id, ok := fun.X.(*ast.Ident); ok {
			switch {
			case f.isRoot(id, stack):
				return sentryFuncKinds[fun.Sel.Name]
			case f.isContrib(id, stack):
				return sentryContribFuncKinds[fun.Sel.Name]
			}
			if f.isImport(id, stack) {
				return f.declKind(f.mf.declOf(fun), depth)
			}
		}
		if k := sentryMethodKinds[f.kindAt(fun.X, stack, depth+1)][fun.Sel.Name]; k != sentryUnknown {
			return k
		}
		if t, ok := exprType(f.mf, call, stack, 0); ok {
			if kind := f.run.modTypeKind(t); kind != sentryUnknown {
				return kind
			}
		}
		return f.run.methodKind(f, fun.Sel.Name)
	case *ast.Ident:
		if f.mf != nil && !modDeclaredLocally(fun.Name, fun.Pos(), stack) {
			return f.declKind(f.mf.declOf(fun), depth)
		}
	}
	return sentryUnknown
}

// isImport reports whether the identifier names a package that the file imports,
// as far as the module index tells; a package of the module is what it can use.
func (f *sentryFile) isImport(id *ast.Ident, stack []ast.Node) bool {
	if f.mf == nil || modDeclaredLocally(id.Name, id.Pos(), stack) {
		return false
	}
	_, ok := f.mf.importFor(id.Name)
	return ok
}

// identKind is the kind of a variable.
func (f *sentryFile) identKind(id *ast.Ident, stack []ast.Node, depth int) sentryKind {
	if d, ok := resolveIdent(id.Name, id.Pos(), stack); ok {
		switch {
		case d.typ != nil:
			return f.declaredKind(d.typ, stack)
		case d.init != nil:
			return f.kindAt(d.init, stack, depth+1)
		}
		return sentryUnknown
	}
	if f.mf != nil && !modDeclaredLocally(id.Name, id.Pos(), stack) {
		return f.declKind(f.mf.declOf(id), depth)
	}
	return sentryUnknown
}

// selectorKind is the kind of pkg.Name, a variable of another package, or of a
// field of a struct value.
func (f *sentryFile) selectorKind(sel *ast.SelectorExpr, stack []ast.Node, depth int) sentryKind {
	if id, ok := sel.X.(*ast.Ident); ok && f.isImport(id, stack) {
		return f.declKind(f.mf.declOf(sel), depth)
	}
	if t, ok := exprType(f.mf, sel, stack, 0); ok {
		if kind := f.run.modTypeKind(t); kind != sentryUnknown {
			return kind
		}
	}
	return f.run.fieldKind(f, sel.Sel.Name)
}

// declKind is the kind of the value a declaration of the module holds: a variable,
// or the first result of a function.
func (f *sentryFile) declKind(d *modDecl, depth int) sentryKind {
	if d == nil || d.dup || depth > sentryMaxDepth {
		return sentryUnknown
	}

	switch d.kind {
	case modFunc, modMethod:
		if results := d.results(); len(results) > 0 {
			return f.run.modTypeKind(results[0])
		}
	case modVar:
		if t, ok := d.varType(); ok {
			return f.run.modTypeKind(t)
		}
		if e, idx, ok := d.varInit(); ok && idx == 0 {
			return f.run.fileOf(d.file).kindAt(e, nil, depth+1)
		}
	default:
		// a constant or a type is not a value of sentry-go
	}
	return sentryUnknown
}

// sentryRun is the state of one run of the migration: the indexes of the modules,
// the files it has seen and the names that the structs and methods of a module
// give to values of sentry.
type sentryRun struct {
	indexes moduleIndexes
	files   map[*modFile]*sentryFile
	cache   kindTablesCache
	// sdk holds, by the root of a module, whether the module imports the middleware
	// of Fiber v3 and is on sentry-go 0.50 for it.
	sdk map[string]bool
	// changed are the files written since the indexes read them.
	changed []string
	// the tallies of what the project is told once
	extras, dropped sentryTally
}

// modTypeKind is the kind of a value of a type written in a file of the module.
func (r *sentryRun) modTypeKind(t modType) sentryKind {
	// the index guesses the name of a package of another module from its path, and
	// sentry-go is called sentry
	if kind := r.fileOf(t.file).typeKind(t.expr, nil); kind != sentryUnknown {
		return kind
	}
	if n, ok := t.named(); ok {
		if !sentryRootPath.MatchString(n.path) {
			return sentryOther
		}
		if kind, known := sentryTypeKinds[n.name]; known {
			return kind
		}
		return sentryOther
	}
	return sentryUnknown
}

// fileOf returns the questions of a file of the module as the index reads it.
func (r *sentryRun) fileOf(mf *modFile) *sentryFile {
	if sf := r.files[mf]; sf != nil {
		return sf
	}
	if r.files == nil {
		r.files = map[*modFile]*sentryFile{}
	}
	sf := &sentryFile{run: r, g: mf.g, mf: mf, names: sentryNamesOf(mf.g.file)}
	r.files[mf] = sf
	return sf
}

// fieldKind is the kind of the values that the fields called name hold, when every
// struct of the module with such a field says the same.
func (r *sentryRun) fieldKind(f *sentryFile, name string) sentryKind {
	return sentryKind(r.tables(f).fields[name])
}

// methodKind is the kind of the first value that the methods called name return,
// when every method of the module with that name says the same.
func (r *sentryRun) methodKind(f *sentryFile, name string) sentryKind {
	return sentryKind(r.tables(f).methods[name])
}

// tables returns the names of the fields and methods that hold values of sentry,
// of the module of the file, or of the file alone outside of a module.
func (r *sentryRun) tables(f *sentryFile) kindTables {
	return r.cache.of(f.mf, f.g,
		func(t modType) uint8 { return uint8(r.modTypeKind(t)) },
		func(t ast.Expr) uint8 { return uint8(f.declaredKind(t, nil)) })
}

// sentryTally collects the places of a rewrite that the project is told about
// once, at the first of them.
type sentryTally struct {
	noteTally
}

// report leaves the note, with the other places.
func (t *sentryTally) report(root, message string) {
	if !t.found {
		return
	}
	internal.AddNote(root, t.note(message))
}
