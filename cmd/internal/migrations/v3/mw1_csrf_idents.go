package v3

import (
	"fmt"
	"go/ast"
	"go/token"
	"slices"
	"strings"
)

// csrfExtractorHelpers are the helpers of v2 that moved to the extractors
// package. CsrfFromCookie is not among them, see selector.
var csrfExtractorHelpers = map[string]string{
	"CsrfFromHeader": nameFromHeader,
	"CsrfFromForm":   nameFromForm,
	"CsrfFromQuery":  nameFromQuery,
	"CsrfFromParam":  nameFromParam,
}

// csrfMissingTokenErrors are the errors an extractor of v2 returned when the
// token was missing. In v3 the extractors share extractors.ErrNotFound, and the
// middleware hands csrf.ErrTokenNotFound to the error handler.
var csrfMissingTokenErrors = []string{"ErrMissingHeader", "ErrMissingForm", "ErrMissingQuery", "ErrMissingParam", "ErrMissingCookie"}

// The v3 errors that replace the referer errors of v2.
const (
	csrfRefererNotFound = "ErrRefererNotFound"
	csrfRefererInvalid  = "ErrRefererInvalid"
	csrfRefererNoMatch  = "ErrRefererNoMatch"
	csrfTokenNotFound   = "ErrTokenNotFound"
)

// selector migrates the members of the csrf package that were renamed or
// removed.
func (m *csrfMigration) selector(sel *ast.SelectorExpr, stack []ast.Node) {
	// csrf.ConfigDefault.<field>
	if inner, ok := sel.X.(*ast.SelectorExpr); ok && mw1IsMember(inner, m.names, "ConfigDefault") {
		m.configDefaultField(sel)
		return
	}

	pkg, name, ok := mw1Selector(sel)
	if !ok || !slices.Contains(m.names, pkg) {
		return
	}

	switch {
	case name == "CsrfFromCookie":
		m.notef(sel, "csrf.CsrfFromCookie is gone in v3, which panics when the extractor reads the CSRF cookie: read the token from a header or from a form field (extractors.FromHeader, extractors.FromForm)")
	case csrfExtractorHelpers[name] != "":
		m.edits.replaceNode(m.g, sel, m.extractorsName+"."+csrfExtractorHelpers[name])
		m.usesExtractors = true
		// the helper of v2 returned a function that was called with the context
		if len(stack) > 0 {
			if call, isCall := stack[len(stack)-1].(*ast.CallExpr); isCall && call.Fun == ast.Expr(sel) && len(stack) > 1 {
				if outer, outerCall := stack[len(stack)-2].(*ast.CallExpr); outerCall && outer.Fun == ast.Expr(call) {
					m.edits.replace(m.g.offset(call.End()), m.g.offset(call.End()), ".Extract")
				}
			}
		}
	case slices.Contains(csrfMissingTokenErrors, name):
		if m.insideCustomExtractor(stack) {
			m.edits.replaceNode(m.g, sel, m.extractorsName+".ErrNotFound")
			m.usesExtractors = true
		} else {
			m.edits.replaceNode(m.g, sel, pkg+"."+csrfTokenNotFound)
		}
	case name == "ErrNoReferer":
		m.edits.replaceNode(m.g, sel.Sel, csrfRefererNotFound)
	case name == "ErrBadReferer":
		m.badReferer(sel, pkg, stack)
	case name == nameCSRFHandler:
		m.handlerType(sel, stack)
	}
}

// insideCustomExtractor reports whether one of the ancestors is a function that
// became an extractor.
func (m *csrfMigration) insideCustomExtractor(stack []ast.Node) bool {
	for _, n := range stack {
		if lit, ok := n.(*ast.FuncLit); ok && m.customExtractors[lit] {
			return true
		}
	}
	return false
}

// configDefaultField migrates csrf.ConfigDefault.<field>.
func (m *csrfMigration) configDefaultField(sel *ast.SelectorExpr) {
	switch sel.Sel.Name {
	case csrfExpiration:
		m.edits.replaceNode(m.g, sel.Sel, csrfIdleTimeout)
	case csrfHandlerContextKey:
		m.notef(sel, "csrf.ConfigDefault.HandlerContextKey is gone in v3: the handler is read with csrf.HandlerFromContext(c), without a key")
	case csrfKeyLookup, csrfTokenLookup, csrfSessionKey:
		m.notef(sel, "csrf.ConfigDefault.%s is gone in v3", sel.Sel.Name)
	}
}

