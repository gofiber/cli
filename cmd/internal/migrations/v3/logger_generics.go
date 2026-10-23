package v3

import (
	"fmt"
	"go/ast"
	"regexp"
	"slices"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// MigrateLoggerGenerics moves the log package to its generic API. AllLogger,
// ConfigurableLogger (v2's ControlLogger) and DefaultLogger take the type of the
// value Logger() returns, which is a second method custom loggers have to
// provide, and WithContext takes any instead of a context.Context.
//
//   - The type argument is the one the project's own logger returns from Logger();
//     a logger that has no such method gets one that returns nil as any. The
//     built-in logger returns a *log.Logger. DefaultLogger[T]() answers nil
//     unless T is that type, so a guess would hand out a nil logger.
//   - SetLogger and LoggerToWriter infer the type argument, so none is written.
//   - A WithContext that took a context.Context takes any, and gets the context
//     back with an assertion when its body uses it.
func MigrateLoggerGenerics(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	loggers := newMW2Loggers(cwd)
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return migrateLoggerGenericsContent(content, fi, loggers)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate logger generics: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating logger generics")
	return nil
}

// The standard library packages the rewrites refer to.
var (
	mw2StdLog     = regexp.MustCompile(`^log$`)
	mw2StdContext = regexp.MustCompile(`^context$`)
)

// mw2GenericLoggers are the interfaces of the log package that became generic.
var mw2GenericLoggers = map[string]string{
	"AllLogger":          "AllLogger",
	"ConfigurableLogger": mw2ConfigurableLogger,
	"ControlLogger":      mw2ConfigurableLogger,
}

// mw2ConfigurableLogger is the generic interface that took the place of v2's ControlLogger.
const mw2ConfigurableLogger = "ConfigurableLogger"

func migrateLoggerGenericsContent(content string, fi internal.FileInfo, loggers *mw2Loggers) string {
	if !strings.Contains(content, "Logger") && !strings.Contains(content, "WithContext") {
		return content
	}
	names := importNames(content, mw2Log, "log")
	if len(names) == 0 {
		return content
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return content
	}

	r := &mw2LoggerRewriter{g: g, fi: fi, names: names, loggers: loggers}
	g.walk(r.visit)
	if len(r.edits.edits) == 0 {
		return content
	}
	return mw2AddImports(r.edits.apply(content), r.imports...)
}

// mw2LoggerRewriter rewrites the logger API of one file.
type mw2LoggerRewriter struct {
	g       *goSource
	loggers *mw2Loggers
	stdName string
	edits   editSet
	// names are the identifiers the file binds to the log package.
	names []string
	// imports are the imports the output needs.
	imports []mw2Import
	fi      internal.FileInfo
}

func (r *mw2LoggerRewriter) visit(n ast.Node, stack []ast.Node) bool {
	switch x := n.(type) {
	case *ast.FuncDecl:
		r.withContext(x)
	case *ast.SelectorExpr:
		r.genericType(x, stack)
	case *ast.CallExpr:
		r.defaultLogger(x)
	}
	return true
}

// genericType instantiates a log interface that is generic in v3.
func (r *mw2LoggerRewriter) genericType(sel *ast.SelectorExpr, stack []ast.Node) {
	replacement, ok := mw2GenericLoggers[sel.Sel.Name]
	if !ok || !mw2IsSelector(sel, r.names, sel.Sel.Name) {
		return
	}
	if len(stack) > 0 {
		if idx, ok := stack[len(stack)-1].(*ast.IndexExpr); ok && idx.X == ast.Expr(sel) {
			return // already instantiated
		}
	}

	arg, ok := r.typeArgument(sel, stack)
	if !ok {
		return
	}
	r.edits.replaceNode(r.g, sel, r.g.text(sel.X)+"."+replacement+"["+arg+"]")
}

// typeArgument finds the type argument for a use of a generic log interface. A
// declaration that names the implementing type, var _ log.AllLogger = (*T)(nil),
// takes the type that T's Logger method returns, anything else the project's.
func (r *mw2LoggerRewriter) typeArgument(sel *ast.SelectorExpr, stack []ast.Node) (string, bool) {
	if spec := mw2EnclosingValueSpec(sel, stack); spec != nil {
		if impl := r.implementer(spec); impl != nil {
			return r.render(impl.result)
		}
	}
	return r.projectArgument(sel)
}

// mw2EnclosingValueSpec returns the declaration whose type is sel.
func mw2EnclosingValueSpec(sel *ast.SelectorExpr, stack []ast.Node) *ast.ValueSpec {
	if len(stack) == 0 {
		return nil
	}
	spec, ok := stack[len(stack)-1].(*ast.ValueSpec)
	if !ok || spec.Type != ast.Expr(sel) {
		return nil
	}
	return spec
}

// implementer returns the custom logger a declaration like
// var _ log.AllLogger = (*T)(nil) or = &T{} asserts.
func (r *mw2LoggerRewriter) implementer(spec *ast.ValueSpec) *mw2LoggerType {
	if len(spec.Values) != 1 {
		return nil
	}
	name := mw2ImplementerName(spec.Values[0])
	if name == "" {
		return nil
	}
	return r.loggers.find(r.fi.Path, r.g.file.Name.Name, name)
}

// mw2ImplementerName finds the type in (*T)(nil), &T{} and T{}.
func mw2ImplementerName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.CallExpr:
		if len(x.Args) == 1 {
			if paren, ok := x.Fun.(*ast.ParenExpr); ok {
				return mw2ImplementerName(paren.X)
			}
		}
	case *ast.StarExpr:
		return mw2ImplementerName(x.X)
	case *ast.UnaryExpr:
		return mw2ImplementerName(x.X)
	case *ast.CompositeLit:
		return mw2ImplementerName(x.Type)
	case *ast.Ident:
		return x.Name
	}
	return ""
}

