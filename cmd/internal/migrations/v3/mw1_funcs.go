package v3

import (
	"go/ast"
)

// mw1ParamCount returns the number of parameters a function type declares: a
// group such as (user, pass string) counts for each name.
func mw1ParamCount(ft *ast.FuncType) int {
	if ft.Params == nil {
		return 0
	}
	n := 0
	for _, f := range ft.Params.List {
		if len(f.Names) == 0 {
			n++
			continue
		}
		n += len(f.Names)
	}
	return n
}

// mw1FuncParamCounts returns the parameter counts of the functions and methods
// declared in the file with the given name. Nothing is returned when the name
// is not declared there.
func mw1FuncParamCounts(file *ast.File, name string) []int {
	var counts []int
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if ok && fd.Name.Name == name {
			counts = append(counts, mw1ParamCount(fd.Type))
		}
	}
	return counts
}

// mw1DeclaredWithParams reports whether e names a function or method that the
// project declares, every declaration of which takes n parameters. For a
// selector only the method name counts, since the receiver cannot be told
// without types. The index covers the files other than the one being migrated.
func mw1DeclaredWithParams(file *ast.File, idx *mw1Index, e ast.Expr, n int) bool {
	var name string
	switch x := e.(type) {
	case *ast.Ident:
		name = x.Name
	case *ast.SelectorExpr:
		name = x.Sel.Name
	default:
		return false
	}

	counts := append(mw1FuncParamCounts(file, name), idx.funcParams(name)...)
	return allEqual(counts, n)
}

// mw1AppendParam returns the offset and the text that add a parameter of the
// given type to the end of the parameter list of ft. The parameter is blank
// when the others are named.
func mw1AppendParam(g *goSource, ft *ast.FuncType, typ string) (offset int, text string) {
	last := ft.Params.List[len(ft.Params.List)-1]
	param := typ
	if len(last.Names) > 0 {
		param = "_ " + typ
	}
	offset = g.offset(last.End())
	if g.line(last.End()) != g.line(ft.Params.Closing) {
		return offset, ",\n" + param
	}
	return offset, ", " + param
}

// mw1ParamsOnOwnLines reports whether the parameters of ft are written one
// per line, the closing parenthesis included.
func mw1ParamsOnOwnLines(g *goSource, ft *ast.FuncType) bool {
	if ft.Params == nil || len(ft.Params.List) == 0 {
		return false
	}
	first := ft.Params.List[0]
	return g.line(ft.Params.Opening) != g.line(first.Pos())
}

// mw1PrependParam returns the offset and the text that add a parameter of the
// given type in front of the parameter list of ft. The parameter is blank when
// the others are named.
func mw1PrependParam(g *goSource, ft *ast.FuncType, typ string) (offset int, text string) {
	first := ft.Params.List[0]
	param := typ
	if len(first.Names) > 0 {
		param = "_ " + typ
	}
	offset = g.offset(first.Pos())
	if mw1ParamsOnOwnLines(g, ft) {
		return offset, param + ",\n"
	}
	return offset, param + ", "
}
