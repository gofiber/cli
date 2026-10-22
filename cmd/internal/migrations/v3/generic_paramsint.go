package v3

import (
	"go/ast"
	"go/token"
	"slices"
)

// ParamsInt returned an error, and v2 returned 0 with it, where strconv.Atoi returns
// the largest int for a number that does not fit. A call without a default is
// migrated by what the code does with the error:
//
//	v, _ := c.ParamsInt(k)       v := fiber.Params[int](c, k)         the error is dropped
//	v, err := c.ParamsInt(k)     v, err := strconv.Atoi(c.Params(k))  the next statement leaves
//	if err != nil { return … }                                        without looking at v
//	v, err = c.ParamsInt(k)      v, err = strconv.Atoi(c.Params(k))   any other statement
//	                             if err != nil { v = 0 }
//	return c.ParamsInt(k)        value, err := strconv.Atoi(c.Params(k))
//	                             if err != nil { return 0, err }
//	                             return value, nil
//	f(c.ParamsInt(k))            func() (int, error) { … }()          the literal returns 0 with the error
//
// Fiber v3 gives 0 for what it cannot read, which is the value v2 returned with the error.

// ctxErrUse is what the code does with the error of a ParamsInt call.
type ctxErrUse int

const (
	// ctxErrOther is any use that may read the value after an error.
	ctxErrOther ctxErrUse = iota
	// ctxErrIgnored is an error assigned to the blank identifier.
	ctxErrIgnored
	// ctxErrGuarded is an error that the code checks and leaves on, without reading
	// the value: the value after an error is of no interest.
	ctxErrGuarded
	// ctxErrReturned is a call whose results are the results of the function.
	ctxErrReturned
	// ctxErrReset is an assignment to a variable and the error, in a list of statements,
	// that the code goes on after.
	ctxErrReset
)

// ctxParamsErr describes the use of the error of a ParamsInt call.
type ctxParamsErr struct {
	// value is the variable of ctxErrReset.
	value ast.Expr
	// ret is the statement of ctxErrReturned.
	ret *ast.ReturnStmt
	// errName is the variable of ctxErrReset that holds the error.
	errName string
	use     ctxErrUse
	// from and to delimit what follows the value in the names or the left side:
	// ", _" of ctxErrIgnored.
	from, to token.Pos
}

// ctxParamsIntErr tells what the code does with the error of the ParamsInt call
// without a default, given the ancestors of the call.
func ctxParamsIntErr(call *ast.CallExpr, stack []ast.Node) ctxParamsErr {
	var value, errExpr ast.Expr
	var stmt ast.Stmt
	define := false
	// the position in the stack of the statement that holds the call
	at := len(stack) - 1

	switch parent := stack[at].(type) {
	case *ast.ReturnStmt:
		if len(parent.Results) == 1 && ctxResultCount(stack) == 2 {
			return ctxParamsErr{use: ctxErrReturned, ret: parent}
		}
		return ctxParamsErr{}
	case *ast.AssignStmt:
		if len(parent.Lhs) != 2 || len(parent.Rhs) != 1 || parent.Rhs[0] != call {
			return ctxParamsErr{}
		}
		value, errExpr, stmt, define = parent.Lhs[0], parent.Lhs[1], parent, parent.Tok == token.DEFINE
	case *ast.ValueSpec:
		// var v, err = c.ParamsInt(k)
		at -= 2
		if at < 0 || len(parent.Names) != 2 || len(parent.Values) != 1 || parent.Values[0] != call {
			return ctxParamsErr{}
		}
		declStmt, ok := stack[at].(*ast.DeclStmt)
		if !ok {
			return ctxParamsErr{}
		}
		value, errExpr, stmt, define = parent.Names[0], parent.Names[1], declStmt, true
	default:
		return ctxParamsErr{}
	}

	errID, ok := errExpr.(*ast.Ident)
	if !ok {
		return ctxParamsErr{}
	}
	if errID.Name == blankIdent {
		return ctxParamsErr{use: ctxErrIgnored, from: value.End(), to: errExpr.End()}
	}

	// the error is read: the value after it may be, unless the code leaves
	valueID, isIdent := value.(*ast.Ident)
	if isIdent && valueID.Name == blankIdent {
		return ctxParamsErr{use: ctxErrGuarded}
	}
	if at == 0 {
		return ctxParamsErr{}
	}
	if isIdent {
		ifs := ctxGuard(stmt, stack[at-1], errID.Name, valueID.Name)
		if ifs != nil && ctxGuardsError(ifs, errID.Name, valueID.Name) && ctxValueUnseen(ifs, stmt, valueID.Name, define, stack) {
			return ctxParamsErr{use: ctxErrGuarded}
		}
	}
	return ctxResetErr(value, errID.Name, stack[at-1])
}