// projectArgument is the type argument of the logger the project registers: the
// one its custom logger returns from Logger(), or the *log.Logger of the built-in
// logger when the project installs no logger of its own.
func (r *mw2LoggerRewriter) projectArgument(at ast.Node) (string, bool) {
	m := r.loggers.module(r.fi.Path)
	if !m.setsLogger {
		return r.stdLoggerType(), true
	}
	if len(m.types) > 0 {
		if arg, ok := mw2UniqueResult(m.types); ok {
			return r.render(arg)
		}
	}

	internal.Notef(r.fi, r.g.line(at.Pos()), "the logger installed with log.SetLogger is not one of the project's own, or the project has several whose Logger() return different types; write the type argument of log.AllLogger, log.ConfigurableLogger and log.DefaultLogger (it is what the logger's Logger() returns) by hand")
	return "", false
}

// mw2UniqueResult returns the Logger() result that all custom loggers share.
func mw2UniqueResult(custom []*mw2LoggerType) (*mw2LoggerResult, bool) {
	first := custom[0].result
	for _, c := range custom[1:] {
		if c.result.text != first.text {
			return nil, false
		}
	}
	return first, true
}

// render spells a type for this file, importing what it needs.
func (r *mw2LoggerRewriter) render(res *mw2LoggerResult) (string, bool) {
	r.imports = append(r.imports, res.imports...)
	return res.text, true
}

// stdLoggerType spells *log.Logger of the standard library for this file.
func (r *mw2LoggerRewriter) stdLoggerType() string {
	if r.stdName == "" {
		r.stdName = mw2FirstName(r.g.src, mw2StdLog, "log")
	}
	if r.stdName == "" {
		r.stdName = "stdlog"
		for mw2NameTaken(r.g, nil, r.stdName) {
			r.stdName = "go" + r.stdName
		}
		r.imports = append(r.imports, mw2Import{alias: r.stdName, path: "log"})
	}
	return "*" + r.stdName + ".Logger"
}

