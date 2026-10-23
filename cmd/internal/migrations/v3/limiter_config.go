package v3

import (
	"fmt"
	"go/ast"
	"go/token"
	"slices"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// mw2LimiterRenames maps the deprecated v2 fields of the limiter config to the
// fields that replaced them, which are the only ones v3 has.
var mw2LimiterRenames = map[string]string{
	nameDuration: cacheExpiration,
	cacheStore:   "Storage",
	cacheKey:     "KeyGenerator",
}

// MigrateLimiterConfig moves a limiter setup to the v3 API: the deprecated
// Duration, Store and Key fields become Expiration, Storage and KeyGenerator, the
// LimiterHandler interface is called Handler and takes a *Config, and so do the
// New methods of FixedWindow and SlidingWindow. Only the limiter's own config
// literals, variables and types are touched, found through the import path.
func MigrateLimiterConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContentInfo(cwd, migrateLimiterContent)
	if err != nil {
		return fmt.Errorf("failed to migrate limiter configs: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating limiter middleware configs")
	return nil
}

func migrateLimiterContent(content string, fi internal.FileInfo) string {
	names := importNames(content, mw2Limiter, "limiter")
	if len(names) == 0 {
		return content
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return content
	}

	r := &mw2LimiterRewriter{g: g, fi: fi, names: names, fiber: fiberNames(content)}
	g.walk(r.visit)
	return r.edits.apply(content)
}

// mw2LimiterRewriter rewrites the limiter API of one file.
type mw2LimiterRewriter struct {
	g     *goSource
	edits editSet
	// names are the identifiers the file binds to the limiter package, fiber those it binds to the core package.
	names []string
	fiber []string
	fi    internal.FileInfo
}

func (r *mw2LimiterRewriter) visit(n ast.Node, stack []ast.Node) bool {
	switch x := n.(type) {
	case *ast.CompositeLit:
		r.literal(x)
	case *ast.SelectorExpr:
		r.selector(x, stack)
	case *ast.CallExpr:
		r.windowNew(x, stack)
	case *ast.FuncDecl:
		r.customHandler(x)
	}
	return true
}

// literal renames the deprecated keys of a limiter.Config literal.
func (r *mw2LimiterRewriter) literal(n *ast.CompositeLit) {
	lit, ok := mw2ConfigLit(n, r.names)
	if !ok || !mw2IsKeyed(lit) {
		return
	}

	for old, replacement := range mw2LimiterRenames {
		_, kv := mw2Field(lit, old)
		if kv == nil {
			continue
		}
		if _, other := mw2Field(lit, replacement); other != nil {
			internal.Notef(r.fi, r.g.line(kv.Pos()),
				"limiter.Config sets both %s and %s; v2 let the deprecated %s win. Left as it is, merge them into %s by hand", old, replacement, old, replacement)
			continue
		}
		r.edits.replaceNode(r.g, kv.Key, replacement)
	}
}

// selector renames the deprecated fields of a limiter config variable and the
// LimiterHandler interface.
func (r *mw2LimiterRewriter) selector(sel *ast.SelectorExpr, stack []ast.Node) {
	if mw2IsSelector(sel, r.names, "LimiterHandler") {
		r.edits.replaceNode(r.g, sel.Sel, "Handler")
		return
	}

	replacement, ok := mw2LimiterRenames[sel.Sel.Name]
	if !ok || !mw2IsTyped(sel.X, r.names, mw2ConfigType, stack, 0) {
		return
	}
	r.edits.replaceNode(r.g, sel.Sel, replacement)
}

// windowNew hands the config of limiter.FixedWindow{}.New and
// limiter.SlidingWindow{}.New over by pointer, the way v3 takes it.
func (r *mw2LimiterRewriter) windowNew(call *ast.CallExpr, stack []ast.Node) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != mw2New || len(call.Args) != 1 || call.Ellipsis.IsValid() {
		return
	}
	recv, ok := sel.X.(*ast.CompositeLit)
	if !ok || recv.Type == nil || (!isFiberType(recv.Type, r.names, "FixedWindow") && !isFiberType(recv.Type, r.names, "SlidingWindow")) {
		return
	}

	arg := call.Args[0]
	switch x := arg.(type) {
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			return
		}
	case *ast.Ident:
		// The parameter of a custom algorithm turns into a pointer itself.
		if !r.isHandlerParam(x, stack) {
			r.edits.replace(r.g.offset(arg.Pos()), r.g.offset(arg.Pos()), "&")
		}
		return
	case *ast.CompositeLit:
		r.edits.replace(r.g.offset(arg.Pos()), r.g.offset(arg.Pos()), "&")
		return
	}
	internal.Notef(r.fi, r.g.line(call.Pos()),
		"%s.New takes a *limiter.Config in v3 and %s cannot be addressed; store it in a variable and pass &variable",
		r.g.text(sel.X), r.g.text(arg))
}

