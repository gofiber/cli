package v3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// mw2Kind says what the v3 function that hands out a middleware's value returns.
type mw2Kind int

const (
	// mw2KindString is a string: "" when the value is absent.
	mw2KindString mw2Kind = iota
	// mw2KindPointer is a pointer: nil when the value is absent.
	mw2KindPointer
	// mw2KindAny is an any: nil when the value is absent.
	mw2KindAny
)

// mw2Role is a value a middleware keeps in the request locals in v2: the config
// field that names its key, the key it uses when the field is empty, and the
// function v3 offers to read it. v3 stores the values under keys of private types,
// so c.Locals(key) returns nil there and the reads have to change.
type mw2Role struct {
	// re matches the import path of the middleware.
	re *regexp.Regexp
	// pkg is the name of the middleware package.
	pkg string
	// addPath is the import path to add when a file reads the value without importing the package.
	addPath string
	// alias is the name the import added for addPath gets, "" when the package is
	// called what its path says (the v3 package of the Fiber middleware for sentry is
	// called sentry, and sentry-go is called that in the files that read the hub).
	alias string
	// nameOf gives the name that an import of the path has when it names none, "" or
	// nil for pkg.
	nameOf func(importPath string) string
	// field is the config field that holds the key.
	field string
	// defaultKey is the key v2 used when the field was empty, "" when the value was only stored once the field was set.
	defaultKey string
	// reader is the v3 function that returns the value, "" when v3 does not keep it.
	reader string
	// logTag is the tag of v3's logger that prints the value, "" when there is none.
	logTag string
	// pointers are the names of the pointer type that v2 code asserts, for mw2KindPointer.
	pointers []string
	kind     mw2Kind
	// masked is set when the logger tag of v3 prints a masked prefix instead of the whole value.
	masked bool
}

// The names of the middleware packages that keep values in the request locals,
// and of the context key field most of them have.
const (
	mw2PkgRequestID   = "requestid"
	mw2PkgKeyAuth     = "keyauth"
	mw2PkgCSRF        = "csrf"
	mw2PkgBasicAuth   = "basicauth"
	mw2PkgJWT         = "jwtware"
	mw2PkgPaseto      = "pasetoware"
	mw2PkgSentry      = "sentry"
	mw2PkgFiberSentry = "fibersentry"
	mw2FieldKey       = "ContextKey"
	// mw2ReaderFromContext is the name most of the v3 readers have.
	mw2ReaderFromContext = "FromContext"
)

// mw2BuiltinPath is the import path of a built-in middleware in v3.
func mw2BuiltinPath(name string) string {
	return "github.com/gofiber/fiber/v3/middleware/" + name
}