// ctxValueUnseen reports whether nothing but the code after the guard can read the variable the
// ParamsInt statement stmt assigns: a variable of the initialization of the if statement, or one
// that the statement declares, is a new one; an assignment can be to a named result (a bare
// return reads it), or to a variable that a closure reads, and then the value after the error
// can be seen.
func ctxValueUnseen(ifs *ast.IfStmt, stmt ast.Stmt, name string, define bool, stack []ast.Node) bool {
	if ifs.Init == stmt {
		return define
	}
	if define {
		return !ctxMentionedBefore(name, stmt.Pos(), stack)
	}

	for _, node := range slices.Backward(stack) {
		var ft *ast.FuncType
		var body *ast.BlockStmt
		switch fn := node.(type) {
		case *ast.FuncDecl:
			ft, body = fn.Type, fn.Body
		case *ast.FuncLit:
			ft, body = fn.Type, fn.Body
		default:
			continue
		}
		if body == nil || (ft.Results != nil && ctxMentions(ft.Results, name)) {
			return false
		}
		return !ctxMentionedInClosure(body, name)
	}
	return false
}

// ctxMentionedInClosure reports whether a function literal inside the body uses the name.
func ctxMentionedInClosure(body *ast.BlockStmt, name string) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok && ctxMentions(lit, name) {
			found = true
		}
		return !found
	})
	return found
}

// ctxResetErr is the use of an error that is read, where the code may read the value as
// well: it can follow the assignment with a statement that makes the value 0 on an error
// when the assignment is in a list of statements and the value is a variable to assign
// to twice.
func ctxResetErr(value ast.Expr, errName string, parent ast.Node) ctxParamsErr {
	switch parent.(type) {
	case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause:
	default:
		return ctxParamsErr{}
	}
	if !ctxIsAssignable(value) {
		return ctxParamsErr{}
	}
	return ctxParamsErr{use: ctxErrReset, value: value, errName: errName}
}

// ctxIsAssignable reports whether e is a variable or a field of one, which can be
// assigned to once more without evaluating anything twice.
func ctxIsAssignable(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Ident:
		return true
	case *ast.SelectorExpr:
		return ctxIsAssignable(x.X)
	case *ast.ParenExpr:
		return ctxIsAssignable(x.X)
	}
	return false
}

// blankIdent is the blank identifier.
const blankIdent = "_"

// ctxGuard returns the if statement that checks the error of the statement stmt, whose
// parent is parent: the one that has it as its initialization, or else the first of the
// statements that follow it that uses the error or the value, when it is an if statement.
// The ones in between neither read the value nor look at the error, so they run the same
// whatever the value is.
func ctxGuard(stmt ast.Stmt, parent ast.Node, errName, valueName string) *ast.IfStmt {
	var list []ast.Stmt
	switch p := parent.(type) {
	case *ast.IfStmt:
		if p.Init == stmt {
			return p
		}
		return nil
	case *ast.BlockStmt:
		list = p.List
	case *ast.CaseClause:
		list = p.Body
	case *ast.CommClause:
		list = p.Body
	default:
		return nil
	}

	i := slices.Index(list, stmt)
	if i < 0 {
		return nil
	}
	for _, next := range list[i+1:] {
		if ifs, ok := next.(*ast.IfStmt); ok && ifs.Init == nil && ctxMentions(ifs.Cond, errName) {
			return ifs
		}
		if ctxMentions(next, errName) || ctxMentions(next, valueName) {
			return nil
		}
	}
	return nil
}

// ctxGuardsError reports whether the if statement leaves when err is not nil, without
// reading the value: its condition is err != nil and its body ends in a return or a
// panic and does not mention the value.
func ctxGuardsError(ifs *ast.IfStmt, errName, valueName string) bool {
	cond, ok := ast.Unparen(ifs.Cond).(*ast.BinaryExpr)
	if !ok || cond.Op != token.NEQ || !isIdentNamed(cond.X, errName) || !isIdentNamed(cond.Y, "nil") {
		return false
	}
	if len(ifs.Body.List) == 0 || !ctxLeaves(ifs.Body.List[len(ifs.Body.List)-1]) {
		return false
	}
	return !ctxMentions(ifs.Body, valueName)
}

// ctxLeaves reports whether the statement leaves the function: a return or a call of
// panic.
func ctxLeaves(stmt ast.Stmt) bool {
	switch s := stmt.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.ExprStmt:
		call, ok := s.X.(*ast.CallExpr)
		return ok && isIdentNamed(call.Fun, "panic")
	}
	return false
}

// ctxMentions reports whether a name is used anywhere in the node.
func ctxMentions(n ast.Node, name string) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if id, ok := x.(*ast.Ident); ok && id.Name == name {
			found = true
		}
		return !found
	})
	return found
}

// ctxMentionedBefore reports whether the function that holds pos uses the name before
// pos: a variable declared by the statement at pos is a new one, unless the name is
// there already.
func ctxMentionedBefore(name string, pos token.Pos, stack []ast.Node) bool {
	for _, node := range slices.Backward(stack) {
		var body *ast.BlockStmt
		switch fn := node.(type) {
		case *ast.FuncDecl:
			body = fn.Body
			if fn.Type != nil && ctxMentions(fn.Type, name) {
				return true
			}
		case *ast.FuncLit:
			body = fn.Body
			if ctxMentions(fn.Type, name) {
				return true
			}
		default:
			continue
		}
		return body == nil || ctxMentionsBefore(body, name, pos)
	}
	return true
}

// ctxMentionsBefore reports whether a name is used in the node before pos.
func ctxMentionsBefore(n ast.Node, name string, pos token.Pos) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if id, ok := x.(*ast.Ident); ok && id.Name == name && id.Pos() < pos {
			found = true
		}
		return !found
	})
	return found
}
