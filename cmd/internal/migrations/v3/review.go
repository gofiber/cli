package v3

import (
	"go/ast"
	"go/token"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/gofiber/cli/cmd/internal"
)

const (
	// reviewCorePath is the import path of the core fiber package of the v3 module.
	reviewCorePath = "github.com/gofiber/fiber/v3"
	// reviewV3Prefix is the import path prefix of every package of the v3 module.
	reviewV3Prefix = reviewCorePath + "/"
	// reviewContribPrefix is the import path prefix of the contrib modules.
	reviewContribPrefix = "github.com/gofiber/contrib/"
)

var (
	reviewReFiberV2 = regexp.MustCompile(`^github\.com/gofiber/fiber/v2(/|$)`)
	// reviewReLegacy matches modules that were folded into contrib for v3.
	reviewReLegacy = regexp.MustCompile(`^github\.com/gofiber/(websocket|jwt)/`)
)

// Review looks at the project after the v2 to v3 migrations ran and records
// what is left for the user as notes: v2 constructs that no migration could
// rewrite and that will not compile on v3, and behavior changes of the
// middleware the project uses that no rewrite can reproduce. It changes nothing.
func Review(cwd string) {
	r := &reviewer{behaviorSeen: map[int]bool{}, run: newCtxRun()}

	_, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		r.scan(content, fi)
		return content
	})
	if err != nil {
		internal.AddNote(cwd, internal.Note{Message: "the project could not be scanned for leftovers: " + err.Error()})
	}
	r.trust.report(cwd)
}

type reviewer struct {
	// behaviorSeen holds the behavior rules that were reported already: a rule
	// is reported once per project.
	behaviorSeen map[int]bool
	// run reads the files next to the one scanned, which declare the types it uses.
	run *ctxRun
	// trust collects what the note about the trusted proxy settings needs (see review_trust.go).
	trust reviewTrust
	// fileNameSeen is set once the SendFile file name note was given, bindSeen the note about
	// the texts of the binder errors.
	fileNameSeen bool
	bindSeen     bool
}

// reviewFile is what the checks need to know about the file being scanned.
type reviewFile struct {
	r *reviewer
	g *goSource
	// packages maps the identifier the file binds to an import to its path, for
	// the packages of the fiber v3 module.
	packages map[string]string
	// env tells the types of the declarations of the file, built when first needed.
	env *ctxEnv
	fi  internal.FileInfo
	// core are the identifiers the file binds to the core fiber package.
	core []string
}

func (r *reviewer) scan(content string, fi internal.FileInfo) {
	if strings.Contains(content, moduleShimMarker) {
		return // a file the migrations added
	}
	if !strings.Contains(content, "github.com/gofiber/") {
		r.scanModuleContexts(content, fi)
		return
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return
	}

	rf := &reviewFile{r: r, g: g, fi: fi, packages: map[string]string{}}
	for _, spec := range g.file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil || (importPath != reviewCorePath && !strings.HasPrefix(importPath, reviewV3Prefix)) {
			continue
		}
		name := defaultImportName(importPath)
		if spec.Name != nil {
			name = spec.Name.Name
		}
		if name == "_" || name == "." {
			continue
		}
		rf.packages[name] = importPath
		if importPath == reviewCorePath {
			rf.core = append(rf.core, name)
		}
	}

	rf.checkImports()
	rf.checkBehavior(content)
	rf.checkUses()
}

func (rf *reviewFile) notef(pos token.Pos, format string, args ...any) {
	internal.Notef(rf.fi, rf.g.line(pos), format, args...)
}

