package v3

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// A project can declare an interface that mirrors the context of Fiber v2, so that a handler
// takes it instead of the context and a test hands it a fake:
//
//	type Context interface {
//	    ParamsInt(key string, defaultValue ...int) (int, error)
//	    BodyParser(out any) error
//	    JSON(data any, ctype ...string) error
//	}
//	func Wrap(h func(Context) error) fiber.Handler { return func(c *fiber.Ctx) error { return h(c) } }
//
// *fiber.Ctx implemented the interface without saying so. The Ctx of v3 does not have the
// methods v3 removed (ParamsInt, QueryInt, BodyParser, UserContext, ...), so h(c) does not
// compile. Where a fiber.Ctx is handed to such an interface, whose methods are all methods of
// v2 that v3 has, removed, or has changed, the migration wraps it in the LegacyCtx of the
// module package, a fiber.Ctx that has the removed methods with the behavior they had in v2.
// An interface that asks for a method v3 has with another signature (Redirect(string, ...int),
// SendFile(string, ...bool), Context() *fasthttp.RequestCtx) cannot be implemented that way
// without giving up the methods of v3, so the places that hand a context to it are left, and
// the note says what they lack.

// The methods of *fiber.Ctx of v2.52.15 that the Ctx of v3 does not have, one per line with their
// signature (see ctxSignature for how it is spelled). LegacyCtx of the module package has all of them.
const ctxLegacyMethodTable = `
AllParams () map[string]string
BodyParser (any) error
CookieParser (any) error
ParamsInt (string, ...int) (int, error)
ParamsParser (any) error
QueryBool (string, ...bool) bool
QueryFloat (string, ...float64) float64
QueryInt (string, ...int) int
QueryParser (any) error
RedirectBack (string, ...int) error
RedirectToRoute (string, Map, ...int) error
ReqHeaderParser (any) error
SetUserContext (ext.Context)
UserContext () ext.Context
`

// The methods of *fiber.Ctx of v2.52.15 that the Ctx of v3 has with another signature, with the one of v2.
const ctxChangedMethodTable = `
Bind (Map) error
Context () *ext.RequestCtx
Format (any) error
Range (int) (Range, error)
Redirect (string, ...int) error
SendFile (string, ...bool) error
`

// The methods of *fiber.Ctx of v2.52.15: an interface that declares a method that is not one of them
// was never implemented by the context.
const ctxV2MethodList = `Accepts AcceptsCharsets AcceptsEncodings AcceptsLanguages AllParams App Append
Attachment BaseURL Bind Body BodyParser BodyRaw ClearCookie ClientHelloInfo Context Cookie CookieParser
Cookies Download FormFile FormValue Format Fresh Get GetReqHeaders GetRespHeader GetRespHeaders GetRouteURL
Hostname IP IPs Is IsFromLocal IsProxyTrusted JSON JSONP Links Locals Location Method MultipartForm Next
OriginalURL Params ParamsInt ParamsParser Path Port Protocol Queries Query QueryBool QueryFloat QueryInt
QueryParser Range Redirect RedirectBack RedirectToRoute Render ReqHeaderParser Request Response
RestartRouting Route SaveFile SaveFileToStorage Secure Send SendFile SendStatus SendStream SendString Set
SetUserContext Stale Status String Subdomains Type UserContext Vary Write WriteString Writef XHR XML`

var (
	// ctxLegacyMethods are the methods of the first table by name, with their signature.
	ctxLegacyMethods = ctxParseMethodTable(ctxLegacyMethodTable)
	// ctxChangedMethods are the methods of the second table by name, with their signature in v2.
	ctxChangedMethods = ctxParseMethodTable(ctxChangedMethodTable)
	// ctxV2Methods are the names of the third list.
	ctxV2Methods = strings.Fields(ctxV2MethodList)
)

// ctxParseMethodTable reads a table of methods, one name and signature on each line.
func ctxParseMethodTable(table string) map[string]string {
	methods := map[string]string{}
	for line := range strings.SplitSeq(table, "\n") {
		if name, sig, ok := strings.Cut(strings.TrimSpace(line), " "); ok {
			methods[name] = sig
		}
	}
	return methods
}