// badReferer migrates csrf.ErrBadReferer. v2 used it for a referer that does
// not parse and for one that does not match; v3 tells them apart
// (ErrRefererInvalid and ErrRefererNoMatch), so a test for the error becomes a
// test for either.
func (m *csrfMigration) badReferer(sel *ast.SelectorExpr, pkg string, stack []ast.Node) {
	invalid := pkg + "." + csrfRefererInvalid
	noMatch := pkg + "." + csrfRefererNoMatch
	fail := func() {
		m.notef(sel, "csrf.ErrBadReferer is split in v3 into csrf.ErrRefererInvalid (the referer does not parse) and csrf.ErrRefererNoMatch (it does not match the host or a trusted origin): "+
			"test for both here (left as it is)")
	}
	if len(stack) == 0 {
		fail()
		return
	}

	switch parent := stack[len(stack)-1].(type) {
	case *ast.CaseClause:
		// case csrf.ErrBadReferer: of a switch on the error
		if len(stack) > 2 {
			if sw, isSwitch := stack[len(stack)-3].(*ast.SwitchStmt); isSwitch && sw.Tag != nil {
				m.edits.replaceNode(m.g, sel, invalid+", "+noMatch)
				return
			}
		}
	case *ast.CallExpr:
		// a call of errors.Is on the error
		if len(parent.Args) == 2 && parent.Args[1] == ast.Expr(sel) && isPlainPath(parent.Args[0]) && strings.HasSuffix(m.g.text(parent.Fun), ".Is") {
			fn, arg := m.g.text(parent.Fun), m.g.text(parent.Args[0])
			m.edits.replaceNode(m.g, parent, fmt.Sprintf("(%[1]s(%[2]s, %[3]s) || %[1]s(%[2]s, %[4]s))", fn, arg, invalid, noMatch))
			return
		}
	case *ast.BinaryExpr:
		// err == csrf.ErrBadReferer
		other := parent.X
		if other == ast.Expr(sel) {
			other = parent.Y
		}
		if isPlainPath(other) && (parent.Op == token.EQL || parent.Op == token.NEQ) {
			op, join := "==", " || "
			if parent.Op == token.NEQ {
				op, join = "!=", " && "
			}
			text := m.g.text(other)
			m.edits.replaceNode(m.g, parent, "("+text+" "+op+" "+invalid+join+text+" "+op+" "+noMatch+")")
			return
		}
	}
	fail()
}

// handlerType migrates csrf.CSRFHandler, the type of the handler that is read
// back from the context.
func (m *csrfMigration) handlerType(sel *ast.SelectorExpr, stack []ast.Node) {
	// a type assertion on something that is not the context lookup is left to
	// the compiler: the handler is not stored under a key any more
	if len(stack) >= 2 {
		if _, isStar := stack[len(stack)-1].(*ast.StarExpr); isStar {
			if assert, isAssert := stack[len(stack)-2].(*ast.TypeAssertExpr); isAssert && !isLocalsLookup(assert.X) {
				m.notef(sel, "the csrf handler is not stored in Locals any more: read it with csrf.HandlerFromContext(c), whose type is *csrf.Handler (csrf.CSRFHandler was left as it is)")
				return
			}
		}
	}
	m.edits.replaceNode(m.g, sel.Sel, "Handler")
}

// isLocalsLookup reports whether e is a call of Locals with a key.
func isLocalsLookup(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	return isSel && sel.Sel.Name == "Locals"
}

// isHandlerType reports whether e is *csrf.CSRFHandler.
func (m *csrfMigration) isHandlerType(e ast.Expr) bool {
	star, ok := e.(*ast.StarExpr)
	return ok && mw1IsMember(star.X, m.names, "CSRFHandler")
}

// handlerCall renders the lookup that replaces `c.Locals(key).(*csrf.CSRFHandler)`.
func (m *csrfMigration) handlerCall(assert *ast.TypeAssertExpr) string {
	star, isStar := assert.Type.(*ast.StarExpr)
	locals, isCall := assert.X.(*ast.CallExpr)
	if !isStar || !isCall {
		return ""
	}
	sel, isSel := locals.Fun.(*ast.SelectorExpr)
	if !isSel {
		return ""
	}
	pkg, _, _ := mw1Selector(star.X)
	return pkg + ".HandlerFromContext(" + m.g.text(sel.X) + ")"
}

