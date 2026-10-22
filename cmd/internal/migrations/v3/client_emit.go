package v3

import (
	"fmt"
	"go/ast"
	"go/token"
	"slices"
	"strconv"
	"strings"
)

// clientEmit writes the edits that turn one request into v3 code.
type clientEmit struct {
	f  *clientFile
	u  *clientUnit
	id int
}

// emit rewrites the request of u.
func (f *clientFile) emit(u *clientUnit) {
	e := &clientEmit{f: f, u: u, id: f.edits.begin()}
	if u.configEligible() {
		e.config()
	} else {
		e.request()
	}
	e.notes()
	e.markHandled()
}

// markHandled records the fiber members the rewrite replaced.
func (e *clientEmit) markHandled() {
	f, u := e.f, e.u
	if u.root.sel != nil {
		f.handled[u.root.sel] = true
	}
	mark := func(n ast.Node) {
		ast.Inspect(n, func(c ast.Node) bool {
			if sel, ok := c.(*ast.SelectorExpr); ok && f.isFiberName(sel.X) {
				f.handled[sel] = true
			}
			return true
		})
	}
	for _, it := range u.items {
		if it.kind == itemDrop || it.kind == itemParse {
			mark(it.stmt)
		}
	}
}

// notes reports the behavior differences of the rewritten request.
func (e *clientEmit) notes() {
	f, u := e.f, e.u
	var all []clientOp
	all = append(all, u.ops...)
	for _, it := range u.items {
		all = append(all, it.ops...)
	}
	for _, op := range all {
		if op.note != "" {
			f.notef(op.line, "%s", op.note)
		}
	}
	e.headerConflicts(all)
}

// headerConflicts reports the headers set by hand that the v3 client replaces
// when it encodes the body: it sets Content-Type, and Accept for JSON.
func (e *clientEmit) headerConflicts(all []clientOp) {
	bodyAt := slices.IndexFunc(all, func(op clientOp) bool { return clientBodyMedia(op.body) != "" })
	if bodyAt < 0 {
		return
	}
	media := clientBodyMedia(all[bodyAt].body)
	isMedia := func(op clientOp) bool {
		return op.headerVal != "" && strings.HasPrefix(strings.ToLower(strings.TrimSpace(op.headerVal)), media)
	}
	for i, op := range all {
		// v2 set the content type when the body was set, so a header set before
		// the body lost against it there too.
		replaced := (op.header == clientHdrContentType && i > bodyAt) ||
			(op.header == clientHdrAccept && all[bodyAt].body == clientBodyJSON)
		if replaced && !isMedia(op) {
			e.f.notef(op.line, "the v3 client sets %s: %s for a body it encodes as %s, which replaces this header",
				map[string]string{clientHdrContentType: "Content-Type", clientHdrAccept: "Accept"}[op.header], media, media[strings.Index(media, "/")+1:])
		}
	}
}

// replaceStmt replaces a statement, keeping its trailing and inner comments.
func (f *clientFile) replaceStmt(id int, s ast.Node, text string) {
	start, end := f.g.offset(s.Pos()), f.g.offset(s.End())
	inner := f.innerComments(start, end)
	if cmt, cend := f.trailingComment(end); cmt != "" {
		end = cend
		first, rest, found := strings.Cut(text, "\n")
		text = first + " " + cmt
		if found {
			text += "\n" + rest
		}
	}
	if len(inner) > 0 {
		text = strings.Join(inner, "\n") + "\n" + text
	}
	f.edits.replace(id, start, end, text)
}

// deleteStmt removes a statement together with its line, keeping the comments
// that were inside it.
func (f *clientFile) deleteStmt(id int, s ast.Node) {
	src := f.g.src
	start, end := f.g.offset(s.Pos()), f.g.offset(s.End())
	inner := f.innerComments(start, end)
	if _, cend := f.trailingComment(end); cend > end {
		end = cend
	}

	ls := start
	for ls > 0 && (src[ls-1] == ' ' || src[ls-1] == '\t') {
		ls--
	}
	le := end
	for le < len(src) && (src[le] == ' ' || src[le] == '\t' || src[le] == '\r') {
		le++
	}
	if (ls == 0 || src[ls-1] == '\n') && (le >= len(src) || src[le] == '\n') {
		if le < len(src) {
			le++
		}
		repl := ""
		if len(inner) > 0 {
			repl = strings.Join(inner, "\n") + "\n"
		}
		f.edits.replace(id, ls, le, repl)
		return
	}
	f.edits.replace(id, start, end, strings.Join(inner, "\n"))
}