// mw2Roles lists the values the middleware of v2 keep in the request locals. The
// session middleware is not among them: it kept nothing there.
var mw2Roles = []*mw2Role{
	{
		re: mw2RequestID, pkg: mw2PkgRequestID, addPath: mw2BuiltinPath(mw2PkgRequestID),
		field: mw2FieldKey, defaultKey: "requestid", reader: mw2ReaderFromContext, logTag: "requestid",
	},
	{
		re: mw2KeyAuth, pkg: mw2PkgKeyAuth, addPath: mw2BuiltinPath(mw2PkgKeyAuth),
		field: mw2FieldKey, defaultKey: "token", reader: "TokenFromContext", logTag: "api-key", masked: true,
	},
	{
		// The token is only stored when ContextKey is set.
		re: mw2CSRF, pkg: mw2PkgCSRF, addPath: mw2BuiltinPath(mw2PkgCSRF),
		field: mw2FieldKey, reader: "TokenFromContext", logTag: "csrf-token", masked: true,
	},
	{
		re: mw2CSRF, pkg: mw2PkgCSRF, addPath: mw2BuiltinPath(mw2PkgCSRF),
		field: "HandlerContextKey", defaultKey: "fiber.csrf.handler", reader: "HandlerFromContext",
		kind: mw2KindPointer, pointers: []string{nameCSRFHandler, "Handler"},
	},
	{
		re: mw2BasicAuth, pkg: mw2PkgBasicAuth, addPath: mw2BuiltinPath(mw2PkgBasicAuth),
		field: "ContextUsername", defaultKey: "username", reader: "UsernameFromContext", logTag: "username",
	},
	{
		// v3 no longer keeps the password in the request context.
		re: mw2BasicAuth, pkg: mw2PkgBasicAuth, addPath: mw2BuiltinPath(mw2PkgBasicAuth),
		field: "ContextPassword", defaultKey: "password",
	},
	{
		re: mw2JWT, pkg: mw2PkgJWT, addPath: "github.com/gofiber/contrib/v3/jwt",
		field: mw2FieldKey, defaultKey: "user", reader: mw2ReaderFromContext,
		kind: mw2KindPointer, pointers: []string{"Token"},
	},
	{
		re: mw2Paseto, pkg: mw2PkgPaseto, addPath: "github.com/gofiber/contrib/v3/paseto",
		field: mw2FieldKey, defaultKey: "auth-token", reader: mw2ReaderFromContext, kind: mw2KindAny,
	},
	{
		// The hub of the request. The v2 middleware had no setting for the key and
		// kept the hub under "sentry-hub", where code that does not use
		// GetHubFromContext read it from; the v3 one stores it under a key of a
		// private type. The package is called fibersentry at v2 and sentry at v3, and
		// the files that read the hub import sentry-go as sentry.
		re: sentryContribPath, pkg: mw2PkgSentry, addPath: sentryContribV3Import, alias: mw2PkgFiberSentry, nameOf: mw2SentryName,
		defaultKey: "sentry-hub", reader: "GetHubFromContext", kind: mw2KindPointer, pointers: []string{"Hub"},
	},
}

// mw2SentryName is the name that an import of the Fiber middleware for sentry has
// when it names none.
func mw2SentryName(importPath string) string {
	if strings.Contains(importPath, "/fibersentry") {
		return mw2PkgFiberSentry
	}
	return mw2PkgSentry
}

// mw2DefaultContextKey is the paseto constant that names the default key.
const mw2DefaultContextKey = "DefaultContextKey"

// mw2Key identifies a key of the request locals. Two keys are the same value in v2
// when they have the same type and value, so a typed constant is not the string
// it wraps.
type mw2Key struct {
	// typ is the type of a typed key, "" for a plain string.
	typ string
	// val is the string, or the source text of an expression that is not a constant.
	val string
	// dir scopes the text of an expression that only means something in its package.
	dir string
	// expr is set when val is the text of an expression, not a constant.
	expr bool
}

// mw2KeyResolver turns the key expression of a Locals call or of a config field
// into the identity of the key.
type mw2KeyResolver struct {
	g   *goSource
	pkg *mw2Package
	// pkgs reads the other packages of the module, nil when there is no module to read.
	pkgs *mw2Packages
	// names maps the identifiers the file binds to the middleware packages to the roles of the package.
	names map[string][]*mw2Role
	dir   string
}

// key resolves e; ok is false when e has no identity worth comparing.
func (kr *mw2KeyResolver) key(e ast.Expr) (mw2Key, bool) {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return kr.key(x.X)
	case *ast.BasicLit, *ast.BinaryExpr:
		if v, ok := kr.scope().str(e, 0); ok {
			return mw2Key{val: v}, true
		}
	case *ast.Ident:
		return kr.ident(x), true
	case *ast.CallExpr:
		if k, ok := kr.conversion(x); ok {
			return k, true
		}
	case *ast.SelectorExpr:
		if k, ok := kr.middlewareDefault(x); ok {
			return k, true
		}
		if k, ok := kr.qualified(x); ok {
			return k, true
		}
		// pkg.Key means the same in every package that imports pkg.
		return mw2Key{val: kr.text(e), expr: true}, true
	}
	return mw2Key{val: kr.text(e), dir: kr.dir, expr: true}, true
}

// text is the source text of e without its blanks.
func (kr *mw2KeyResolver) text(e ast.Expr) string {
	return strings.Join(strings.Fields(kr.g.text(e)), "")
}

