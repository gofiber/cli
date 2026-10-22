package v3

import (
	"go/ast"
	"go/token"
	"strings"
)

// Import paths of the packages the file helpers refer to.
const (
	clientIOPath    = "io"
	clientBytesPath = "bytes"
)

// clientFormRole says what a setting does for a form request.
type clientFormRole int

const (
	clientFormNone clientFormRole = iota
	// clientFormFields is Form: the fields of the arguments are the body.
	clientFormFields
	// clientFormMultipart is MultipartForm: the fields and the files added so
	// far are the body.
	clientFormMultipart
	// clientFormFiles adds files, which only MultipartForm sends in v2.
	clientFormFiles
	// clientFormBoundary is Boundary.
	clientFormBoundary
)

// formFieldsOp builds the op of Form and MultipartForm. v2 copied the fields of
// the arguments into the body when the method was called, so the statement that
// replaces it does the same at the same place.
func (f *clientFile) formFieldsOp(op clientOp, arg ast.Expr, multipart bool) (clientOp, clientUnsupported) {
	op.form = clientFormFields
	op.body = clientBodyForm
	if multipart {
		op.form = clientFormMultipart
		op.body = clientBodyMultipart
	}
	if isIdentNamed(arg, "nil") {
		if !multipart {
			return op, "Form(nil), which only sets the content type"
		}
		// MultipartForm(nil) writes the files alone.
		op.dropped = true
		return op, ""
	}
	op.stmt = f.formFieldsStmt(arg)
	return op, ""
}

// formFieldsStmt renders the statement that copies the fields of the
// fasthttp.Args arg to the request.
func (f *clientFile) formFieldsStmt(arg ast.Expr) func(recv string) string {
	args := f.text(arg)
	guard := !f.argsNonNil(arg)
	return func(recv string) string {
		key, val := "key", "value"
		if recv == key || recv == val {
			key, val = "k", "v"
		}
		if recv == key || recv == val {
			key, val = "name", "val"
		}
		text := args + ".VisitAll(func(" + key + ", " + val + " []byte) {\n" +
			recv + ".AddFormData(string(" + key + "), string(" + val + "))\n})"
		if guard {
			// v2 sent no fields for a nil *Args, VisitAll would panic.
			return "if " + args + " != nil {\n" + text + "\n}"
		}
		return text
	}
}

// argsNonNil reports whether e is evidently a non-nil *fasthttp.Args.
func (f *clientFile) argsNonNil(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return f.argsNonNil(x.X)
	case *ast.UnaryExpr:
		return x.Op == token.AND
	case *ast.CallExpr:
		if sel, ok := f.fiberSel(x.Fun); ok {
			return sel.Sel.Name == clientMemberAcquireArgs
		}
		return isIdentNamed(x.Fun, "new")
	case *ast.Ident:
		d, ok := f.resolve(x)
		return ok && d.typ == nil && d.init != nil && f.argsNonNil(d.init)
	}
	return false
}

// sendFileOp builds the op of SendFile and SendFiles: files that MultipartForm
// sends.
func (f *clientFile) sendFileOp(op clientOp, call *ast.CallExpr, many bool) (clientOp, clientUnsupported) {
	op.form = clientFormFiles
	args := call.Args
	pkg := f.use(clientPkgPath, "client")

	// file renders one file; field is the argument that names its form field.
	file := func(path, field ast.Expr) string {
		setters := []string{pkg + ".SetFilePath(" + f.text(path) + ")"}
		if field != nil {
			if s, lit := clientStringLit(field); !lit || s != "" {
				setters = append(setters, pkg+".SetFileFieldName("+f.text(field)+")")
			}
		}
		return pkg + ".AcquireFile(" + strings.Join(setters, ", ") + ")"
	}

	switch {
	case many && len(args) == 0:
		op.dropped = true
	case many:
		// The arguments are pairs of a path and a field name, the last path of
		// an odd list has no field name.
		var files []string
		for i := 0; i < len(args); i += 2 {
			var field ast.Expr
			if i+1 < len(args) {
				field = args[i+1]
			}
			files = append(files, file(args[i], field))
		}
		op.req = "AddFiles(" + strings.Join(files, ", ") + ")"
	case len(args) == 1:
		op.req = "AddFile(" + f.text(args[0]) + ")"
	case len(args) == 2:
		op.req = "AddFiles(" + file(args[0], args[1]) + ")"
	default:
		return op, "SendFile() with more than one field name"
	}
	return op, ""
}