// trailingComment returns the line comment that follows offset end on the same
// line and the offset it ends at.
func (f *clientFile) trailingComment(end int) (string, int) {
	src := f.g.src
	for _, cg := range f.g.file.Comments {
		for _, c := range cg.List {
			cs := f.g.offset(c.Pos())
			if cs < end || !strings.HasPrefix(c.Text, "//") {
				continue
			}
			if strings.TrimSpace(src[end:cs]) == "" && !strings.Contains(src[end:cs], "\n") {
				return c.Text, f.g.offset(c.End())
			}
		}
	}
	return "", end
}

// innerComments returns the comments inside [start, end).
func (f *clientFile) innerComments(start, end int) []string {
	var out []string
	for _, cg := range f.g.file.Comments {
		for _, c := range cg.List {
			if cs := f.g.offset(c.Pos()); cs > start && f.g.offset(c.End()) < end {
				out = append(out, c.Text)
			}
		}
	}
	return out
}

// request writes the request as a chain of calls on a client.Request, in
// place of the statements it replaces.
func (e *clientEmit) request() {
	f, u := e.f, e.u
	recv := u.varName
	if u.create != nil {
		f.replaceStmt(e.id, u.create, recv+" := "+e.requestChain())
	}
	for _, it := range u.items {
		switch it.kind {
		case itemSetter:
			if text := setterLines(recv, it.ops); text != "" {
				f.replaceStmt(e.id, it.stmt, text)
			} else {
				f.deleteStmt(e.id, it.stmt)
			}
		case itemDrop:
			f.deleteStmt(e.id, it.stmt)
		case itemParse:
			f.deleteStmt(e.id, it.stmt)
			if it.parse.check != nil {
				f.deleteStmt(e.id, it.parse.check)
			}
		case itemTerm:
			e.requestTerm(it, recv)
		case itemEscape:
		}
	}
}

// requestTerm writes the statement that sends the request.
func (e *clientEmit) requestTerm(it *clientItem, recv string) {
	pre := setterLines(recv, it.ops)
	call := recv + ".Send()"
	if e.u.varName == "" {
		call = e.requestChain() + ".Send()"
	}
	e.term(it, call, pre)
}

// requestChain renders the creation of the request: the client it belongs to,
// the client settings, the method and URL, and the settings chained to it.
func (e *clientEmit) requestChain() string {
	f, u := e.f, e.u
	pkg := f.use(clientPkgPath, "client")
	var parts []string
	switch {
	case u.kind == clientViaClient:
		parts = append(parts, f.text(u.client))
	case u.dedicated:
		parts = append(parts, pkg+".New()")
	default:
		parts = append(parts, pkg+".C()")
	}
	for _, op := range u.ops {
		if op.cli != "" {
			parts = append(parts, op.cli)
		}
	}
	parts = append(parts, "R()")
	if u.kind != clientAcquire && u.kind != clientExternal {
		parts = append(parts, fmt.Sprintf("SetMethod(%q)", strings.ToUpper(u.method)), "SetURL("+f.text(u.url)+")")
	}
	for _, op := range u.ops {
		if op.req != "" {
			parts = append(parts, op.req)
		}
	}
	return strings.Join(parts, ".")
}

// setterLines renders the ops of one statement: the calls on the request are
// chained, the others get a statement of their own.
func setterLines(recv string, ops []clientOp) string {
	var lines, chain []string
	flush := func() {
		if len(chain) > 0 {
			lines = append(lines, recv+"."+strings.Join(chain, "."))
			chain = nil
		}
	}
	for _, op := range ops {
		switch {
		case op.dropped:
		case op.cli != "":
			flush()
			lines = append(lines, recv+".Client()."+op.cli)
		case op.stmt != "":
			flush()
			lines = append(lines, fmt.Sprintf(op.stmt, recv))
		case op.req != "":
			chain = append(chain, op.req)
		}
	}
	flush()
	return strings.Join(lines, "\n")
}

// config writes the request as one call of the client package with a
// client.Config.
func (e *clientEmit) config() {
	f, u := e.f, e.u
	if u.create != nil {
		f.deleteStmt(e.id, u.create)
	}
	for _, it := range u.items {
		switch it.kind {
		case itemSetter, itemDrop, itemParse:
			f.deleteStmt(e.id, it.stmt)
		case itemTerm:
			e.term(it, e.configCall(), "")
		case itemEscape:
		}
	}
}

// configCall renders the request as client.Get(url, client.Config{...}).
func (e *clientEmit) configCall() string {
	f, u := e.f, e.u
	pkg := f.use(clientPkgPath, "client")
	url := f.text(u.url)
	var entries []clientCfg
	for _, op := range u.allOps() {
		if op.hasQuery {
			base, _ := clientStringLit(u.url)
			base, _, _ = strings.Cut(base, "?")
			if len(op.query) > 0 {
				base += "?" + op.rawQuery
			}
			url = strconv.Quote(base)
			continue
		}
		entries = append(entries, op.cfg...)
	}
	recv := pkg
	if u.kind == clientViaClient {
		recv = f.text(u.client)
	}
	return recv + "." + u.method + "(" + url + clientConfigLit(pkg, entries) + ")"
}