// ctxMirror describes an interface of the project that the context of v2 implemented and the
// one of v3 does not.
type ctxMirror struct {
	// legacy are the methods of the interface that v3 removed, changed those it has with
	// another signature.
	legacy, changed []string
}

// adaptable reports whether LegacyCtx implements the interface.
func (m *ctxMirror) adaptable() bool { return len(m.changed) == 0 }

// lacks lists the methods the Ctx of v3 does not have in the form the interface asks for.
func (m *ctxMirror) lacks() []string {
	return slices.Concat(m.legacy, m.changed)
}

// ctxMirrors is what a run learned about the interfaces that mirror the context.
type ctxMirrors struct {
	// handed are the places, file:line, where a fiber.Ctx is handed to an interface, by the
	// key of the interface (see ctxCanonicalKey).
	handed map[string][]string
}

// MigrateContextMirrors wraps the context in a LegacyCtx where the code hands it to an
// interface of the project that mirrors methods of the context of v2 which v3 removed, and
// says which interfaces a fiber.Ctx no longer implements.
func MigrateContextMirrors(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	// Most projects declare no such interface: a look at the text says so before the files
	// are parsed.
	maybe := false
	if _, err := internal.ChangeFileContentInfo(cwd, func(content string, _ internal.FileInfo) string {
		maybe = maybe || ctxMayDeclareMirror(content)
		return content
	}); err != nil {
		return fmt.Errorf("failed to look for interfaces that mirror the context: %w", err)
	}
	if !maybe {
		return nil
	}

	run := newCtxRun()
	mirrors := &ctxMirrors{handed: map[string][]string{}}

	// The first pass finds where a context is handed to such an interface: the note at the
	// declaration of an interface needs to know whether the code hands one to it, and the
	// declaration can be in a file that is looked at before the code that hands the context.
	if _, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		ctxMirrorsContent(content, fi, run, mirrors, true)
		return content
	}); err != nil {
		return fmt.Errorf("failed to look for interfaces that mirror the context: %w", err)
	}

	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return ctxMirrorsContent(content, fi, run, mirrors, false)
	})
	run.report(cwd)
	if err != nil {
		return fmt.Errorf("failed to migrate interfaces that mirror the context: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating interfaces that mirror the context")
	return nil
}

// ctxMethodSpec matches the declaration of a method of the context of v2 that v3 removed or
// changed where an interface declares it: at the start of a line, or behind a brace or a semicolon.
var ctxMethodSpec = regexp.MustCompile(`(?m)(^|[{;])[ \t]*(` + strings.Join(ctxMirrorMethodNames(), "|") + `)\(`)

