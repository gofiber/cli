package v3

import (
	"fmt"
	"go/ast"
	"strings"
)

// A method of the v2 request context that Fiber v3 does not have in the same form
// can be used as a value, without calling it: back := c.RedirectBack,
// parse(c.QueryInt), handlers{redirect: c.Redirect}. The value becomes a function
// literal with the signature the method had, whose body does what the rewritten
// call does, so every use of the value goes on working.
//
// Go binds a method value to its receiver where the value is made: the receiver
// expression is evaluated once, then, and a later change of the variable it names
// does not reach the value. The literal gets its context the same way. A variable
// that nothing assigns again is used by the literal itself; any other receiver
// (c.Status(200), s.ctx, a call) is evaluated where the value is made and handed
// to a literal that returns the function:
//
//	back := c.RedirectBack
//	back := func(fallback string, status ...int) error { ... c.Redirect().Status(code).Back(fallback) }
//
//	back := s.ctx.RedirectBack
//	back := func(c fiber.Ctx) func(fallback string, status ...int) error {
//	    return func(fallback string, status ...int) error { ... c.Redirect().Status(code).Back(fallback) }
//	}(s.ctx)
//
// A type that embeds the context hands the embedded one over, because the literal
// needs a fiber.Ctx and the type may declare methods that make it none, and
// because the method of v2 used the members of the context and not those of the
// type that embeds it. A method expression, (*fiber.Ctx).RedirectBack, gets the
// context as its first parameter.

// ctxValueRecv is how the function literal of a method value reaches the context.
type ctxValueRecv struct {
	// name is the name the body of the literal uses for the context.
	name string
	// typ is the type of the parameter that takes the context, when there is one.
	typ string
	// path is the embedded field to go through, when the receiver is a type that
	// embeds the context.
	path string
	// bound is set when the receiver is evaluated where the method value is made
	// and handed to the literal; a variable that nothing assigns again is used by
	// name instead.
	bound bool
	// expr is set for a method expression, which takes the context as its first
	// parameter.
	expr bool
}

// ctxValue is a function literal: its parameters ("name type"), its results as
// they are written after the parameters, and the statements of its body.
type ctxValue struct {
	results string
	body    string
	params  []string
}

// ctxValueBuilder writes the literal of one method value. Its names stay clear of
// the ones the receiver and the packages the body refers to use.
type ctxValueBuilder struct {
	env   *ctxEnv
	sel   *ast.SelectorExpr
	namer ctxNamer
	recv  ctxValueRecv
}

// newValueBuilder prepares the literal for the method value sel, whose ancestors
// are stack; extra are the names of the packages (strconv, fmt) the body refers
// to. It looks at the code around the method value now, because the walk moves
// on; the literal is written later, when the edits of the code inside the
// receiver are known (see text).
func (env *ctxEnv) newValueBuilder(sel *ast.SelectorExpr, stack []ast.Node, extra ...string) *ctxValueBuilder {
	// The body refers to the packages and, when the literal uses a variable of the
	// code around it, to that variable: the names of the literal stay clear of them.
	// A receiver that is handed over is evaluated outside the literal.
	namer := newCtxNamer(nil, append(ctxFiberNames(env), extra...)...)
	b := &ctxValueBuilder{env: env, sel: sel, namer: namer}

	path := env.embeddedCtxOf(sel.X, stack)
	id, isIdent := ast.Unparen(sel.X).(*ast.Ident)
	switch {
	case env.isCtxType(sel.X):
		b.recv = ctxValueRecv{name: namer.fresh("c"), typ: env.qual(ctxTypeCtx), expr: true}
	case isIdent && path == "" && ctxVariableStable(id.Name, id.Pos(), stack):
		namer[id.Name] = true
		b.recv = ctxValueRecv{name: id.Name}
	default:
		b.recv = ctxValueRecv{name: namer.fresh("c"), typ: env.qual(ctxTypeCtx), path: path, bound: true}
	}
	return b
}

// isCtxType reports whether x is the fiber.Ctx type, the receiver of a method
// expression such as (*fiber.Ctx).RedirectBack, and not a value.
func (env *ctxEnv) isCtxType(x ast.Expr) bool {
	return ctxIsFiberType(ast.Unparen(x), env.names, ctxTypeCtx)
}

// name returns a name for a parameter or a variable of the literal.
func (b *ctxValueBuilder) name(base string) string { return b.namer.fresh(base) }

// text returns the code that replaces the method value. e holds the edits already
// scheduled, which a receiver expression that is moved carries along.
func (b *ctxValueBuilder) text(e *editSet, v ctxValue) string {
	r := b.recv
	params := v.params
	if r.expr {
		params = append([]string{r.name + " " + r.typ}, params...)
	}
	sig := "(" + strings.Join(params, ", ") + ")"
	if v.results != "" {
		sig += " " + v.results
	}

	if !r.bound {
		return "func" + sig + " {\n" + v.body + "\n}"
	}

	bind := ctxRenderNode(e, b.env.g, b.sel.X)
	if r.path != "" {
		bind += "." + r.path
	}
	return "func(" + r.name + " " + r.typ + ") func" + sig + " {\nreturn func" + sig + " {\n" + v.body + "\n}\n}(" + bind + ")"
}