// handlerAssertion migrates `c.Locals(key).(*csrf.CSRFHandler)`: v3 does not
// store the handler under a key, csrf.HandlerFromContext returns it. It reports
// whether the expression was dealt with, key included.
func (m *csrfMigration) handlerAssertion(assert *ast.TypeAssertExpr, stack []ast.Node) bool {
	if assert.Type == nil || !m.isHandlerType(assert.Type) || !isLocalsLookup(assert.X) {
		return false
	}
	// var handler, ok = ...
	if len(stack) > 0 {
		if spec, isSpec := stack[len(stack)-1].(*ast.ValueSpec); isSpec && len(spec.Names) == 2 {
			m.reportHandlerLookup(spec)
			return true
		}
	}
	m.edits.replaceNode(m.g, assert, m.handlerCall(assert))
	return true
}

// handlerAssignment migrates the form of the lookup that tests the result:
//
//	handler, ok := c.Locals(key).(*csrf.CSRFHandler)
//
// is `handler := csrf.HandlerFromContext(c)` followed by `ok := handler != nil`,
// or the same in the condition of an if. It reports whether the statement was
// dealt with, which includes the cases that are reported and left as they are.
func (m *csrfMigration) handlerAssignment(as *ast.AssignStmt, stack []ast.Node) bool {
	if len(as.Lhs) != 2 || len(as.Rhs) != 1 {
		return false
	}
	assert, ok := as.Rhs[0].(*ast.TypeAssertExpr)
	if !ok || assert.Type == nil || !m.isHandlerType(assert.Type) || !isLocalsLookup(assert.X) {
		return false
	}

	handler, okVar := identName(as.Lhs[0]), identName(as.Lhs[1])
	if handler == "" || okVar == "" || (as.Tok != token.DEFINE && as.Tok != token.ASSIGN) || len(stack) == 0 {
		m.reportHandlerLookup(as)
		return true
	}
	call := m.handlerCall(assert)

	// the result of the test is not used
	if okVar == "_" && handler != "_" {
		assign := " := "
		if as.Tok == token.ASSIGN {
			assign = " = "
		}
		m.edits.replaceNode(m.g, as, handler+assign+call)
		return true
	}

	// if handler, ok := ...; ok {
	if ifStmt, isIf := stack[len(stack)-1].(*ast.IfStmt); isIf && ifStmt.Init == ast.Stmt(as) && as.Tok == token.DEFINE {
		subject := handler
		if handler == "_" {
			subject = call
		}
		edits, valid := m.condition(ifStmt.Cond, okVar, subject)
		if !valid {
			m.reportHandlerLookup(as)
			return true
		}
		if handler == "_" {
			// the lookup moves into the condition, the init statement goes
			m.edits.replace(m.g.offset(as.Pos()), m.g.offset(ifStmt.Cond.Pos()), "")
		} else {
			m.edits.replaceNode(m.g, as, handler+" := "+call)
		}
		m.edits.edits = append(m.edits.edits, edits...)
		return true
	}

	// a statement of a block
	switch stack[len(stack)-1].(type) {
	case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause:
		if mw1StartsLine(m.g.src, m.g.offset(as.Pos())) {
			indent := mw1Indent(m.g.src, m.g.offset(as.Pos()))
			assign := " := "
			if as.Tok == token.ASSIGN {
				assign = " = "
			}
			if handler == "_" {
				m.edits.replaceNode(m.g, as, okVar+assign+call+" != nil")
			} else {
				m.edits.replaceNode(m.g, as, handler+assign+call+"\n"+indent+okVar+assign+handler+" != nil")
			}
			return true
		}
	}
	m.reportHandlerLookup(as)
	return true
}

// condition returns the edits that rewrite the condition of an if that tested
// the ok of the lookup: the handler is nil when there is none. subject is the
// expression that holds the handler. valid is false when the variable is used
// in a way that cannot be expressed. Only the uses of the variable are edited,
// so that the rest of the condition is migrated like any other code.
func (m *csrfMigration) condition(cond ast.Expr, okVar, subject string) (edits []textEdit, valid bool) {
	var found editSet
	valid = true
	exact := mw1IdentNamed(cond, okVar) || isNot(cond, okVar)
	wrap := func(s string) string {
		if exact {
			return s
		}
		return "(" + s + ")"
	}
	ast.Inspect(cond, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.UnaryExpr:
			if isNot(x, okVar) {
				found.replaceNode(m.g, x, wrap(subject+" == nil"))
				return false
			}
		case *ast.SelectorExpr:
			// a field that is called like the variable is not a use of it
			ast.Inspect(x.X, func(inner ast.Node) bool {
				if id, ok := inner.(*ast.Ident); ok && id.Name == okVar {
					found.replaceNode(m.g, id, wrap(subject+" != nil"))
				}
				return true
			})
			return false
		case *ast.Ident:
			if x.Name == okVar {
				found.replaceNode(m.g, x, wrap(subject+" != nil"))
			}
		case *ast.FuncLit:
			valid = false
		}
		return true
	})
	return found.edits, valid && len(found.edits) > 0
}