// fileDataOp builds the op of FileData. it is the statement that holds the
// call, nil when the call is part of the chain that creates the request.
func (f *clientFile) fileDataOp(op clientOp, call *ast.CallExpr, it *clientItem) (clientOp, clientUnsupported) {
	op.form = clientFormFiles
	if len(call.Args) == 0 {
		op.dropped = true
		return op, ""
	}
	files := make([]string, 0, len(call.Args))
	for _, arg := range call.Args {
		if lit, ok := f.formFileLiteral(arg); ok {
			text, reason := f.formFileValue(lit)
			if reason != "" {
				return op, reason
			}
			files = append(files, text)
			continue
		}
		id, isIdent := arg.(*ast.Ident)
		if !isIdent || it == nil {
			return op, "FileData() with a file that is not a fiber.FormFile literal or a variable of the same block"
		}
		ff, reason := f.formFileVar(id, it)
		if reason != "" {
			return op, reason
		}
		op.files = append(op.files, ff)
		files = append(files, id.Name)
	}
	op.req = "AddFiles(" + strings.Join(files, ", ") + ")"
	return op, ""
}

// formFileLiteral recognizes &fiber.FormFile{...}.
func (f *clientFile) formFileLiteral(e ast.Expr) (*ast.CompositeLit, bool) {
	if p, ok := e.(*ast.ParenExpr); ok {
		return f.formFileLiteral(p.X)
	}
	un, ok := e.(*ast.UnaryExpr)
	if !ok || un.Op != token.AND {
		return nil, false
	}
	lit, ok := un.X.(*ast.CompositeLit)
	if !ok || lit.Type == nil || !isFiberType(lit.Type, f.fiber, clientMemberFormFile) {
		return nil, false
	}
	return lit, true
}

// formFileValue renders a fiber.FormFile literal as the client.File it stands
// for.
func (f *clientFile) formFileValue(lit *ast.CompositeLit) (string, clientUnsupported) {
	pkg := f.use(clientPkgPath, "client")
	var setters []string
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			return "", "a fiber.FormFile literal without field names"
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			return "", "a fiber.FormFile literal without field names"
		}
		switch key.Name {
		case clientMemberFieldname:
			setters = append(setters, pkg+".SetFileFieldName("+f.text(kv.Value)+")")
		case clientMemberName:
			setters = append(setters, pkg+".SetFileName("+f.text(kv.Value)+")")
		case "Content":
			setters = append(setters, pkg+".SetFileReader("+f.contentReader(kv.Value)+")")
		default:
			return "", clientUnsupported("the " + key.Name + " field of a fiber.FormFile")
		}
	}
	return pkg + ".AcquireFile(" + strings.Join(setters, ", ") + ")", ""
}

// contentReader renders the reader that serves the content of a file.
func (f *clientFile) contentReader(content ast.Expr) string {
	return f.contentReaderText(f.text(content))
}

func (f *clientFile) contentReaderText(content string) string {
	return f.use(clientIOPath, "io") + ".NopCloser(" + f.use(clientBytesPath, "bytes") + ".NewReader(" + content + "))"
}

// clientFormFile is a variable that holds a fiber.FormFile and is handed to
// one request: it becomes a client.File.
type clientFormFile struct {
	// create is the declaration of the variable and value what it is set to.
	create *ast.AssignStmt
	// valueText is the client.File the declaration creates.
	valueText string
	// fields are the assignments to the fields of the file.
	fields []clientFileField
	// releases are the statements that return the file to its pool.
	releases []ast.Stmt
}

