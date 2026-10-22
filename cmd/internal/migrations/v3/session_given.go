package v3

import (
	"go/ast"
	"go/token"
)

// A v2 config keeps the CookieName and the KeyLookup it is given as they are, until
// it makes a store, and the store keeps its config: what the program reads back
// from them is what it wrote. The Extractor of v3 holds the lookup, and the reads
// that mw3LookupReader writes rebuild the strings from it, which gives the same
// strings except where the program gave a CookieName (the extractor of a config
// that has one says "cookie:name" for KeyLookup, which v2 never filled in) or gave
// none (the extractor of a store that reads a cookie says its name for
// CookieName, which v2 never filled in). Where the program shows how the config
// was made, the strings are known, and a read is the string itself.

// mw3Given is the CookieName and the KeyLookup that a config was given, as the
// program wrote them.
type mw3Given struct {
	cookie string
	lookup string
}

// mw3ConfigDefaultGiven is what session.ConfigDefault was given.
var mw3ConfigDefaultGiven = mw3Given{lookup: mw3SourceCookie + ":" + mw3DefaultCookie}

// mw3GivenReader reads what a config was given from the place that makes it.
type mw3GivenReader struct {
	f     *mw3SessionFile
	facts *mw3SessionFacts
	env   *mw3Env
	depth int
}

func (f *mw3SessionFile) givenReader() *mw3GivenReader {
	return &mw3GivenReader{f: f, facts: f.facts(), env: f.env}
}

// with returns the reader for an expression that another file of the package
// declares.
func (r *mw3GivenReader) with(env *mw3Env) *mw3GivenReader {
	other := *r
	other.env = env
	return &other
}

// isSessionMember reports whether e is session.member in the file the reader is
// in.
func (r *mw3GivenReader) isSessionMember(e ast.Expr, member string, stack []ast.Node) bool {
	sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != member {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && r.env.isSession(id.Name) && !mw3Shadowed(id.Name, id.Pos(), stack)
}

// ofStore returns what the config of the store was given, when the expression is
// a variable that is made by session.New, or such a call.
func (r *mw3GivenReader) ofStore(e ast.Expr, stack []ast.Node) (mw3Given, bool) {
	if r.depth > mw3MaxKindDepth {
		return mw3Given{}, false
	}
	r.depth++
	defer func() { r.depth-- }()

	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		if origin, ok := r.originOf(x, stack, false); ok {
			return r.with(origin.env).ofStore(origin.init, origin.stack)
		}
	case *ast.CallExpr:
		if !r.isSessionMember(x.Fun, mw3SessionNew, stack) && !r.isSessionMember(x.Fun, mw3SessionNewStore, stack) {
			return mw3Given{}, false
		}
		switch {
		case x.Ellipsis.IsValid():
		case len(x.Args) == 0:
			return mw3ConfigDefaultGiven, true
		case len(x.Args) == 1:
			return r.ofConfig(x.Args[0], stack)
		}
	}
	return mw3Given{}, false
}

// ofConfig returns what the config was given, when the expression is
// session.ConfigDefault, a literal, a variable that is made by one, or a call of a
// function of the package that returns one.
func (r *mw3GivenReader) ofConfig(e ast.Expr, stack []ast.Node) (mw3Given, bool) {
	if r.depth > mw3MaxKindDepth {
		return mw3Given{}, false
	}
	r.depth++
	defer func() { r.depth-- }()

	switch x := ast.Unparen(e).(type) {
	case *ast.SelectorExpr:
		if r.isSessionMember(x, mw3ConfigDefault, stack) {
			return mw3ConfigDefaultGiven, true
		}
	case *ast.CompositeLit:
		if x.Type != nil && r.isSessionMember(x.Type, mw3Config, stack) {
			return r.literal(x, stack)
		}
	case *ast.Ident:
		if origin, ok := r.originOf(x, stack, true); ok {
			return r.with(origin.env).ofConfig(origin.init, origin.stack)
		}
	case *ast.CallExpr:
		return r.ofFunc(x, stack)
	}
	return mw3Given{}, false
}

// literal reads the lookup of a session.Config literal. v2 gave the fields it did
// not find the empty string.
func (r *mw3GivenReader) literal(lit *ast.CompositeLit, stack []ast.Node) (mw3Given, bool) {
	var given mw3Given
	for _, elt := range lit.Elts {
		if _, keyed := elt.(*ast.KeyValueExpr); !keyed {
			return mw3Given{}, false
		}
	}
	if mw3Field(lit, mw3ExtractorField) != nil {
		return mw3Given{}, false // the extractor is the lookup
	}

	strs := newMw3Strings(r.f.g, r.f.pkgOf())
	for name, dst := range map[string]*string{mw3KeyLookup: &given.lookup, mw3CookieName: &given.cookie} {
		kv := mw3Field(lit, name)
		if kv == nil {
			continue
		}
		value, ok := strs.value(kv.Value, stack)
		if !ok {
			return mw3Given{}, false
		}
		*dst = value
	}
	return given, true
}