// checkImports reports imports that cannot work on v3.
func (rf *reviewFile) checkImports() {
	for _, spec := range rf.g.file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}

		rest, isContrib := strings.CutPrefix(importPath, reviewContribPrefix)
		switch {
		case reviewRemovedPackages[importPath] != "":
			rf.notef(spec.Pos(), "%s does not exist in Fiber v3 (%s)", importPath, reviewRemovedPackages[importPath])
		case reviewReFiberV2.MatchString(importPath):
			rf.notef(spec.Pos(), "still imports the Fiber v2 package %s, which was not migrated", importPath)
		case importPath == reviewV3Prefix+"utils":
			rf.notef(spec.Pos(), "%s does not exist in Fiber v3, the helpers moved to github.com/gofiber/utils/v2", importPath)
		case isContrib && rest != "v3" && !strings.HasPrefix(rest, "v3/"):
			rf.notef(spec.Pos(), "still imports %s, a contrib module for Fiber v2, which was not migrated", importPath)
		case reviewReLegacy.MatchString(importPath):
			rf.notef(spec.Pos(), "still imports %s, a Fiber v2 module that moved into github.com/gofiber/contrib/v3", importPath)
		}
	}
}

// checkBehavior reports each behavior rule once per project, at the first
// import it applies to.
func (rf *reviewFile) checkBehavior(content string) {
	for i, rule := range reviewBehaviorRules {
		if rf.r.behaviorSeen[i] || (rule.uses != nil && !rule.uses.MatchString(content)) || (rule.unless != nil && rule.unless.MatchString(content)) {
			continue
		}

		for _, spec := range rf.g.file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil || !rule.pkg.MatchString(importPath) {
				continue
			}
			if rule.when != nil && !rule.when(rf) {
				break
			}
			message := rule.message
			if rule.tests {
				message += rf.httptestNote()
			}
			rf.r.behaviorSeen[i] = true
			rf.notef(spec.Pos(), "%s", message)
			break
		}
	}
}

// checkUses reports uses of API that v2 had and v3 does not.
func (rf *reviewFile) checkUses() {
	rf.g.walk(func(n ast.Node, stack []ast.Node) bool {
		rf.collectTrust(n, stack)
		switch x := n.(type) {
		case *ast.SelectorExpr:
			rf.checkSelector(x)
		case *ast.CompositeLit:
			rf.checkLiteral(x)
		case *ast.CallExpr:
			rf.checkCall(x, stack)
			rf.checkBindErrors(x, stack)
		}
		return true
	})
}

// packageOf returns the identifier and the import path of the fiber v3 package
// that e names.
func (rf *reviewFile) packageOf(e ast.Expr) (alias, importPath string, ok bool) {
	id, isIdent := e.(*ast.Ident)
	if !isIdent {
		return "", "", false
	}
	importPath, ok = rf.packages[id.Name]
	return id.Name, importPath, ok
}

// checkSelector flags pkg.Name where Name no longer exists in the package.
func (rf *reviewFile) checkSelector(sel *ast.SelectorExpr) {
	_, importPath, ok := rf.packageOf(sel.X)
	if !ok || !slices.Contains(reviewRemovedNames[importPath], sel.Sel.Name) {
		return
	}

	rf.notef(sel.Pos(), "%s was removed in Fiber v3%s", rf.g.text(sel), reviewHint(importPath+"."+sel.Sel.Name))
}

// checkLiteral flags keys of a pkg.Type{...} literal that the v3 struct lacks.
func (rf *reviewFile) checkLiteral(lit *ast.CompositeLit) {
	sel, ok := lit.Type.(*ast.SelectorExpr)
	if !ok {
		return
	}
	alias, importPath, ok := rf.packageOf(sel.X)
	if !ok {
		return
	}

	removed := reviewRemovedFields[importPath+"."+sel.Sel.Name]
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || !slices.Contains(removed, key.Name) {
			continue
		}
		hint := reviewHint(importPath + "." + sel.Sel.Name + "." + key.Name)
		if importPath == reviewV3Prefix+"middleware/csrf" && csrfLookupSource(kv.Value) == csrfCookieSource {
			// the extractors package has no equivalent that v3 accepts
			hint = ": a lookup of a cookie is the CSRF cookie itself, which v3 refuses; send the token in a header or a form field"
		}
		rf.notef(key.Pos(), "%s.%s has no %s field in Fiber v3%s", alias, sel.Sel.Name, key.Name, hint)
	}
}