// ident resolves a name: a constant string of the package, typed or not, a
// variable of it that nothing assigns to, or otherwise the name itself.
func (kr *mw2KeyResolver) ident(id *ast.Ident) mw2Key {
	scope := kr.scope()
	if c, ok := kr.pkg.consts[id.Name]; ok && c.value != nil {
		if v, known := (mw2ConstScope{pkgs: kr.pkgs, pkg: kr.pkg, imports: c.imports}).str(c.value, 1); known {
			switch typ := mw2TypeText(c.typ); {
			case c.typ == nil || typ == nameString:
				return mw2Key{val: v}
			case typ != "":
				return mw2Key{typ: typ, val: v}
			}
		}
	} else if v, known := scope.str(id, 0); known {
		return mw2Key{val: v}
	}
	return mw2Key{val: id.Name, dir: kr.dir, expr: true}
}

// conversion resolves T("constant") for a type T of the package.
func (kr *mw2KeyResolver) conversion(call *ast.CallExpr) (mw2Key, bool) {
	fn, ok := call.Fun.(*ast.Ident)
	if !ok || len(call.Args) != 1 || !kr.pkg.types[fn.Name] {
		return mw2Key{}, false
	}
	v, ok := mw2ConstString(call.Args[0], kr.pkg.constValue, 0)
	return mw2Key{typ: fn.Name, val: v}, ok
}

// middlewareDefault resolves <pkg>.ConfigDefault.<Field> and paseto's
// <pkg>.DefaultContextKey to the key the middleware uses by default.
func (kr *mw2KeyResolver) middlewareDefault(sel *ast.SelectorExpr) (mw2Key, bool) {
	if inner, ok := sel.X.(*ast.SelectorExpr); ok && inner.Sel.Name == "ConfigDefault" {
		if id, ok := inner.X.(*ast.Ident); ok {
			for _, role := range kr.names[id.Name] {
				if role.field == sel.Sel.Name && role.defaultKey != "" {
					return mw2Key{val: role.defaultKey}, true
				}
			}
		}
	}

	if id, ok := sel.X.(*ast.Ident); ok && sel.Sel.Name == mw2DefaultContextKey {
		for _, role := range kr.names[id.Name] {
			if role.kind == mw2KindAny && role.defaultKey != "" {
				return mw2Key{val: role.defaultKey}, true
			}
		}
	}
	return mw2Key{}, false
}

// mw2RoleNames maps the identifiers a file binds to the packages of the roles to
// the roles of each package.
func mw2RoleNames(src string) map[string][]*mw2Role {
	names := map[string][]*mw2Role{}
	for _, role := range mw2Roles {
		for _, name := range role.importNames(src) {
			if name != "." {
				names[name] = append(names[name], role)
			}
		}
	}
	return names
}

// importNames returns the identifiers src binds to the package of the role.
func (r *mw2Role) importNames(src string) []string {
	if r.nameOf == nil {
		return importNames(src, r.re, r.pkg)
	}

	// the name of an import that has none depends on the path
	var names []string
	for _, path := range mw2ImportPaths(src, r.re) {
		names = append(names, importNames(src, regexp.MustCompile(`^`+regexp.QuoteMeta(path)+`$`), r.nameOf(path))...)
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// mw2ImportPaths returns the import paths of src that match re.
func mw2ImportPaths(src string, re *regexp.Regexp) []string {
	file, err := parser.ParseFile(token.NewFileSet(), "", src, parser.ImportsOnly|parser.SkipObjectResolution)
	if err != nil && file == nil {
		return nil
	}
	var paths []string
	for _, spec := range file.Imports {
		if path, unquoteErr := strconv.Unquote(spec.Path.Value); unquoteErr == nil && re.MatchString(path) && !slices.Contains(paths, path) {
			paths = append(paths, path)
		}
	}
	return paths
}

// mw2IsLocalsCall reports whether n is a call of a method called Locals and
// returns the method selector.
func mw2IsLocalsCall(n ast.Node) (*ast.CallExpr, *ast.SelectorExpr, bool) {
	call, ok := n.(*ast.CallExpr)
	if !ok || call.Ellipsis.IsValid() {
		return nil, nil, false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Locals" || len(call.Args) == 0 {
		return nil, nil, false
	}
	return call, sel, true
}