// clientFileField is the assignment of one field of a fiber.FormFile.
type clientFileField struct {
	stmt  *ast.AssignStmt
	value ast.Expr
	recv  ast.Expr
	field string
}

// formFileVar analyzes the uses of the FormFile variable id, which the
// statement it hands to a request. The variable has to be declared in the same
// block, and only be set field by field, handed to this request and released.
func (f *clientFile) formFileVar(id *ast.Ident, it *clientItem) (*clientFormFile, clientUnsupported) {
	const unknown = clientUnsupported("FileData() with a FormFile that is used in a way the migration does not handle")
	l := it.list
	at := -1
	for i := it.idx - 1; i >= 0; i-- {
		if clientStmtDeclares(l.stmts[i], id.Name) {
			at = i
			break
		}
	}
	if at < 0 {
		return nil, "FileData() with a FormFile that is not declared in the same block"
	}
	create, ok := l.stmts[at].(*ast.AssignStmt)
	if !ok || create.Tok != token.DEFINE || len(create.Lhs) != 1 || len(create.Rhs) != 1 || !isIdentNamed(create.Lhs[0], id.Name) {
		return nil, unknown
	}
	ff := &clientFormFile{create: create}
	switch rhs := create.Rhs[0].(type) {
	case *ast.CallExpr:
		sel, isFiber := f.fiberSel(rhs.Fun)
		if !isFiber || sel.Sel.Name != nameAcquireFormFile || len(rhs.Args) != 0 {
			return nil, unknown
		}
		ff.valueText = f.use(clientPkgPath, "client") + ".AcquireFile()"
	default:
		lit, isLit := f.formFileLiteral(rhs)
		if !isLit {
			return nil, unknown
		}
		text, reason := f.formFileValue(lit)
		if reason != "" {
			return nil, reason
		}
		ff.valueText = text
	}

	refs, _ := clientRefsIn(id.Name, l.stmts[at+1:])
	groups, ok := clientGroupRefs(l, refs)
	if !ok {
		return nil, unknown
	}
	handed := 0
	for _, g := range groups {
		if g.nested {
			return nil, unknown
		}
		switch {
		case f.isFormFileRelease(g, id.Name):
			ff.releases = append(ff.releases, g.stmt)
		case clientIsFileHandover(g):
			handed += len(g.refs)
		default:
			field, ok := clientFileFieldOf(g, id.Name)
			if !ok {
				return nil, unknown
			}
			ff.fields = append(ff.fields, field)
		}
	}
	if handed != 1 {
		return nil, "FileData() with a FormFile that more than one call sends (a v3 file can be read once)"
	}
	return ff, ""
}

// isFormFileRelease recognizes fiber.ReleaseFormFile(ff) and its deferred form.
func (f *clientFile) isFormFileRelease(g clientRefGroup, name string) bool {
	var call *ast.CallExpr
	switch s := g.stmt.(type) {
	case *ast.ExprStmt:
		if c, ok := s.X.(*ast.CallExpr); ok {
			call = c
		}
	case *ast.DeferStmt:
		call = s.Call
	}
	if call == nil {
		return false
	}
	sel, ok := f.fiberSel(call.Fun)
	return ok && sel.Sel.Name == nameReleaseFormFile && len(call.Args) == 1 && isIdentNamed(call.Args[0], name)
}

// isFormFileHandover recognizes a statement in which every reference is an
// argument of a call of FileData.
func clientIsFileHandover(g clientRefGroup) bool {
	refs := map[*ast.Ident]bool{}
	for _, r := range g.refs {
		refs[r.id] = true
	}
	found := 0
	ast.Inspect(g.stmt, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "FileData" {
			return true
		}
		for _, a := range call.Args {
			if id, isIdent := a.(*ast.Ident); isIdent && refs[id] {
				found++
			}
		}
		return true
	})
	return found == len(g.refs)
}