// clientInlineConfig is the longest client.Config literal that is written on
// one line.
const clientInlineConfig = 72

// clientConfigLit renders a client.Config literal, preceded by a comma, or
// nothing for no entries. The fields come in the order they were first set.
func clientConfigLit(pkg string, entries []clientCfg) string {
	if len(entries) == 0 {
		return ""
	}
	var order []string
	maps := map[string][]string{}
	scalars := map[string]string{}
	for _, en := range entries {
		_, inMap := maps[en.field]
		_, inScalar := scalars[en.field]
		if !inMap && !inScalar {
			order = append(order, en.field)
		}
		if en.field == fieldHeader || en.field == fieldCookie {
			maps[en.field] = append(maps[en.field], en.key+": "+en.val)
		} else {
			scalars[en.field] = en.val
		}
	}

	parts := make([]string, 0, len(order))
	for _, field := range order {
		if kv, ok := maps[field]; ok {
			parts = append(parts, field+": map[string]string{"+strings.Join(kv, ", ")+"}")
			continue
		}
		parts = append(parts, field+": "+scalars[field])
	}
	joined := strings.Join(parts, ", ")
	if len(parts) <= 2 && len(joined) <= clientInlineConfig && !strings.Contains(joined, "\n") {
		return ", " + pkg + ".Config{" + joined + "}"
	}
	return ", " + pkg + ".Config{\n" + strings.Join(parts, ",\n") + ",\n}"
}

// allOps returns the settings of the request in the order they were made.
func (u *clientUnit) allOps() []clientOp {
	ops := slices.Clone(u.ops)
	for _, it := range u.items {
		ops = append(ops, it.ops...)
	}
	return ops
}

// configEligible reports whether the request can be written as one call with a
// client.Config: it is created, configured and sent by consecutive statements
// of one block and every setting has a field in the Config.
func (u *clientUnit) configEligible() bool {
	switch u.kind {
	case clientFiberFunc, clientViaClient, clientFluent:
	default:
		return false
	}
	if u.dedicated {
		return false
	}

	terms := 0
	last := u.idx
	for _, it := range u.items {
		switch it.kind {
		case itemTerm:
			terms++
		case itemEscape:
			return false
		case itemSetter, itemDrop, itemParse:
		}
		if it.nested || it.list.owner != u.list.owner {
			return false
		}
		last = max(last, it.idx)
	}
	if terms != 1 || u.items[len(u.items)-1].kind != itemTerm {
		return false
	}
	if !u.statementsBetweenAreInert(last) || !u.opsFitConfig() {
		return false
	}
	return true
}

// statementsBetweenAreInert reports whether the statements between the
// creation of the request and its last use are all part of the request or
// declarations without a value.
func (u *clientUnit) statementsBetweenAreInert(last int) bool {
	for i := u.idx + 1; i < last; i++ {
		s := u.list.stmts[i]
		if slices.ContainsFunc(u.items, func(it *clientItem) bool { return it.stmt == s }) {
			continue
		}
		decl, ok := s.(*ast.DeclStmt)
		if !ok {
			return false
		}
		gd, ok := decl.Decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			return false
		}
		for _, spec := range gd.Specs {
			if vs, ok := spec.(*ast.ValueSpec); !ok || len(vs.Values) > 0 {
				return false
			}
		}
	}
	return true
}

// opsFitConfig reports whether every setting of the request has a field in a
// client.Config, without two settings competing for the same field.
func (u *clientUnit) opsFitConfig() bool {
	ops := u.allOps()
	scalars := map[string]bool{}
	headers := map[string]bool{}
	bodies, queries := 0, 0
	for _, op := range ops {
		if op.dropped {
			continue
		}
		if op.stmt != "" || op.cli != "" || op.setsMethod || op.setsURL {
			return false
		}
		if op.hasQuery {
			queries++
			if _, lit := clientStringLit(u.url); !lit {
				return false
			}
			continue
		}
		if len(op.cfg) == 0 {
			return false
		}
		if op.body != clientBodyNone {
			bodies++
		}
		for _, en := range op.cfg {
			switch en.field {
			case "Header":
				key := op.header
				if key == "" {
					key = en.key
				}
				if headers[key] {
					return false
				}
				headers[key] = true
			case fieldCookie:
			default:
				if scalars[en.field] {
					return false
				}
				scalars[en.field] = true
			}
		}
	}
	return bodies <= 1 && queries <= 1
}
