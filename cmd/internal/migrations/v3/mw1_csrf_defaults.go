package v3

import (
	"go/ast"
	"go/token"
)

// A token lasted an hour by default in v2 and lasts half an hour in v3. The
// migration sets the hour wherever a config does not name a lifetime itself.

// csrfV3Fields are the fields that only exist in v3: a config that sets one is
// written for v3 and keeps the defaults of v3.
var csrfV3Fields = []string{csrfIdleTimeout, "TrustedOrigins", "DisableValueRedaction"}

const (
	// csrfDefaultNote is the note for a copy of csrf.ConfigDefault that cannot be
	// completed with the lifetime of v2.
	csrfDefaultNote = "csrf.ConfigDefault is not what it was in v2 (a token lasts half an hour, not an hour): set IdleTimeout on the copy"
	// csrfZeroNote is the note for a config variable without a value that cannot
	// be completed with the lifetime of v2.
	csrfZeroNote = "a csrf.Config without values is not what it was in v2 (a token lasts half an hour, not an hour): set IdleTimeout"
)

// hour renders one hour in the time package of the file.
func (m *csrfMigration) hour() string {
	m.usesTime = true
	return m.timeName + ".Hour"
}

// defaultLifetime gives a csrf.Config literal the lifetime of v2 when it names
// none. A literal that is written for v3 already, because it sets a field of v3
// or an extractor of the extractors package, keeps the defaults of v3.
func (m *csrfMigration) defaultLifetime(lit *ast.CompositeLit, fields map[string]*ast.KeyValueExpr, stack []ast.Node) {
	for _, name := range append([]string{csrfExpiration, csrfCookieExpires}, csrfV3Fields...) {
		if fields[name] != nil {
			return
		}
	}
	if kv := fields[csrfExtractor]; kv != nil && m.isExtractorOfV3(kv.Value, stack) {
		return
	}
	start, end, text := mw1AddElements(m.g, lit, []string{csrfIdleTimeout + ": " + m.hour()})
	m.edits.replace(start, end, text)
}

// newCall gives csrf.New() without a config, and csrf.New(csrf.ConfigDefault),
// the config of v2.
func (m *csrfMigration) newCall(call *ast.CallExpr) {
	if !mw1IsMember(call.Fun, m.names, "New") {
		return
	}
	pkg, _, _ := mw1Selector(call.Fun)
	switch {
	case len(call.Args) == 0:
		offset := m.g.offset(call.Rparen)
		m.edits.replace(offset, offset, pkg+".Config{"+csrfIdleTimeout+": "+m.hour()+"}")
	case len(call.Args) == 1 && mw1IsMember(call.Args[0], m.names, "ConfigDefault"):
		m.edits.replaceNode(m.g, call.Args[0], pkg+".Config{"+csrfIdleTimeout+": "+m.hour()+"}")
	}
}

// defaultCopy gives a config that starts as a copy of csrf.ConfigDefault the
// lifetime of v2: the statement follows the declaration, unless the code sets
// the lifetime itself afterwards.
func (m *csrfMigration) defaultCopy(as *ast.AssignStmt, stack []ast.Node) {
	if len(as.Lhs) != 1 || len(as.Rhs) != 1 || !mw1IsMember(as.Rhs[0], m.names, "ConfigDefault") {
		return
	}
	id, ok := as.Lhs[0].(*ast.Ident)
	if !ok || as.Tok != token.DEFINE || len(stack) == 0 {
		return
	}
	list, ok := mw1StmtList(stack[len(stack)-1])
	if !ok || !mw1StartsLine(m.g.src, m.g.offset(as.Pos())) {
		m.notef(as, csrfDefaultNote)
		return
	}
	m.restoreLifetime(as, id.Name, list)
}

// valueSpec handles the declarations `var cfg = csrf.ConfigDefault`, which are
// reported, and `var cfg csrf.Config`, whose zero value is a different config in
// v3. stack holds the ancestors of the spec.
func (m *csrfMigration) valueSpec(spec *ast.ValueSpec, stack []ast.Node) {
	if len(spec.Values) == 1 && mw1IsMember(spec.Values[0], m.names, "ConfigDefault") {
		m.notef(spec, csrfDefaultNote)
		return
	}
	if len(spec.Values) != 0 || spec.Type == nil || !mw1IsMember(spec.Type, m.names, "Config") {
		return
	}

	const ancestors = 3 // the declaration, the statement that holds it, and the block
	if len(stack) >= ancestors {
		if decl, ok := stack[len(stack)-2].(*ast.DeclStmt); ok && mw1StartsLine(m.g.src, m.g.offset(decl.Pos())) {
			if list, isList := mw1StmtList(stack[len(stack)-3]); isList {
				for _, name := range spec.Names {
					m.restoreLifetime(decl, name.Name, list)
				}
				return
			}
		}
	}
	m.notef(spec, csrfZeroNote)
}

// restoreLifetime adds the statement that sets the lifetime of v2 after the
// statement that declares the config variable name, unless the code sets it
// later in the same list.
func (m *csrfMigration) restoreLifetime(decl ast.Stmt, name string, list []ast.Stmt) {
	set := mw1FieldsSetAfter(list, decl, name)
	for _, field := range append([]string{csrfExpiration, csrfCookieExpires}, csrfV3Fields...) {
		if set[field] {
			return
		}
	}
	mw1InsertStatements(m.g, &m.edits, decl, []string{name + "." + csrfIdleTimeout + " = " + m.hour()})
}

// isExtractorOfV3 reports whether the expression is an extractors.Extractor: a
// call of the extractors package or a variable or field of that type.
func (m *csrfMigration) isExtractorOfV3(e ast.Expr, stack []ast.Node) bool {
	if call, ok := e.(*ast.CallExpr); ok {
		pkg, _, isSel := mw1Selector(call.Fun)
		return isSel && pkg == m.extractorsName
	}
	return m.isV3Extractor(e, stack)
}