// ctxMirrorMethodNames returns the names of the methods of the context that v3 removed or changed.
func ctxMirrorMethodNames() []string {
	names := make([]string, 0, len(ctxLegacyMethods)+len(ctxChangedMethods))
	for name := range ctxLegacyMethods {
		names = append(names, name)
	}
	for name := range ctxChangedMethods {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// ctxMayDeclareMirror reports whether the text of a file can declare an interface with a method of
// the context of v2 that v3 removed or changed.
func ctxMayDeclareMirror(content string) bool {
	return strings.Contains(content, "interface") && ctxMethodSpec.MatchString(content)
}

func ctxMirrorsContent(content string, fi internal.FileInfo, run *ctxRun, mirrors *ctxMirrors, dry bool) string {
	g, env, ok := run.open(fi, content)
	if !ok {
		if !dry {
			ctxNoteMirrorDecls(content, fi, run, mirrors, nil)
		}
		return content
	}

	shim := &legacyShim{fi: fi}
	var plan ctxPlan
	env.mirrorSites(func(site ctxMirrorSite) {
		line := g.line(site.value.Pos())
		if dry {
			mirrors.handed[site.key] = append(mirrors.handed[site.key], fmt.Sprintf("%s:%d", fi.Rel, line))
			return
		}

		if site.plain && site.mirror.adaptable() && shim.available() {
			pkg := env.pkgAs(site.value.Pos(), shim.path, moduleShimPkg, legacyShimAlias)
			plan.add(func(e *editSet) {
				e.replaceNode(g, site.value, pkg+"."+legacyShimType+"{Ctx: "+ctxRenderNode(e, g, site.value)+"}")
			})
			return
		}
		if !site.plain {
			internal.Notef(fi, line, "%s embeds the fiber context and is given to %s, which the Ctx of Fiber v3 does not implement (it has no %s): add the methods to the type of the project; %s",
				g.text(site.value), site.name, ctxListNames(site.mirror.lacks()), ctxMirrorHints(site.mirror))
			return
		}
		internal.Notef(fi, line, "%s is given to %s, which a fiber.Ctx of Fiber v3 does not implement (it has no %s): %s",
			g.text(site.value), site.name, ctxListNames(site.mirror.lacks()), ctxMirrorAdvice(site.mirror))
	})

	if !dry {
		ctxNoteMirrorDecls(content, fi, run, mirrors, env)
	}
	if dry {
		return content
	}
	return env.finish(content, plan.apply(content))
}

// ctxNoteMirrorDecls reports the interfaces of the file that a fiber.Ctx is handed to. env is
// the environment of the file, nil when it is none of the business of the context migrations.
func ctxNoteMirrorDecls(content string, fi internal.FileInfo, run *ctxRun, mirrors *ctxMirrors, env *ctxEnv) {
	if len(mirrors.handed) == 0 || !strings.Contains(content, "interface") {
		return
	}
	if env == nil {
		g, ok := parseSource(content)
		if !ok {
			return
		}
		env = run.newEnv(fi, g, nil)
	}

	g := env.g
	for _, decl := range g.file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.TypeParams != nil {
				continue
			}
			if _, isInterface := ts.Type.(*ast.InterfaceType); !isInterface {
				continue
			}
			places := mirrors.handed[env.canonicalKey(ts.Name.Name)]
			if len(places) == 0 {
				continue
			}
			mirror := env.mirrorOf(ts.Name)
			if mirror == nil {
				continue
			}
			internal.Notef(fi, g.line(ts.Name.Pos()),
				"%s declares methods of the context of Fiber v2 that v3 removed or changed (%s), so a fiber.Ctx does not implement it any more. The code gives it one at %s. %s",
				ts.Name.Name, ctxListNames(mirror.lacks()), ctxListPlaces(places), ctxMirrorAdvice(mirror))
		}
	}
}

// ctxMirrorHints lists what v3 has in place of the methods of v2 that were removed.
func ctxMirrorHints(m *ctxMirror) string {
	hints := make([]string, 0, len(m.legacy))
	for _, name := range m.legacy {
		if hint, ok := reviewAdvice["Ctx."+name]; ok {
			hints = append(hints, name+": "+hint)
		}
	}
	return strings.Join(hints, "; ")
}

// ctxMirrorAdvice says what to do about an interface the context no longer implements.
func ctxMirrorAdvice(m *ctxMirror) string {
	if m.adaptable() {
		return fmt.Sprintf("The migration wraps the context in fibermigrate.%s{Ctx: c}, a fiber.Ctx that has the removed methods as they were in v2, where it finds it given to the interface; wrap the ones it did not find the same way, or ask for what v3 has instead (fiber.Params[int](c, key), fiber.Query[int](c, key), c.Bind().Body(&v), the Ctx itself as a context.Context)",
			legacyShimType)
	}
	return fmt.Sprintf("The changed methods (%s) cannot be added to a fiber.Ctx without giving up the ones of v3: write a type that implements the interface around the context, with %s",
		ctxListNames(m.changed), ctxMirrorHints(m))
}

// ctxListNames lists names for a message.
func ctxListNames(names []string) string {
	return strings.Join(names, ", ")
}