// customHandler turns the New method of a user-defined limiter algorithm, the
// implementation of v2's LimiterHandler, to the pointer parameter v3's Handler has.
func (r *mw2LimiterRewriter) customHandler(fn *ast.FuncDecl) {
	param, ok := r.handlerParam(fn)
	if !ok {
		return
	}

	r.edits.replace(r.g.offset(param.Type.Pos()), r.g.offset(param.Type.Pos()), "*")
	if len(param.Names) == 1 && fn.Body != nil && mw2UsedByValue(fn.Body, param.Names[0].Name) {
		internal.Notef(r.fi, r.g.line(param.Pos()),
			"%s is a *limiter.Config in v3 and is used here as a value; dereference it (*%s) where a limiter.Config is expected",
			param.Names[0].Name, param.Names[0].Name)
	}
}

// handlerParam returns the config parameter of fn when fn is the New method of a
// limiter algorithm written for v2: it takes a limiter.Config and returns a
// fiber.Handler.
func (r *mw2LimiterRewriter) handlerParam(fn *ast.FuncDecl) (*ast.Field, bool) {
	if fn.Recv == nil || fn.Name.Name != mw2New || fn.Type.Params == nil || len(fn.Type.Params.List) != 1 {
		return nil, false
	}
	param := fn.Type.Params.List[0]
	if len(param.Names) > 1 || !isFiberType(param.Type, r.names, "Config") {
		return nil, false
	}
	if _, isPointer := param.Type.(*ast.StarExpr); isPointer || !r.returnsHandler(fn) {
		return nil, false
	}
	return param, true
}

// isHandlerParam reports whether id names the config parameter of the limiter
// algorithm that contains it.
func (r *mw2LimiterRewriter) isHandlerParam(id *ast.Ident, stack []ast.Node) bool {
	for _, n := range slices.Backward(stack) {
		fn, ok := n.(*ast.FuncDecl)
		if !ok {
			continue
		}
		param, ok := r.handlerParam(fn)
		return ok && len(param.Names) == 1 && param.Names[0].Name == id.Name
	}
	return false
}

// returnsHandler reports whether fn returns a fiber.Handler.
func (r *mw2LimiterRewriter) returnsHandler(fn *ast.FuncDecl) bool {
	res := fn.Type.Results
	if res == nil || len(res.List) != 1 {
		return false
	}
	return isFiberType(res.List[0].Type, r.fiber, "Handler")
}

// mw2IsWindowNew reports whether call is X{}.New(...) on a composite literal,
// which is how the limiter algorithms are called.
func mw2IsWindowNew(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != mw2New {
		return false
	}
	_, ok = sel.X.(*ast.CompositeLit)
	return ok
}

// mw2UsedByValue reports whether the identifier name is used in body other than
// to select a field of it, which works through a pointer as well.
func mw2UsedByValue(body *ast.BlockStmt, name string) bool {
	if name == "_" {
		return false
	}

	byValue := false
	var stack []ast.Node
	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if id, ok := n.(*ast.Ident); ok && id.Name == name && len(stack) > 0 {
			switch parent := stack[len(stack)-1].(type) {
			case *ast.SelectorExpr:
				byValue = byValue || parent.X != n
			case *ast.CallExpr:
				// FixedWindow{}.New(cfg) takes the pointer.
				byValue = byValue || !mw2IsWindowNew(parent)
			default:
				byValue = true
			}
		}
		stack = append(stack, n)
		return true
	})
	return byValue
}