// defaultLogger rewrites log.DefaultLogger() to log.DefaultLogger[T]().
func (r *mw2LoggerRewriter) defaultLogger(call *ast.CallExpr) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !mw2IsSelector(sel, r.names, "DefaultLogger") || len(call.Args) != 0 {
		return
	}

	arg, ok := r.projectArgument(call)
	if !ok {
		return
	}
	r.edits.replace(r.g.offset(sel.End()), r.g.offset(sel.End()), "["+arg+"]")
}

// withContext moves a WithContext method of a custom logger to the v3 signature
// WithContext(ctx any) and gives the logger the Logger method v3 requires.
func (r *mw2LoggerRewriter) withContext(fn *ast.FuncDecl) {
	if !r.isCustomWithContext(fn) {
		return
	}

	if r.takesContext(fn) {
		r.convertParam(fn)
	}
	if r.needsLoggerMethod(fn) {
		r.addLoggerMethod(fn)
	}
}

// isCustomWithContext reports whether fn is the WithContext method of a logger:
// it returns the log package's CommonLogger.
func (r *mw2LoggerRewriter) isCustomWithContext(fn *ast.FuncDecl) bool {
	if fn.Recv == nil || fn.Name.Name != "WithContext" || fn.Body == nil {
		return false
	}
	if fn.Type.Params == nil || len(fn.Type.Params.List) != 1 || len(fn.Type.Params.List[0].Names) > 1 {
		return false
	}
	res := fn.Type.Results
	return res != nil && len(res.List) == 1 && isFiberType(res.List[0].Type, r.names, "CommonLogger")
}

// takesContext reports whether the parameter of fn is a context.Context.
func (r *mw2LoggerRewriter) takesContext(fn *ast.FuncDecl) bool {
	sel, ok := fn.Type.Params.List[0].Type.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Context" {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && slices.Contains(importNames(r.g.src, mw2StdContext, "context"), id.Name)
}

// convertParam changes the context parameter of fn to any. A body that uses the
// context gets it back from the new parameter.
func (r *mw2LoggerRewriter) convertParam(fn *ast.FuncDecl) {
	param := fn.Type.Params.List[0]
	typ := param.Type
	r.edits.replaceNode(r.g, typ, "any")

	if len(param.Names) == 0 || param.Names[0].Name == "_" || !mw2Uses(fn.Body, param.Names[0].Name) {
		return
	}

	name := param.Names[0].Name
	renamed := name + "Any"
	r.edits.replaceNode(r.g, param.Names[0], renamed)
	r.edits.replace(r.g.offset(fn.Body.Lbrace)+1, r.g.offset(fn.Body.Lbrace)+1,
		"\n"+name+", _ := "+renamed+".("+r.g.text(typ)+")")
}

// mw2Uses reports whether body mentions the identifier name.
func mw2Uses(body *ast.BlockStmt, name string) bool {
	used := false
	ast.Inspect(body, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == name {
			used = true
		}
		return !used
	})
	return used
}

// needsLoggerMethod reports whether the logger type of fn has no Logger method.
func (r *mw2LoggerRewriter) needsLoggerMethod(fn *ast.FuncDecl) bool {
	name := mw2ReceiverName(fn)
	if name == "" {
		return false
	}
	t := r.loggers.find(r.fi.Path, r.g.file.Name.Name, name)
	return t != nil && t.stub
}

// addLoggerMethod adds Logger() any after the WithContext method.
func (r *mw2LoggerRewriter) addLoggerMethod(fn *ast.FuncDecl) {
	recv := r.g.text(fn.Recv)
	at := r.g.offset(fn.End())
	r.edits.replace(at, at, "\n\n// TODO(fiber migrate): v3's log.AllLogger[T] requires Logger() T, which returns the logger you wrap.\n"+
		"func "+recv+" Logger() any { return nil }")
}

// mw2ReceiverName returns the name of the type fn is a method of.
func mw2ReceiverName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return ""
	}
	t := fn.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}