// ctxListPlaces lists the first places of a list for a message.
func ctxListPlaces(places []string) string {
	const shown = 3
	places = slices.Clone(places)
	slices.Sort(places)
	places = slices.Compact(places)
	if len(places) <= shown {
		return strings.Join(places, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(places[:shown], ", "), len(places)-shown)
}

// ctxMirrorSite is a place of the code where a fiber.Ctx is handed to an interface that it
// implemented in v2 and does not any more.
type ctxMirrorSite struct {
	value  ast.Expr
	mirror *ctxMirror
	// key is the key of the interface (see canonicalKey), name how the code spells it.
	key, name string
	// plain is set for a value that is a fiber.Ctx, and not a type of the project that
	// embeds one.
	plain bool
}

// interfaceName spells the type with the key for a message: Context for a type of the package
// of the file, contract.Context for one of another package.
func (env *ctxEnv) interfaceName(key string) string {
	importPath, name, ok := ctxSplitKey(key)
	if !ok || !ctxIsForeignKey(key) || importPath == env.pkgPath {
		return strings.TrimPrefix(key, env.pkgPath+".")
	}
	return path.Base(importPath) + "." + name
}

// isPlainCtx reports whether t is the fiber.Ctx type, as the code spells it or as the
// environment does when it types the result of a call, or an alias of it.
func (env *ctxEnv) isPlainCtx(t ast.Expr, stack []ast.Node) bool {
	return ctxIsFiberType(ast.Unparen(t), env.pkgNames, ctxTypeCtx) || env.isCtxAlias(ast.Unparen(ctxDerefType(t)), stack)
}

// isLegacyCtx reports whether t is the LegacyCtx of the module package, which has the methods.
func (env *ctxEnv) isLegacyCtx(t ast.Expr) bool {
	importPath, name, ok := ctxSplitKey(env.typeKey(t))
	return ok && name == legacyShimType && strings.HasSuffix(importPath, "/"+moduleShimDir)
}

// canonicalKey returns a key of the type that is the same in every file of the module.
func (env *ctxEnv) canonicalKey(key string) string {
	if ctxIsForeignKey(key) {
		return key
	}
	return ctxForeignKey(env.pkgPath, key)
}

// mirrorSites calls visit for each place of the file where a value that is a fiber.Ctx is
// handed to an interface that mirrors the context: an argument of a call, a conversion, a
// result, the value of an assignment or of a declaration with a type, a field or an element
// of a literal, a value sent on a channel.
func (env *ctxEnv) mirrorSites(visit func(ctxMirrorSite)) {
	consider := func(value, target ast.Expr, stack []ast.Node) {
		if value == nil || target == nil {
			return
		}
		inner := ast.Unparen(value)
		t := env.typeOfAt(inner, stack, 1)
		if t == nil || env.isLegacyCtx(t) {
			return
		}
		// the context itself, or a type that embeds it and hides nothing of what the
		// interface asks for
		plain := env.isPlainCtx(t, stack)
		if !plain && env.typeIs(t, ctxTypeCtx) != ctxYes {
			return
		}
		mirror := env.mirrorOf(target)
		if mirror == nil {
			return
		}
		visit(ctxMirrorSite{
			value:  inner,
			mirror: mirror,
			key:    env.canonicalKey(env.typeKey(ast.Unparen(target))),
			name:   env.interfaceName(env.typeKey(ast.Unparen(target))),
			plain:  plain,
		})
	}

	env.g.walk(func(n ast.Node, stack []ast.Node) bool {
		inside := append(slices.Clone(stack), n)
		switch x := n.(type) {
		case *ast.CallExpr:
			env.callSites(x, inside, consider)
		case *ast.ReturnStmt:
			if results := ctxEnclosingResults(stack); len(results) == len(x.Results) {
				for i, value := range x.Results {
					consider(value, results[i], inside)
				}
			}
		case *ast.AssignStmt:
			if x.Tok == token.ASSIGN && len(x.Lhs) == len(x.Rhs) {
				for i, value := range x.Rhs {
					consider(value, env.typeOfAt(x.Lhs[i], inside, 1), inside)
				}
			}
		case *ast.ValueSpec:
			if x.Type != nil && len(x.Values) == len(x.Names) {
				for _, value := range x.Values {
					consider(value, x.Type, inside)
				}
			}
		case *ast.CompositeLit:
			env.literalSites(x, inside, consider)
		case *ast.SendStmt:
			if ch, ok := env.typeOfAt(x.Chan, inside, 1).(*ast.ChanType); ok {
				consider(x.Value, ch.Value, inside)
			}
		}
		return true
	})
}

// callSites calls consider for the arguments of a call of a function, and for the argument of a
// conversion to a type, with the type each goes to.
func (env *ctxEnv) callSites(call *ast.CallExpr, stack []ast.Node, consider func(value, target ast.Expr, stack []ast.Node)) {
	if len(call.Args) == 1 && env.isTypeName(call.Fun) {
		consider(call.Args[0], call.Fun, stack)
		return
	}

	params := env.calleeParams(call, stack)
	for i, arg := range call.Args {
		switch {
		case len(params) == 0:
			return
		case i < len(params) && !isEllipsis(params[i]):
			consider(arg, params[i], stack)
		case !call.Ellipsis.IsValid():
			if variadic, ok := params[len(params)-1].(*ast.Ellipsis); ok {
				consider(arg, variadic.Elt, stack)
			}
		}
	}
}

// isEllipsis reports whether the type of a parameter is the ...T of a variadic function.
func isEllipsis(t ast.Expr) bool {
	_, ok := t.(*ast.Ellipsis)
	return ok
}

// literalSites calls consider for the values of a composite literal: the fields of a struct,
// the elements of a slice, an array or a map.
func (env *ctxEnv) literalSites(lit *ast.CompositeLit, stack []ast.Node, consider func(value, target ast.Expr, stack []ast.Node)) {
	if lit.Type == nil {
		return
	}

	switch t := ast.Unparen(lit.Type).(type) {
	case *ast.ArrayType:
		for _, elt := range lit.Elts {
			consider(elt, t.Elt, stack)
		}
	case *ast.MapType:
		for _, elt := range lit.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				consider(kv.Value, t.Value, stack)
			}
		}
	default:
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if key, isIdent := kv.Key.(*ast.Ident); isIdent {
				consider(kv.Value, env.structFieldType(lit.Type, key.Name, 0), stack)
			}
		}
	}
}