// ofFunc reads what a call of a function of the package that returns a config
// gives: what every return of the function gives, which all have to agree.
func (r *mw3GivenReader) ofFunc(call *ast.CallExpr, stack []ast.Node) (mw3Given, bool) {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return mw3Given{}, false
	}
	if _, local := resolveIdent(id.Name, id.Pos(), stack); local || mw3Shadowed(id.Name, id.Pos(), stack) {
		return mw3Given{}, false
	}
	fn, ok := r.facts.funcDecls[id.Name]
	if !ok || fn.decl.Body == nil || fn.decl.Type.Results == nil || len(fn.decl.Type.Results.List) != 1 ||
		len(fn.decl.Type.Results.List[0].Names) > 1 {
		return mw3Given{}, false
	}

	inner := r.with(fn.env)
	var (
		result mw3Given
		seen   bool
		failed bool
	)
	mw3Walk(fn.decl, func(n ast.Node, ancestors []ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false // its returns are its own
		case *ast.ReturnStmt:
			if len(n.Results) != 1 {
				failed = true
				return false
			}
			given, known := inner.ofConfig(n.Results[0], ancestors)
			if !known || (seen && given != result) {
				failed = true
				return false
			}
			result, seen = given, true
		}
		return !failed
	})
	return result, seen && !failed
}

// originOf returns the expression that a variable is initialized with, and where
// that is, when nothing changes the variable afterwards: neither an assignment to
// it, nor one to its lookup (or, for the variable that holds a store, to its
// config), nor its address being taken. config is whether the variable holds a
// config rather than a store.
func (r *mw3GivenReader) originOf(id *ast.Ident, stack []ast.Node, config bool) (mw3Origin, bool) {
	if d, local := resolveIdent(id.Name, id.Pos(), stack); local {
		fn := mw3OutermostFunc(stack)
		if d.init == nil || fn == nil || mw3Changes(fn, id.Name, config) {
			return mw3Origin{}, false
		}
		return mw3Origin{init: d.init, stack: stack, env: r.env}, true
	}
	if mw3Shadowed(id.Name, id.Pos(), stack) {
		return mw3Origin{}, false
	}

	v, found := r.facts.vars[id.Name]
	if !found || v.init == nil || r.facts.changes(id.Name, config) {
		return mw3Origin{}, false
	}
	return mw3Origin{init: v.init, env: v.env}, true
}

// mw3Origin is the expression a variable is initialized with, the ancestors to
// read it with, and what the file that has it says about its names.
type mw3Origin struct {
	init  ast.Expr
	env   *mw3Env
	stack []ast.Node
}

// changes reports whether a file of the package changes the variable of the
// package level, or its lookup.
func (f *mw3SessionFacts) changes(name string, config bool) bool {
	key := name
	if config {
		key += ".config"
	}
	if changed, done := f.mutated[key]; done {
		return changed
	}
	changed := false
	f.owner.goFiles(func(file *ast.File, _ string) {
		changed = changed || mw3Changes(file, name, config)
	})
	f.mutated[key] = changed
	return changed
}

// mw3Changes reports whether the code under root changes the variable called
// name: it assigns to it, to the lookup of the config it holds or, for the
// variable that holds a store, to the config of the store, or it takes the address
// of any of them. The variable is found by its name, wherever it is declared.
func mw3Changes(root ast.Node, name string, config bool) bool {
	touches := func(e ast.Expr) bool {
		// The path of selectors from the variable: nothing at all is the variable,
		// then the first field.
		path := []string{}
		for {
			switch x := ast.Unparen(e).(type) {
			case *ast.Ident:
				if x.Name != name {
					return false
				}
				if len(path) == 0 {
					return true
				}
				first := path[len(path)-1]
				return first == mw3KeyLookup || first == mw3CookieName || (!config && first == mw3Config)
			case *ast.SelectorExpr:
				path = append(path, x.Sel.Name)
				e = x.X
			case *ast.IndexExpr:
				e = x.X
			case *ast.StarExpr:
				e = x.X
			default:
				return false
			}
		}
	}

	changed := false
	ast.Inspect(root, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			if x.Tok != token.DEFINE {
				for _, lhs := range x.Lhs {
					changed = changed || touches(lhs)
				}
			}
		case *ast.IncDecStmt:
			changed = changed || touches(x.X)
		case *ast.UnaryExpr:
			changed = changed || (x.Op == token.AND && touches(x.X))
		case *ast.RangeStmt:
			if x.Tok == token.ASSIGN {
				changed = changed || (x.Key != nil && touches(x.Key)) || (x.Value != nil && touches(x.Value))
			}
		}
		return !changed
	})
	return changed
}

// mw3OutermostFunc returns the function declaration, or else the outermost
// function literal, that the node whose ancestors are stack is in.
func mw3OutermostFunc(stack []ast.Node) ast.Node {
	for _, node := range stack {
		switch node.(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			return node
		}
	}
	return nil
}

// mw3Walk calls fn for every node below root with the chain of its ancestors, root
// first, as goSource.walk does for a file.
func mw3Walk(root ast.Node, fn func(n ast.Node, ancestors []ast.Node) bool) {
	var stack []ast.Node
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if !fn(n, stack) {
			return false
		}
		stack = append(stack, n)
		return true
	})
}