// checkCall flags calls of methods that v3 removed from the context, the app,
// groups and hooks.
func (rf *reviewFile) checkCall(call *ast.CallExpr, stack []ast.Node) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	name := sel.Sel.Name

	switch {
	case (name == mw3SendFile || name == "Download") && !rf.r.fileNameSeen && rf.sendsDynamicFile(call, sel, stack):
		rf.r.fileNameSeen = true
		rf.notef(sel.Sel.Pos(), "%s takes the file name literally in Fiber v3 instead of percent-decoding it, so a name taken from a route parameter keeps its %%XX escapes unless fiber.Config{UnescapePath: true} is set; decode it and make sure it stays inside the directory you serve", name)
	case slices.Contains(reviewRemovedMethods[reviewCorePath+".Ctx"], name) && rf.isCtxMethod(sel, stack):
		rf.notef(sel.Sel.Pos(), "%s was removed in Fiber v3%s%s", name, reviewHint("Ctx."+name), rf.wrapperHint(sel, stack))
	case name == "Redirect" && len(call.Args) > 0 && rf.isCtxMethod(sel, stack):
		rf.notef(sel.Sel.Pos(), "Redirect takes no arguments in Fiber v3%s", reviewHint("Ctx.Redirect"))
	case reviewRemovedRouterMethod(name) && rf.isRouterExpr(sel.X, stack, 0):
		rf.notef(sel.Sel.Pos(), "%s was removed in Fiber v3%s", name, reviewHint("App."+name))
	case slices.Contains(reviewRemovedMethods[reviewCorePath+".Hooks"], name) && rf.isHooksExpr(sel.X, stack, 0):
		rf.notef(sel.Sel.Pos(), "Hooks().%s was removed in Fiber v3%s", name, reviewHint("Hooks."+name))
	}
}

// isCtxMethod reports whether the method sel selects is one of the fiber request
// context. Besides the variables declared as a context, that is a value of a type
// that embeds the context, as long as the type does not declare a method or a
// field of that name itself.
func (rf *reviewFile) isCtxMethod(sel *ast.SelectorExpr, stack []ast.Node) bool {
	if isFiberCtxExpr(sel.X, rf.core, stack) {
		return true
	}
	if rf.env == nil {
		rf.env = rf.r.run.newEnv(rf.fi, rf.g, rf.core)
	}
	return rf.env.receiverKind(sel, stack) == ctxYes
}

// wrapperHint explains what to do when the receiver is a type that embeds the
// context and not the context itself: the replacement of a removed method has to
// be called on the embedded context when the type declares a method of its name
// (a wrapper with a Redirect(to string) of its own hides the Redirect() of v3).
func (rf *reviewFile) wrapperHint(sel *ast.SelectorExpr, stack []ast.Node) string {
	if isFiberCtxExpr(sel.X, rf.core, stack) || rf.env.isCtxAlias(ctxDerefType(rf.env.typeOf(sel.X, stack)), nil) {
		return ""
	}
	recv := rf.g.text(sel.X)
	embedded := "the embedded context"
	if path := rf.env.embeddedCtxOf(sel.X, stack); path != "" {
		embedded = recv + "." + path
	}
	return "; " + recv + " is not a fiber.Ctx but embeds one, so if it declares a method of the replacement's name, call the replacement on " + embedded
}

// isStringLiteral reports whether e is a string literal.
func isStringLiteral(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	return ok && lit.Kind == token.STRING
}

// reviewRemovedRouterMethod reports whether v3 removed the method from the app,
// groups and routers.
func reviewRemovedRouterMethod(name string) bool {
	for _, typ := range []string{reviewAppType, reviewGroupType, reviewRouterType} {
		if slices.Contains(reviewRemovedMethods[reviewCorePath+"."+typ], name) {
			return true
		}
	}
	return false
}