// formFileField recognizes ff.Fieldname = x, ff.Name = x and ff.Content = x.
func clientFileFieldOf(g clientRefGroup, name string) (clientFileField, bool) {
	s, ok := g.stmt.(*ast.AssignStmt)
	if !ok || s.Tok != token.ASSIGN || len(s.Lhs) != 1 || len(s.Rhs) != 1 || len(g.refs) != 1 {
		return clientFileField{}, false
	}
	sel, ok := s.Lhs[0].(*ast.SelectorExpr)
	if !ok || !isIdentNamed(sel.X, name) {
		return clientFileField{}, false
	}
	switch sel.Sel.Name {
	case clientMemberFieldname, clientMemberName, clientMemberContent:
		return clientFileField{stmt: s, value: s.Rhs[0], field: sel.Sel.Name, recv: sel.X}, true
	}
	return clientFileField{}, false
}

// formFile writes the code that turns a FormFile variable into a client.File.
func (e *clientEmit) formFile(ff *clientFormFile) {
	f := e.f
	rhs := ff.create.Rhs[0]
	f.edits.replace(e.id, f.g.offset(rhs.Pos()), f.g.offset(rhs.End()), ff.valueText)
	f.markFiberSelectors(ff.create)
	for _, fld := range ff.fields {
		var text string
		switch fld.field {
		case clientMemberFieldname:
			text = f.text(fld.recv) + ".SetFieldName(" + f.text(fld.value) + ")"
		case clientMemberName:
			text = f.text(fld.recv) + ".SetName(" + f.text(fld.value) + ")"
		default:
			text = f.text(fld.recv) + ".SetReader(" + f.contentReader(fld.value) + ")"
		}
		f.replaceStmt(e.id, fld.stmt, text)
	}
	for _, rel := range ff.releases {
		f.deleteStmt(e.id, rel)
		f.markFiberSelectors(rel)
	}
}

// clientValidateForms checks the settings of a form request against what v2 did with
// them and the single body type a v3 request has.
func clientValidateForms(u *clientUnit) clientUnsupported {
	type setting struct {
		it *clientItem
		op clientOp
	}
	var all []setting
	for _, op := range u.ops {
		all = append(all, setting{op: op})
	}
	for _, it := range u.items {
		for _, op := range it.ops {
			all = append(all, setting{it: it, op: op})
		}
	}

	var files, forms, multiparts int
	var boundary bool
	kinds := map[clientBodyKind]bool{}
	for _, s := range all {
		if s.op.form != clientFormNone && s.it != nil && s.it.nested {
			return "a form, a file or a boundary is set inside a nested block"
		}
		if s.op.body != clientBodyNone {
			kinds[s.op.body] = true
		}
		switch s.op.form {
		case clientFormFields:
			forms++
		case clientFormFiles:
			if multiparts > 0 {
				return "files are added after MultipartForm(), which v2 does not send"
			}
			files++
		case clientFormMultipart:
			if files == 0 {
				return "MultipartForm() without a file: the v3 client sends a form without files url-encoded, never as multipart"
			}
			multiparts++
		case clientFormBoundary:
			if multiparts > 0 {
				return "Boundary() after MultipartForm(), which has no effect in v2"
			}
			boundary = true
		case clientFormNone:
		}
	}

	switch {
	case files > 0 && multiparts == 0:
		return "FileData()/SendFile() without a MultipartForm() call after them: v2 only sends the files from MultipartForm()"
	case boundary && multiparts == 0:
		return "Boundary() without a MultipartForm() call after it, which has no effect in v2"
	case forms > 1 || multiparts > 1:
		return "Form() or MultipartForm() is called more than once"
	case forms+multiparts > 0 && len(kinds) > 1:
		return "a form together with another kind of body"
	}
	return ""
}