// isTypeName reports whether e names a type of the module, so that a call of it is a
// conversion.
func (env *ctxEnv) isTypeName(e ast.Expr) bool {
	switch ast.Unparen(e).(type) {
	case *ast.Ident, *ast.SelectorExpr:
		return env.typeSpec(env.typeKey(ast.Unparen(e))) != nil
	}
	return false
}

// enclosingResults returns the types of the results of the function around the innermost node of
// stack.
func ctxEnclosingResults(stack []ast.Node) []ast.Expr {
	for _, node := range slices.Backward(stack) {
		switch fn := node.(type) {
		case *ast.FuncLit:
			return ctxResultTypes(fn.Type)
		case *ast.FuncDecl:
			return ctxResultTypes(fn.Type)
		}
	}
	return nil
}

// calleeParams returns the types of the parameters of the function that call calls, the type of
// the last one an Ellipsis for a variadic function. It is nil when the function is not known.
func (env *ctxEnv) calleeParams(call *ast.CallExpr, stack []ast.Node) []ast.Expr {
	var ft *ast.FuncType
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		switch {
		case env.funcTypeOf(env.typeOfAt(fun, stack, 1), 1) != nil:
			ft = env.funcTypeOf(env.typeOfAt(fun, stack, 1), 1)
		case env.funcDecl[fun.Name] != nil:
			ft = env.funcDeclOf(fun.Name).Type
		default:
			if mem := env.dotMember(fun.Name); mem != nil && mem.fn != nil {
				ft = mem.fn.Type
			}
		}
	case *ast.SelectorExpr:
		if mem := env.pkgMember(fun, stack); mem != nil {
			if mem.fn != nil {
				ft = mem.fn.Type
			} else if mem.typ != nil {
				ft = env.funcTypeOf(mem.typ, 1)
			}
			break
		}
		if recv := env.typeOfAt(fun.X, stack, 1); recv != nil {
			ft, _ = env.methodFuncType(recv, fun.Sel.Name, 0)
		}
		if ft == nil {
			ft = env.funcTypeOf(env.fieldTypeOf(fun, stack, 0), 1)
		}
	case *ast.FuncLit:
		ft = fun.Type
	default:
		ft = env.funcTypeOf(env.typeOfAt(call.Fun, stack, 1), 1)
	}
	if ft == nil {
		return nil
	}
	return ctxParamTypes(ft)
}

