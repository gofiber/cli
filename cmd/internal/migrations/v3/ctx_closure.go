package v3

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"
)

// ctxIsPure reports whether e can be evaluated later, or more than once, and
// still mean the same: it calls nothing but conversions and the builtins that
// only look at their arguments, and it receives from no channel. A rewrite moves
// such an operand into a function literal as it is; any other operand is
// evaluated where it was written and handed to the literal as an argument.
func ctxIsPure(e ast.Expr) bool {
	switch x := e.(type) {
	case nil:
		return true
	case *ast.Ident, *ast.BasicLit, *ast.FuncLit:
		return true
	case *ast.ParenExpr:
		return ctxIsPure(x.X)
	case *ast.SelectorExpr:
		return ctxIsPure(x.X)
	case *ast.StarExpr:
		return ctxIsPure(x.X)
	case *ast.UnaryExpr:
		return x.Op != token.ARROW && ctxIsPure(x.X)
	case *ast.BinaryExpr:
		return ctxIsPure(x.X) && ctxIsPure(x.Y)
	case *ast.IndexExpr:
		return ctxIsPure(x.X) && ctxIsPure(x.Index)
	case *ast.SliceExpr:
		return ctxIsPure(x.X) && ctxIsPure(x.Low) && ctxIsPure(x.High) && ctxIsPure(x.Max)
	case *ast.KeyValueExpr:
		return ctxIsPure(x.Key) && ctxIsPure(x.Value)
	case *ast.CompositeLit:
		for _, elt := range x.Elts {
			if !ctxIsPure(elt) {
				return false
			}
		}
		return true
	case *ast.CallExpr:
		return ctxIsPureCall(x)
	}
	return false
}

// ctxIsPureCall reports whether call is a conversion to a predeclared type or a
// call of a builtin that only looks at its arguments, with arguments that are
// pure themselves.
func ctxIsPureCall(call *ast.CallExpr) bool {
	if call.Ellipsis.IsValid() {
		return false
	}

	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		switch fun.Name {
		case "len", "cap", "min", "max":
		default:
			if !ctxIsPredeclaredType(fun.Name) {
				return false
			}
		}
	case *ast.ArrayType:
		// []byte(s)
	default:
		return false
	}

	for _, arg := range call.Args {
		if !ctxIsPure(arg) {
			return false
		}
	}
	return true
}

// ctxNamer hands out names for the parameters and the variables of a function
// literal that a rewrite writes, so that none of them hides a name the operands
// of the rewritten call use.
type ctxNamer map[string]bool

// newCtxNamer returns a namer that stays clear of every name the expressions
// refer to (the field behind a selector is no name in scope) and of the extra
// names.
func newCtxNamer(exprs []ast.Expr, extra ...string) ctxNamer {
	namer := ctxNamer{}
	for _, e := range exprs {
		namer.collect(e)
	}
	for _, name := range extra {
		namer[name] = true
	}
	return namer
}

// collect takes the names e refers to.
func (n ctxNamer) collect(e ast.Expr) {
	if e == nil {
		return
	}
	ast.Inspect(e, func(node ast.Node) bool {
		switch x := node.(type) {
		case *ast.Ident:
			n[x.Name] = true
		case *ast.SelectorExpr:
			n.collect(x.X)
			return false
		}
		return true
	})
}

// fresh returns base, or base followed by a number when the name is taken, and
// takes the name.
func (n ctxNamer) fresh(base string) string {
	name := base
	for i := 2; n[name]; i++ {
		name = base + strconv.Itoa(i)
	}
	n[name] = true
	return name
}

// The types and names that the parameters of a function literal are given.
const (
	ctxLitString = "string"
	ctxLitInt    = "int"
	ctxLitInts   = "...int"
	ctxLitKey    = "key"
	ctxLitError  = "error"
)

// ctxOperand is a value that the call being rewritten evaluates before it runs.
type ctxOperand struct {
	// expr is the operand as it was written, which tells whether it is pure.
	expr ast.Expr
	// text is the operand as the output spells it (with the rewrites inside it
	// applied); for a variadic operand the arguments, comma separated.
	text string
	// typ is the type of the parameter that carries the operand into the literal.
	typ string
	// name is the name the parameter is given when it can.
	name string
	// spread is set for a slice that is passed on to a variadic parameter.
	spread bool
	// unused is set for an operand the body does not refer to, which is only
	// evaluated.
	unused bool
}

// ctxLiteral builds an immediately called function literal around a rewritten
// call. An operand that is pure stays in the body where it is used; any other is
// evaluated at the call of the literal, in the order the operands were written,
// and reaches the body through a parameter.
type ctxLiteral struct {
	namer  ctxNamer
	params []string
	args   []string
}

// bind adds an operand to the literal and returns how the body refers to it:
// its text, or the name of the parameter that carries it. It is empty for an
// operand that is pure and not used.
func (l *ctxLiteral) bind(op ctxOperand) string {
	if op.expr != nil && ctxIsPure(op.expr) {
		if op.unused {
			return ""
		}
		return op.text
	}

	name := "_"
	if !op.unused {
		name = l.namer.fresh(op.name)
	}
	arg := op.text
	if op.spread {
		arg += "..."
	}
	l.params = append(l.params, name+" "+op.typ)
	l.args = append(l.args, arg)
	return name
}

// text returns the literal with the results and the body, and the call of it.
func (l *ctxLiteral) text(results, body string) string {
	return "func(" + strings.Join(l.params, ", ") + ") " + results + " {\n" + body + "\n}(" + strings.Join(l.args, ", ") + ")"
}