// The types of the core package that carry removed methods.
const (
	reviewAppType    = "App"
	reviewGroupType  = "Group"
	reviewRouterType = "Router"
)

// reviewMaxDepth bounds how far the receiver checks follow variables.
const reviewMaxDepth = 4

// isRouterExpr reports whether e is a fiber app, group or router.
func (rf *reviewFile) isRouterExpr(e ast.Expr, stack []ast.Node, depth int) bool {
	if depth > reviewMaxDepth {
		return false
	}

	switch x := e.(type) {
	case *ast.ParenExpr:
		return rf.isRouterExpr(x.X, stack, depth+1)
	case *ast.Ident:
		d, ok := resolveIdent(x.Name, x.Pos(), stack)
		if !ok {
			return false
		}
		if d.typ != nil {
			return isFiberType(d.typ, rf.core, reviewAppType) || isFiberType(d.typ, rf.core, reviewGroupType) || isFiberType(d.typ, rf.core, reviewRouterType)
		}
		return d.init != nil && rf.isRouterExpr(d.init, stack, depth+1)
	case *ast.CallExpr:
		sel, ok := x.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		if id, isIdent := sel.X.(*ast.Ident); isIdent && slices.Contains(rf.core, id.Name) {
			return sel.Sel.Name == mw2New || sel.Sel.Name == "NewWithCustomCtx"
		}
		return sel.Sel.Name == reviewGroupType && rf.isRouterExpr(sel.X, stack, depth+1)
	}
	return false
}

// isHooksExpr reports whether e is the hooks of a fiber app.
func (rf *reviewFile) isHooksExpr(e ast.Expr, stack []ast.Node, depth int) bool {
	if depth > reviewMaxDepth {
		return false
	}

	switch x := e.(type) {
	case *ast.ParenExpr:
		return rf.isHooksExpr(x.X, stack, depth+1)
	case *ast.Ident:
		d, ok := resolveIdent(x.Name, x.Pos(), stack)
		if !ok {
			return false
		}
		if d.typ != nil {
			return isFiberType(d.typ, rf.core, "Hooks")
		}
		return d.init != nil && rf.isHooksExpr(d.init, stack, depth+1)
	case *ast.CallExpr:
		sel, ok := x.Fun.(*ast.SelectorExpr)
		return ok && sel.Sel.Name == nameHooks && rf.isRouterExpr(sel.X, stack, depth+1)
	}
	return false
}

// reviewHint returns the hint that goes with a removed identifier, prefixed with
// ": ", or "" when there is none.
func reviewHint(qualified string) string {
	if hint, ok := reviewAdvice[qualified]; ok {
		return ": " + hint
	}
	if short, isCore := strings.CutPrefix(qualified, reviewCorePath+"."); isCore {
		if hint, ok := reviewAdvice[short]; ok {
			return ": " + hint
		}
	}
	return ""
}

// reviewClientHint is the advice for the Fiber v2 Agent API.
const (
	reviewClientHint    = "the HTTP client moved to github.com/gofiber/fiber/v3/client"
	reviewBinderHint    = "it moved to github.com/gofiber/fiber/v3/binder"
	reviewExtractorHint = "use Extractor with the extractors package"
)