// redirect writes Redirect, RedirectBack and RedirectToRoute: the status is the
// first of the ones given, 302 Found when there is none, as in v2.
func (b *ctxValueBuilder) redirect(method string) ctxValue {
	target := ctxRedirectTarget[method]
	status, code := b.name("status"), b.name("code")

	var params []string
	var setup, call string
	switch method {
	case ctxRedirect:
		to := b.name("location")
		params, call = []string{to + " " + ctxLitString}, to
	case ctxRedirectBack:
		fallback := b.name("fallback")
		params, call = []string{fallback + " " + ctxLitString}, fallback
	default:
		route, args := b.name("routeName"), b.name("params")
		cfg, queries, ok := b.name("cfg"), b.name("queries"), b.name("ok")
		params = []string{route + " " + ctxLitString, args + " " + b.env.qual("Map")}
		// v2 took the query string from the "queries" entry of the params
		setup = fmt.Sprintf("%[1]s := %[2]s{Params: %[3]s}\nif %[4]s, %[5]s := %[3]s[\"queries\"].(map[string]string); %[5]s {\n%[1]s.Queries = %[4]s\n}\n",
			cfg, b.env.qual("RedirectConfig"), args, queries, ok)
		call = route + ", " + cfg
	}

	body := fmt.Sprintf("%[1]s := %[2]s\nif len(%[3]s) > 0 {\n%[1]s = %[3]s[0]\n}\n%[4]sreturn %[5]s.Redirect().Status(%[1]s).%[6]s(%[7]s)",
		code, b.env.qual(ctxStatusFound), status, setup, b.recv.name, target.method, call)
	return ctxValue{params: append(params, status+" "+ctxLitInts), results: "error", body: body}
}

// paramsInt writes ParamsInt: the first default and no error when the parameter
// is no integer and there is a default, the error v2 returned otherwise.
func (b *ctxValueBuilder) paramsInt(strconvName, fmtName string) ctxValue {
	key, defaults, value, err := b.name("key"), b.name("defaultValue"), b.name("value"), b.name("err")
	body := fmt.Sprintf(`%[1]s, %[2]s := %[3]s.Atoi(%[4]s.Params(%[5]s))
if %[2]s != nil {
if len(%[6]s) > 0 {
return %[6]s[0], nil
}
return 0, %[7]s.Errorf("failed to convert: %%w", %[2]s)
}
return %[1]s, nil`, value, err, strconvName, b.recv.name, key, defaults, fmtName)
	return ctxValue{params: []string{key + " " + ctxLitString, defaults + " " + ctxLitInts}, results: "(int, error)", body: body}
}

// query writes QueryInt, QueryFloat and QueryBool.
func (b *ctxValueBuilder) query(helper ctxHelper) ctxValue {
	key, defaults := b.name("key"), b.name("defaultValue")
	body := fmt.Sprintf("return %s[%s](%s, %s, %s...)", b.env.qual(helper.fn), helper.typ, b.recv.name, key, defaults)
	return ctxValue{params: []string{key + " " + ctxLitString, defaults + " ..." + helper.typ}, results: helper.typ, body: body}
}

// parser writes a parser method for a method expression: the binder of v3 takes
// the place of the method.
func (b *ctxValueBuilder) parser(method string) ctxValue {
	out := b.name("out")
	return ctxValue{
		params:  []string{out + " interface{}"},
		results: "error",
		body:    fmt.Sprintf("return %s.Bind().%s(%s)", b.recv.name, ctxBinders[method], out),
	}
}

// rangeOf writes Range: v3 takes the size as an int64.
func (b *ctxValueBuilder) rangeOf() ctxValue {
	size := b.name("size")
	return ctxValue{
		params:  []string{size + " " + ctxLitInt},
		results: "(" + b.env.qual(ctxTypeRange) + ", error)",
		body:    fmt.Sprintf("return %s.Range(int64(%s))", b.recv.name, size),
	}
}

// allParams writes AllParams, which v3 does not have.
func (b *ctxValueBuilder) allParams() ctxValue {
	params, name := b.name("params"), b.name("name")
	return ctxValue{results: "map[string]string", body: ctxAllParamsBody(b.recv.name, params, name)}
}

// sendFile writes SendFile: the compress flag moves into the config, which also
// asks for the byte ranges v2 always served.
func (b *ctxValueBuilder) sendFile() ctxValue {
	file, compress, cfg := b.name("file"), b.name("compress"), b.name("cfg")
	body := fmt.Sprintf("%[1]s := %[2]s{ByteRange: true}\nif len(%[3]s) > 0 {\n%[1]s.Compress = %[3]s[0]\n}\nreturn %[4]s.SendFile(%[5]s, %[1]s)",
		cfg, b.env.qual("SendFile"), compress, b.recv.name, file)
	return ctxValue{params: []string{file + " " + ctxLitString, compress + " ...bool"}, results: "error", body: body}
}