// mirrorOf returns the description of the interface that the type expression names when it
// mirrors the context of v2 and a fiber.Ctx of v3 does not implement it, nil for anything else.
func (env *ctxEnv) mirrorOf(target ast.Expr) *ctxMirror {
	t := ast.Unparen(target)
	switch t.(type) {
	case *ast.Ident, *ast.SelectorExpr:
	default:
		return nil
	}
	spec := env.typeSpec(env.typeKey(t))
	if spec == nil || spec.TypeParams != nil {
		return nil
	}
	it, ok := spec.Type.(*ast.InterfaceType)
	if !ok {
		return nil
	}
	methods, ok := env.interfaceMethods(it, 0)
	if !ok || len(methods) == 0 {
		return nil
	}

	var m ctxMirror
	for name, ft := range methods {
		if !slices.Contains(ctxV2Methods, name) {
			// the context never had it
			return nil
		}
		sig := ctxSignature(ft, env.pkgNames)
		switch {
		case ctxLegacyMethods[name] != "":
			if sig != ctxLegacyMethods[name] {
				return nil
			}
			m.legacy = append(m.legacy, name)
		case ctxChangedMethods[name] != "":
			if sig != ctxChangedMethods[name] {
				return nil
			}
			m.changed = append(m.changed, name)
		}
	}
	if len(m.legacy)+len(m.changed) == 0 {
		return nil
	}
	slices.Sort(m.legacy)
	slices.Sort(m.changed)
	return &m
}

// interfaceMethods returns the methods of an interface type, with the ones of the interfaces it
// embeds, by name. It reports false for an interface with an embedded type it does not know.
func (env *ctxEnv) interfaceMethods(it *ast.InterfaceType, depth int) (map[string]*ast.FuncType, bool) {
	if depth > ctxMaxDepth || it.Methods == nil {
		return nil, depth <= ctxMaxDepth
	}

	methods := map[string]*ast.FuncType{}
	for _, f := range it.Methods.List {
		if ft, ok := f.Type.(*ast.FuncType); ok {
			for _, name := range f.Names {
				methods[name.Name] = ft
			}
			continue
		}

		switch embedded := ast.Unparen(f.Type).(type) {
		case *ast.Ident, *ast.SelectorExpr:
			spec := env.typeSpec(env.typeKey(embedded))
			if spec == nil {
				return nil, false
			}
			inner, ok := spec.Type.(*ast.InterfaceType)
			if !ok {
				return nil, false
			}
			more, ok := env.interfaceMethods(inner, depth+1)
			if !ok {
				return nil, false
			}
			maps.Copy(methods, more)
		case *ast.InterfaceType:
			more, ok := env.interfaceMethods(embedded, depth+1)
			if !ok {
				return nil, false
			}
			maps.Copy(methods, more)
		default:
			return nil, false
		}
	}
	return methods, true
}

// ctxExternalType matches the qualifier of a type of a package that is not the fiber package:
// context.Context, and fasthttp.RequestCtx, or the placeholder that the types of the packages
// outside of the module are given when a declaration of another package is read.
var ctxExternalType = regexp.MustCompile(`\x00ext:harmless\.|\bcontext\.|\bfasthttp\.`)

// ctxSignature spells the types of the parameters and the results of a function the way the
// tables of the methods do: no names, interface{} as any, the types of the fiber package
// without their qualifier, and the types of the other packages as ext.Name.
func ctxSignature(ft *ast.FuncType, fiberNames []string) string {
	spell := func(fl *ast.FieldList) []string {
		if fl == nil {
			return nil
		}
		var out []string
		for _, f := range fl.List {
			text := types.ExprString(f.Type)
			text = strings.ReplaceAll(text, "interface{}", "any")
			for _, name := range fiberNames {
				text = strings.ReplaceAll(text, name+".", "")
			}
			text = ctxExternalType.ReplaceAllString(text, "ext.")
			for range max(len(f.Names), 1) {
				out = append(out, text)
			}
		}
		return out
	}

	sig := "(" + strings.Join(spell(ft.Params), ", ") + ")"
	switch results := spell(ft.Results); len(results) {
	case 0:
	case 1:
		sig += " " + results[0]
	default:
		sig += " (" + strings.Join(results, ", ") + ")"
	}
	return sig
}