// reviewAdvice maps removed identifiers to what replaces them, by the import
// path of the v3 package and the name ("path.Name", "path.Type.Field"), or for
// methods of the core package by "Type.Method".
var reviewAdvice = map[string]string{
	"Ctx.AllParams":       "use c.Bind().URI(&params)",
	"Ctx.BodyParser":      "use c.Bind().Body(&v)",
	"Ctx.CookieParser":    "use c.Bind().Cookie(&v)",
	"Ctx.ParamsInt":       `use fiber.Params[int](c, "name")`,
	"Ctx.ParamsParser":    "use c.Bind().URI(&v)",
	"Ctx.QueryBool":       `use fiber.Query[bool](c, "name")`,
	"Ctx.QueryFloat":      `use fiber.Query[float64](c, "name")`,
	"Ctx.QueryInt":        `use fiber.Query[int](c, "name")`,
	"Ctx.QueryParser":     "use c.Bind().Query(&v)",
	"Ctx.RedirectBack":    "use c.Redirect().Back(fallback)",
	"Ctx.RedirectToRoute": "use c.Redirect().Route(name, config)",
	"Ctx.ReqHeaderParser": "use c.Bind().Header(&v)",
	"Ctx.UserContext":     "the Ctx itself implements context.Context, pass c where a context is needed",
	"Ctx.SetUserContext":  "use c.SetContext, or context.WithValue on c",
	"Ctx.Redirect":        "use c.Redirect().To(url) and Status(code)",
	"App.Mount":           "use app.Use(prefix, subApp)",
	"App.Static":          "use the static middleware, github.com/gofiber/fiber/v3/middleware/static",
	"App.ListenTLS":       "use app.Listen with fiber.ListenConfig{CertFile, CertKeyFile}",
	"Hooks.OnShutdown":    "use OnPreShutdown or OnPostShutdown",

	reviewCorePath + ".Static":           "use the static middleware, github.com/gofiber/fiber/v3/middleware/static",
	reviewCorePath + ".ParserConfig":     reviewBinderHint,
	reviewCorePath + ".ParserType":       reviewBinderHint,
	reviewCorePath + ".SetParserDecoder": reviewBinderHint,

	reviewV3Prefix + "middleware/proxy.New":                "use proxy.Balancer",
	reviewV3Prefix + "middleware/proxy.WithTlsConfig":      "set proxy.Config.TLSConfig",
	reviewV3Prefix + "middleware/proxy.Config.TlsConfig":   "it is TLSConfig now",
	reviewV3Prefix + "middleware/timeout.NewWithContext":   "use timeout.New(handler, timeout.Config{Timeout: d})",
	reviewV3Prefix + "middleware/limiter.LimiterHandler":   "use limiter.Handler",
	reviewV3Prefix + "middleware/logger.TagHeader":         "use logger.TagReqHeader",
	reviewV3Prefix + "middleware/logger.Config.Output":     "it is Stream now",
	reviewV3Prefix + "middleware/csrf.Config.Expiration":   "it is IdleTimeout now",
	reviewV3Prefix + "middleware/csrf.Config.KeyLookup":    reviewExtractorHint,
	reviewV3Prefix + "middleware/csrf.Config.TokenLookup":  reviewExtractorHint,
	reviewV3Prefix + "middleware/session.Config.KeyLookup": reviewExtractorHint,
	reviewV3Prefix + "middleware/keyauth.Config.KeyLookup": reviewExtractorHint,
}

func init() {
	// The client types and helpers of the core package share one hint.
	for name := range strings.FieldsSeq(`AcquireAgent AcquireArgs AcquireClient AcquireFormFile AcquireResponse Agent Args Client
		Delete FormFile Get Head Patch Post Put ReleaseAgent ReleaseArgs ReleaseClient ReleaseFormFile ReleaseResponse
		Request Response RetryIfFunc`) {
		reviewAdvice[reviewCorePath+"."+name] = reviewClientHint
	}
	for _, name := range []string{nameListenMutualTLS, nameListenMutualTLSWithCertificate, nameListenTLSWithCertificate} {
		reviewAdvice["App."+name] = reviewAdvice["App.ListenTLS"]
	}
	for _, typ := range []string{reviewGroupType, reviewRouterType} {
		reviewAdvice[typ+".Mount"] = reviewAdvice["App.Mount"]
		reviewAdvice[typ+".Static"] = reviewAdvice["App.Static"]
	}
}
