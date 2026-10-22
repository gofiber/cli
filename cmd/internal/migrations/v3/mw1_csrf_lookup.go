package v3

import (
	"go/ast"
	"go/token"
	"slices"
	"strconv"
	"strings"
)

// csrfSources maps the sources of a v2 KeyLookup ("header", "form", "query",
// "param") to the extractors of v3.
var csrfSources = func() map[string]string {
	sources := map[string]string{}
	for _, fn := range []string{"FromHeader", "FromForm", "FromQuery", "FromParam"} {
		sources[strings.ToLower(strings.TrimPrefix(fn, "From"))] = fn
	}
	return sources
}()

// csrfCustomKey names an extractor function that does not read a single
// request value.
const csrfCustomKey = "csrf"

// lookup replaces the KeyLookup, or the deprecated TokenLookup, of a csrf.Config
// literal with the Extractor of v3.
func (m *csrfMigration) lookup(lit *ast.CompositeLit, fields map[string]*ast.KeyValueExpr, index map[string]int) {
	keyLookup, tokenLookup := fields[csrfKeyLookup], fields[csrfTokenLookup]
	if keyLookup == nil && tokenLookup == nil {
		return
	}

	remove := func(name string) {
		start, end := mw1ElementSpan(m.g, lit, index[name])
		m.edits.replace(start, end, "")
	}

	// v2 ignores the lookup when an Extractor is given
	if fields[csrfExtractor] != nil {
		for _, name := range []string{csrfKeyLookup, csrfTokenLookup} {
			if fields[name] != nil {
				remove(name)
			}
		}
		return
	}

	// and prefers the deprecated field over the other
	if keyLookup != nil && tokenLookup != nil {
		m.notef(tokenLookup, "csrf.Config has both KeyLookup and TokenLookup and v2 used TokenLookup: set the Extractor instead (extractors.FromHeader, FromForm, FromQuery, FromParam); both fields were left as they are")
		return
	}

	kv := keyLookup
	name := csrfKeyLookup
	if kv == nil {
		kv, name = tokenLookup, csrfTokenLookup
	}

	text, ok := m.lookupExtractor(kv.Value)
	switch {
	case !ok:
		m.notef(kv, "csrf.Config has no %s in v3, and %s cannot be turned into an Extractor (left as it is): "+
			"use extractors.FromHeader, FromForm, FromQuery or FromParam; v3 panics when the extractor reads the CSRF cookie itself",
			name, m.g.text(kv.Value))
	case text == "":
		// "" is the default lookup, which is also the default extractor
		remove(name)
	default:
		m.edits.replaceNode(m.g, kv.Key, csrfExtractor)
		m.edits.replaceNode(m.g, kv.Value, text)
		m.usesExtractors = true
	}
}

// lookupExtractor turns the value of a KeyLookup into the extractor of v3. The
// text is empty for the empty lookup, which stands for the default one.
func (m *csrfMigration) lookupExtractor(e ast.Expr) (text string, ok bool) {
	if s, isLiteral := mw1StringLiteral(e); isLiteral {
		if s == "" {
			return "", true
		}
		source, key, valid := splitLookup(s)
		if !valid {
			return "", false
		}
		fn, known := csrfSources[source]
		if !known {
			return "", false
		}
		return m.extractorsName + "." + fn + "(" + strconv.Quote(key) + ")", true
	}

	// "header:" + name
	bin, isBinary := e.(*ast.BinaryExpr)
	if !isBinary || bin.Op != token.ADD {
		return "", false
	}
	prefix, isLiteral := mw1StringLiteral(bin.X)
	if !isLiteral {
		return "", false
	}
	source, rest, found := strings.Cut(prefix, ":")
	fn, known := csrfSources[source]
	if !found || !known || strings.Contains(rest, ":") {
		return "", false
	}

	key := m.g.text(bin.Y)
	if rest != "" {
		key = strconv.Quote(rest) + " + " + key
	}
	return m.extractorsName + "." + fn + "(" + key + ")", true
}

// splitLookup splits "<source>:<key>".
func splitLookup(s string) (source, key string, ok bool) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// extractor handles the value of the Extractor field: v2 takes a function, v3
// an extractors.Extractor.
func (m *csrfMigration) extractor(value ast.Expr, stack []ast.Node) {
	switch x := value.(type) {
	case *ast.ParenExpr:
		m.extractor(x.X, stack)
	case *ast.FuncLit:
		m.wrapCustom(x, x)
	case *ast.Ident:
		if x.Name != mw1NilIdent && !m.isV3Extractor(x, stack) {
			m.wrapCustom(x, nil)
		}
	case *ast.SelectorExpr:
		if !m.isV3Extractor(x, stack) {
			m.wrapCustom(x, nil)
		}
	}
	// a call is either csrf.CsrfFrom*(...), which selector renames, or an
	// extractor of v3 already
}

// isV3Extractor reports whether the expression is an extractors.Extractor: a
// member of the extractors package, or a variable declared as one.
func (m *csrfMigration) isV3Extractor(e ast.Expr, stack []ast.Node) bool {
	switch x := e.(type) {
	case *ast.SelectorExpr:
		pkg, _, ok := mw1Selector(x)
		return ok && pkg == m.extractorsName
	case *ast.Ident:
		d, ok := resolveIdent(x.Name, x.Pos(), stack)
		if !ok {
			return false
		}
		if d.typ != nil {
			return mw1IsMember(d.typ, []string{m.extractorsName}, "Extractor")
		}
		call, isCall := d.init.(*ast.CallExpr)
		if !isCall {
			return false
		}
		pkg, _, isSel := mw1Selector(call.Fun)
		return isSel && pkg == m.extractorsName
	}
	return false
}

// wrapCustom wraps a function in extractors.FromCustom.
func (m *csrfMigration) wrapCustom(e ast.Expr, lit *ast.FuncLit) {
	key := csrfCustomKey
	if lit != nil {
		m.customExtractors[lit] = true
		if k, ok := lookedUpKey(lit); ok {
			key = k
		}
	}
	m.edits.replace(m.g.offset(e.Pos()), m.g.offset(e.Pos()), m.extractorsName+".FromCustom("+strconv.Quote(key)+", ")
	m.edits.replace(m.g.offset(e.End()), m.g.offset(e.End()), ")")
	m.usesExtractors = true
}

// requestReaders are the methods of the context that read one named value of
// the request.
var requestReaders = strings.Fields("Get FormValue Query Params Cookies")

// lookedUpKey returns the name an extractor function reads when it reads a
// single one, which is what the metadata of an extractor is for.
func lookedUpKey(lit *ast.FuncLit) (string, bool) {
	var keys []string
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return true
		}
		sel, isSel := call.Fun.(*ast.SelectorExpr)
		if !isSel {
			return true
		}
		if slices.Contains(requestReaders, sel.Sel.Name) {
			if s, isLiteral := mw1StringLiteral(call.Args[0]); isLiteral && s != "" {
				keys = append(keys, s)
			}
		}
		return true
	})
	if len(keys) != 1 {
		return "", false
	}
	return keys[0], true
}