func (m *csrfMigration) reportHandlerLookup(at ast.Node) {
	m.notef(at, "the csrf handler is not stored in Locals any more: read it with csrf.HandlerFromContext(c), which returns nil when there is none (csrf.CSRFHandler was left as it is)")
}

// identName returns the name of an identifier expression, empty otherwise.
func identName(e ast.Expr) string {
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// isNot reports whether e is `!name`.
func isNot(e ast.Expr, name string) bool {
	u, ok := e.(*ast.UnaryExpr)
	return ok && u.Op == token.NOT && mw1IdentNamed(u.X, name)
}

// The fields of the deprecated fiber.Cookie that v2 copied into the config, in
// the order of the config.
var csrfCookieFields = []struct{ from, to string }{
	{"Name", "CookieName"},
	{"Domain", "CookieDomain"},
	{"Path", "CookiePath"},
	{"Secure", "CookieSecure"},
	{"HTTPOnly", "CookieHTTPOnly"},
	{"SameSite", "CookieSameSite"},
}

// cookie replaces the deprecated Cookie: &fiber.Cookie{...} with the Cookie*
// fields that v2 copied from it.
func (m *csrfMigration) cookie(lit *ast.CompositeLit, kv *ast.KeyValueExpr, idx int, fields map[string]*ast.KeyValueExpr) {
	value := kv.Value
	if u, ok := value.(*ast.UnaryExpr); ok && u.Op == token.AND {
		value = u.X
	}
	cookie, ok := value.(*ast.CompositeLit)
	if !ok || cookie.Type == nil || !m.isFiberCookie(cookie.Type) {
		m.notef(kv, "csrf.Config has no Cookie in v3: set CookieName, CookieDomain, CookiePath, CookieSecure, CookieHTTPOnly and CookieSameSite (left as it is)")
		return
	}

	var out []string
	given := map[string]bool{}
	for _, elt := range cookie.Elts {
		pair, isPair := elt.(*ast.KeyValueExpr)
		name := ""
		if isPair {
			name = identName(pair.Key)
		}
		if name == "" {
			m.notef(kv, "csrf.Config has no Cookie in v3: set CookieName, CookieDomain, CookiePath, CookieSecure, CookieHTTPOnly and CookieSameSite (left as it is)")
			return
		}
		given[name] = true

		to := ""
		for _, f := range csrfCookieFields {
			if f.from == name {
				to = f.to
			}
		}
		// the other fields of the cookie were never read by the middleware
		if to == "" {
			continue
		}
		// and v2 skipped the empty strings, which are the defaults
		if s, isLiteral := mw1StringLiteral(pair.Value); isLiteral && s == "" {
			continue
		}
		if fields[to] != nil {
			m.notef(kv, "csrf.Config sets %s next to the deprecated Cookie, which v2 let win: keep the value you mean and remove the Cookie (left as it is)", to)
			return
		}
		out = append(out, to+": "+m.g.text(pair.Value))
	}
	// v2 always copied these two, a missing one is false
	for _, f := range []string{"Secure", "HTTPOnly"} {
		if to := "Cookie" + f; !given[f] && fields[to] != nil {
			m.notef(kv, "csrf.Config sets %s next to the deprecated Cookie, which v2 let win: keep the value you mean and remove the Cookie (left as it is)", to)
			return
		}
	}

	if len(out) == 0 {
		start, end := mw1ElementSpan(m.g, lit, idx)
		m.edits.replace(start, end, "")
		return
	}
	sep := ", "
	if mw1MultiLine(m.g, lit) {
		sep = ",\n"
	}
	m.edits.replaceNode(m.g, kv, strings.Join(out, sep))
}

// isFiberCookie reports whether e is fiber.Cookie.
func (m *csrfMigration) isFiberCookie(e ast.Expr) bool {
	pkg, name, ok := mw1Selector(e)
	return ok && name == clientFieldCookie && slices.Contains(fiberNamesOf(m.g.src), pkg)
}

// fiberNamesOf returns the identifiers the source binds to the core package.
func fiberNamesOf(src string) []string {
	return mw1Qualifiers(fiberNames(src))
}

// mw1IdentNamed is isIdentNamed for a value that may be nil.
func mw1IdentNamed(e ast.Expr, name string) bool {
	return e != nil && isIdentNamed(e, name)
}
