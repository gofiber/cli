package v3

import (
	"encoding/base64"
	"fmt"
	"go/ast"
	"go/token"
	"net/url"
	"strconv"
	"strings"
)

// Import paths the emitted code refers to.
const (
	clientPkgPath    = "github.com/gofiber/fiber/v3/client"
	clientFasthttp   = "github.com/valyala/fasthttp"
	clientTLSPath    = "crypto/tls"
	clientBase64Path = "encoding/base64"
)

// Members of the v2 client that several places of the migration refer to.
const (
	clientMemberName        = "Name"
	clientMemberUserAgent   = "UserAgent"
	clientMemberJSONEncoder = "JSONEncoder"
	clientMemberJSONDecoder = "JSONDecoder"
	clientMemberAcquireArgs = "AcquireArgs"
	clientMemberFieldname   = "Fieldname"
	clientMemberContent     = "Content"
)

// Agent methods that take any number of cookies.
const (
	clientMethodCookies      = "Cookies"
	clientMethodCookiesBytes = "CookiesBytesKV"
)

// The fields of a client.Config that hold more than one value or that other
// code refers to.
const (
	clientFieldHeader = "Header"
	clientFieldCookie = "Cookie"
	clientFieldBody   = "Body"
)

// clientBodyKind says how an op sets the request body.
type clientBodyKind int

const (
	clientBodyNone clientBodyKind = iota
	clientBodyRaw
	clientBodyJSON
	clientBodyXML
	clientBodyForm
	clientBodyMultipart
)

// clientBodyMedia is the media type the v3 client gives a body it encodes.
func clientBodyMedia(k clientBodyKind) string {
	switch k {
	case clientBodyJSON:
		return "application/json"
	case clientBodyXML:
		return "application/xml"
	case clientBodyForm:
		return "application/x-www-form-urlencoded"
	case clientBodyMultipart:
		return "multipart/form-data"
	default:
		return ""
	}
}

// Header names, lower-cased, the body encoders of the v3 client set themselves.
const (
	clientHdrContentType = "content-type"
	clientHdrAccept      = "accept"
)

// clientCfg is one entry of a client.Config literal.
type clientCfg struct {
	field string // Header, Cookie, UserAgent, Referer, Body, Timeout, MaxRedirects
	key   string // key expression of the Header and Cookie maps
	val   string
}

// clientParam is one decoded query parameter.
type clientParam struct{ key, val string }

// clientOp is one setting of a v2 request that the migration understood: the
// v3 spelling of it on a request and, when there is one, in a client.Config.
type clientOp struct {
	// urlArg is the argument of an op that sets the URL.
	urlArg ast.Expr
	// stmt renders a complete statement for the request recv, for the ops that
	// cannot be chained.
	stmt func(recv string) string
	// method is the v2 method the op comes from.
	method string
	// req is the call to make on the request, without the receiver.
	req string
	// cli is the call to make on the client that sends the request.
	cli string
	// note is reported when the op was migrated but the behavior differs.
	note string
	// header is the lower-case name of the literal header the op sets, and
	// headerVal its value when that is a literal too.
	header    string
	headerVal string
	// rawQuery is the literal query string of a QueryString op.
	rawQuery string
	// files are the FormFile variables the op hands to the request.
	files []*clientFormFile
	// cfg is the client.Config form of the op, empty when it has none.
	cfg []clientCfg
	// query holds the pairs of a literal query string.
	query []clientParam
	// line is the line of the call in the source.
	line int
	body clientBodyKind
	// form says what the op does for a form request.
	form clientFormRole
	// hasQuery marks a QueryString op.
	hasQuery bool
	// dropped marks an op that has no v3 call at all.
	dropped bool
	// setsMethod marks an op that changes the HTTP method.
	setsMethod bool
	// setsURL marks an op that sets the URL, urlArg is its argument.
	setsURL bool
	// touchesURI marks an op that changes the URI the request was given, which
	// setting the request URI replaces.
	touchesURI bool
}

// clientHeaderKey returns the lower-case name of a literal header key,
// "" when the key is not a literal.
func clientHeaderKey(e ast.Expr) string {
	if x, ok := clientBytesConversion(e); ok {
		e = x
	}
	s, ok := clientStringLit(e)
	if !ok {
		return ""
	}
	return strings.ToLower(s)
}

// clientStringLit returns the value of a string literal.
func clientStringLit(e ast.Expr) (string, bool) {
	if p, ok := e.(*ast.ParenExpr); ok {
		return clientStringLit(p.X)
	}
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

// clientIsByteSliceType reports whether e is the type []byte.
func clientIsByteSliceType(e ast.Expr) bool {
	at, ok := e.(*ast.ArrayType)
	return ok && at.Len == nil && isIdentNamed(at.Elt, "byte")
}

// clientBytesConversion returns the operand of a []byte(x) conversion.
func clientBytesConversion(e ast.Expr) (ast.Expr, bool) {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || call.Ellipsis.IsValid() || !clientIsByteSliceType(call.Fun) {
		return nil, false
	}
	return call.Args[0], true
}

// strOf renders a []byte expression as a string expression: the literal
// itself for []byte("literal"), a string conversion otherwise.
func (f *clientFile) strOf(e ast.Expr) string {
	if x, ok := clientBytesConversion(e); ok {
		if _, isLit := clientStringLit(x); isLit {
			return f.text(x)
		}
	}
	return "string(" + f.text(e) + ")"
}

// bytesOf renders a string expression as a []byte expression.
func (f *clientFile) bytesOf(e ast.Expr) string {
	return "[]byte(" + f.text(e) + ")"
}

// clientUnsupported is the reason a setting could not be migrated.
type clientUnsupported string

// clientAgentArity lists the Agent methods with a fixed number of arguments.
var clientAgentArity = map[string]int{
	nameSet: 2, "SetBytesK": 2, "SetBytesV": 2, "SetBytesKV": 2,
	"Add": 2, "AddBytesK": 2, "AddBytesV": 2, "AddBytesKV": 2,
	"ConnectionClose": 0, clientMemberUserAgent: 1, "UserAgentBytes": 1,
	clientFieldCookie: 2, "CookieBytesK": 2, "CookieBytesKV": 2,
	"Referer": 1, "RefererBytes": 1, nameContentType: 1, "ContentTypeBytes": 1,
	"QueryString": 1, "QueryStringBytes": 1, "BasicAuth": 2, "BasicAuthBytes": 2,
	"BodyString": 1, clientFieldBody: 1, "BodyStream": 2, "XML": 1, "Boundary": 1,
	"Form": 1, "MultipartForm": 1,
	"Timeout": 1, "Reuse": 0, "InsecureSkipVerify": 0, nameTLSConfig: 1,
	"MaxRedirectsCount": 1, clientMemberJSONEncoder: 1, clientMemberJSONDecoder: 1,
}

// clientHostReason is the explanation for the Host setters, which v3 cannot express.
const clientHostReason = "Host(): the v3 client cannot send a Host header that differs from the URL " +
	"(use a custom dialer, client.New().SetDial(...), with the virtual host in the URL)"

// agentOp maps the call of the v2 Agent method m to the v3 spelling.
func (f *clientFile) agentOp(m string, call *ast.CallExpr, it *clientItem) (op clientOp, reason clientUnsupported) {
	args := call.Args
	op = clientOp{method: m, line: f.g.line(call.Pos())}
	if call.Ellipsis.IsValid() && m != clientMethodCookies && m != clientMethodCookiesBytes {
		return op, clientUnsupported("a spread argument of " + m + "()")
	}
	if want, fixed := clientAgentArity[m]; fixed && len(args) != want {
		return op, clientUnsupported("the arguments of " + m + "()")
	}
	a := func(i int) string { return f.text(args[i]) }
	s := func(i int) string { return f.strOf(args[i]) }

	switch m {
	case "Set":
		return clientHeaderOp(op, "SetHeader", args[0], a(0), a(1), true)
	case "SetBytesK":
		return clientHeaderOp(op, "SetHeader", args[0], s(0), a(1), true)
	case "SetBytesV":
		return clientHeaderOp(op, "SetHeader", args[0], a(0), s(1), true)
	case "SetBytesKV":
		return clientHeaderOp(op, "SetHeader", args[0], s(0), s(1), true)
	case "Add":
		return clientHeaderOp(op, "AddHeader", args[0], a(0), a(1), false)
	case "AddBytesK":
		return clientHeaderOp(op, "AddHeader", args[0], s(0), a(1), false)
	case "AddBytesV":
		return clientHeaderOp(op, "AddHeader", args[0], a(0), s(1), false)
	case "AddBytesKV":
		return clientHeaderOp(op, "AddHeader", args[0], s(0), s(1), false)
	case "ContentType":
		return clientHeaderOp(op, "SetHeader", nil, `"Content-Type"`, a(0), true)
	case "ContentTypeBytes":
		return clientHeaderOp(op, "SetHeader", nil, `"Content-Type"`, s(0), true)
	case "ConnectionClose":
		return clientHeaderOp(op, "SetHeader", nil, `"Connection"`, `"close"`, true)
	case clientMemberUserAgent:
		return clientScalarOp(op, clientMemberUserAgent, a(0))
	case "UserAgentBytes":
		return clientScalarOp(op, clientMemberUserAgent, s(0))
	case "Referer":
		return clientScalarOp(op, "Referer", a(0))
	case "RefererBytes":
		return clientScalarOp(op, "Referer", s(0))
	case "Timeout":
		return clientScalarOp(op, "Timeout", a(0))
	case "MaxRedirectsCount":
		return clientScalarOp(op, "MaxRedirects", a(0))
	case "Cookie":
		return clientCookieOp(op, a(0), a(1))
	case "CookieBytesK":
		return clientCookieOp(op, s(0), a(1))
	case "CookieBytesKV":
		return clientCookieOp(op, s(0), s(1))
	case clientMethodCookies, clientMethodCookiesBytes:
		return f.cookiesOp(op, call)
	case "BasicAuth":
		return f.basicAuthOp(op, args[0], args[1], false)
	case "BasicAuthBytes":
		return f.basicAuthOp(op, args[0], args[1], true)
	case "QueryString", "QueryStringBytes":
		return clientQueryOp(op, args[0])
	case "BodyString":
		op.req = "SetRawBody(" + f.bytesOf(args[0]) + ")"
		op.body = clientBodyRaw
		op.cfg = []clientCfg{{field: clientFieldBody, val: a(0)}}
	case "Body":
		op.req = "SetRawBody(" + a(0) + ")"
		op.body = clientBodyRaw
		op.cfg = []clientCfg{{field: clientFieldBody, val: f.rawBodyCfg(args[0])}}
	case "BodyStream":
		op.stmt = clientBodyStreamStmt(a(0), a(1))
		op.body = clientBodyRaw
	case "JSON":
		if len(args) != 1 {
			return op, "JSON() with a custom content type (the v3 client always sends application/json for a JSON body)"
		}
		op.req = "SetJSON(" + a(0) + ")"
		op.body = clientBodyJSON
		if f.jsonConfigSafe(args[0]) {
			op.cfg = []clientCfg{{field: clientFieldBody, val: a(0)}}
		}
	case "XML":
		op.req = "SetXML(" + a(0) + ")"
		op.body = clientBodyXML
	case "Boundary":
		op.req = "SetBoundary(" + a(0) + ")"
		op.form = clientFormBoundary
	case "Form":
		return f.formFieldsOp(op, args[0], false)
	case "MultipartForm":
		return f.formFieldsOp(op, args[0], true)
	case mw3SendFile:
		return f.sendFileOp(op, call, false)
	case "SendFiles":
		return f.sendFileOp(op, call, true)
	case "FileData":
		return f.fileDataOp(op, call, it)
	case "Reuse":
		op.dropped = true
	case "Debug":
		op.dropped = true
		op.note = "Debug() has no per-request equivalent in the v3 client and was removed, so the request/response dump is gone; " +
			"call Debug() on the client (client.C().Debug() or client.New().Debug()) to log every request through its logger"
	case "InsecureSkipVerify":
		tls := f.use(clientTLSPath, "tls")
		op.cli = "SetTLSConfig(&" + tls + ".Config{InsecureSkipVerify: true})"
	case "TLSConfig":
		op.cli = "SetTLSConfig(" + a(0) + ")"
	case clientMemberJSONEncoder:
		op.cli = "SetJSONMarshal(" + a(0) + ")"
	case clientMemberJSONDecoder:
		op.cli = "SetJSONUnmarshal(" + a(0) + ")"
	case "Host", "HostBytes":
		return op, clientHostReason
	case "SetResponse", "Dest", "RetryIf":
		return op, clientUnsupported(m + "() has no v3 equivalent (use the client.Response that Send returns, or client.NewWithHostClient for fasthttp tuning)")
	default:
		return op, clientUnsupported(m + "()")
	}
	return op, ""
}

// clientScalarOp builds an op that sets one scalar field of client.Config through the
// request method of the same name.
func clientScalarOp(op clientOp, field, val string) (clientOp, clientUnsupported) {
	switch field {
	case "MaxRedirects":
		op.req = "SetMaxRedirects(" + val + ")"
	default:
		op.req = "Set" + field + "(" + val + ")"
	}
	op.cfg = []clientCfg{{field: field, val: val}}
	return op, ""
}

// clientHeaderOp builds the op of a header setter. keyExpr is the argument that
// names the header, nil when the name is fixed; key and val are the rendered
// arguments of the v3 call. replace says the call sets instead of adding, which
// is the only form a client.Config has.
func clientHeaderOp(op clientOp, call string, keyExpr ast.Expr, key, val string, replace bool) (clientOp, clientUnsupported) {
	if keyExpr != nil {
		op.header = clientHeaderKey(keyExpr)
	} else if k, err := strconv.Unquote(key); err == nil {
		op.header = strings.ToLower(k)
	}
	switch op.header {
	case "host":
		return op, "a Host header (the v3 client cannot send a Host header that differs from the URL)"
	case "user-agent":
		// The v3 client sets these two headers itself, from the user agent and
		// the referer of the request, and discards what a header call set.
		return clientScalarOp(op, clientMemberUserAgent, val)
	case "referer":
		return clientScalarOp(op, "Referer", val)
	}
	if v, err := strconv.Unquote(val); err == nil {
		op.headerVal = v
	}
	op.req = fmt.Sprintf("%s(%s, %s)", call, key, val)
	if replace {
		op.cfg = []clientCfg{{field: clientFieldHeader, key: key, val: val}}
	}
	return op, ""
}

// clientCookieOp builds the op of a cookie setter.
func clientCookieOp(op clientOp, key, val string) (clientOp, clientUnsupported) {
	op.req = fmt.Sprintf("SetCookie(%s, %s)", key, val)
	op.cfg = []clientCfg{{field: clientFieldCookie, key: key, val: val}}
	return op, ""
}

// cookiesOp builds the op of Cookies and CookiesBytesKV, which take key and
// value pairs. A trailing key without a value is ignored, as in v2.
func (f *clientFile) cookiesOp(op clientOp, call *ast.CallExpr) (clientOp, clientUnsupported) {
	if call.Ellipsis.IsValid() {
		return op, clientUnsupported("Cookies() with a spread argument")
	}
	asBytes := op.method == clientMethodCookiesBytes
	var calls []string
	for i := 1; i < len(call.Args); i += 2 {
		key, val := f.text(call.Args[i-1]), f.text(call.Args[i])
		if asBytes {
			key, val = f.strOf(call.Args[i-1]), f.strOf(call.Args[i])
		}
		calls = append(calls, fmt.Sprintf("SetCookie(%s, %s)", key, val))
		op.cfg = append(op.cfg, clientCfg{field: clientFieldCookie, key: key, val: val})
	}
	if len(calls) == 0 {
		op.dropped = true
		return op, ""
	}
	op.req = strings.Join(calls, ".")
	return op, ""
}

// basicAuthOp builds the Authorization header v2 derived from the user name
// and password of the URI.
func (f *clientFile) basicAuthOp(op clientOp, user, pass ast.Expr, asBytes bool) (clientOp, clientUnsupported) {
	literal := func(e ast.Expr) (string, bool) {
		if x, ok := clientBytesConversion(e); ok && asBytes {
			e = x
		}
		return clientStringLit(e)
	}
	var val string
	u, uok := literal(user)
	p, pok := literal(pass)
	if uok && pok {
		val = strconv.Quote("Basic " + base64.StdEncoding.EncodeToString([]byte(u+":"+p)))
	} else {
		ut, pt := f.text(user), f.text(pass)
		if asBytes {
			ut, pt = f.strOf(user), f.strOf(pass)
		}
		b64 := f.use(clientBase64Path, "base64")
		val = fmt.Sprintf(`"Basic " + %s.StdEncoding.EncodeToString([]byte(%s + ":" + %s))`, b64, ut, pt)
	}
	op.header = "authorization"
	op.req = fmt.Sprintf(`SetHeader("Authorization", %s)`, val)
	op.cfg = []clientCfg{{field: clientFieldHeader, key: `"Authorization"`, val: val}}
	return op, ""
}

// clientQueryOp builds the op of a literal query string. v2 replaced the query of the
// URL with the string it was given, sent as is.
func clientQueryOp(op clientOp, arg ast.Expr) (clientOp, clientUnsupported) {
	if x, isConv := clientBytesConversion(arg); isConv {
		arg = x
	}
	lit, ok := clientStringLit(arg)
	if !ok {
		return op, "QueryString() with a value that is not a string literal (put the query in the URL instead)"
	}
	pairs, ok := clientParseQuery(lit)
	if !ok {
		return op, "QueryString() with a value that is not a valid query string"
	}
	op.hasQuery = true
	op.rawQuery = lit
	op.query = pairs
	calls := make([]string, 0, len(pairs))
	for _, p := range pairs {
		calls = append(calls, fmt.Sprintf("AddParam(%s, %s)", strconv.Quote(p.key), strconv.Quote(p.val)))
	}
	if len(calls) == 0 {
		op.dropped = true
		return op, ""
	}
	op.req = strings.Join(calls, ".")
	return op, ""
}

// clientParseQuery decodes a query string into its pairs, in order.
func clientParseQuery(q string) ([]clientParam, bool) {
	var pairs []clientParam
	for part := range strings.SplitSeq(q, "&") {
		if part == "" {
			continue
		}
		key, val, _ := strings.Cut(part, "=")
		k, err := url.QueryUnescape(key)
		if err != nil {
			return nil, false
		}
		v, err := url.QueryUnescape(val)
		if err != nil {
			return nil, false
		}
		pairs = append(pairs, clientParam{key: k, val: v})
	}
	return pairs, true
}

// rawBodyCfg renders the argument of Body for a client.Config, whose Body only
// treats a value of exactly the type []byte as a raw body.
func (f *clientFile) rawBodyCfg(e ast.Expr) string {
	if f.isByteSlice(e) {
		return f.text(e)
	}
	return "[]byte(" + f.text(e) + ")"
}

// isByteSlice reports whether e is evidently a []byte.
func (f *clientFile) isByteSlice(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return f.isByteSlice(x.X)
	case *ast.CompositeLit:
		return clientIsByteSliceType(x.Type)
	case *ast.CallExpr:
		return clientIsByteSliceType(x.Fun)
	case *ast.Ident:
		d, ok := f.resolve(x)
		if !ok {
			return false
		}
		if d.typ != nil {
			return clientIsByteSliceType(d.typ)
		}
		return d.init != nil && f.isByteSlice(d.init)
	}
	return false
}

// jsonConfigSafe reports whether e is evidently neither a string nor a []byte,
// the two types a client.Config sends as they are instead of encoding them.
func (f *clientFile) jsonConfigSafe(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return f.jsonConfigSafe(x.X)
	case *ast.CompositeLit:
		return !clientIsByteSliceType(x.Type)
	case *ast.UnaryExpr:
		return x.Op == token.AND && f.jsonConfigSafe(x.X)
	case *ast.Ident:
		d, ok := f.resolve(x)
		if !ok {
			return false
		}
		if d.typ != nil {
			return clientPlainDataType(d.typ)
		}
		return d.init != nil && f.jsonConfigSafe(d.init)
	}
	return false
}

// clientPlainDataType reports whether t is a type whose values a client.Config
// encodes as JSON: it is not string, []byte or an interface.
func clientPlainDataType(t ast.Expr) bool {
	switch x := t.(type) {
	case *ast.StarExpr:
		return clientPlainDataType(x.X)
	case *ast.ArrayType:
		return !clientIsByteSliceType(x)
	case *ast.MapType, *ast.StructType, *ast.SelectorExpr:
		return true
	case *ast.Ident:
		switch x.Name {
		case nameString, "any", "error", "byte":
			return false
		}
		return true
	}
	return false
}

// reqOp maps a call on the fasthttp request of a v2 agent to the v3 request.
// path is the chain of selectors below the request, "Header.Set" or
// "URI().QueryArgs().Add".
func (f *clientFile) reqOp(path string, call *ast.CallExpr, u *clientUnit) (op clientOp, reason clientUnsupported) {
	args := call.Args
	op = clientOp{method: "req." + path, line: f.g.line(call.Pos())}
	if call.Ellipsis.IsValid() {
		return op, clientUnsupported("a spread argument of req." + path + "()")
	}
	a := func(i int) string { return f.text(args[i]) }
	s := func(i int) string { return f.strOf(args[i]) }
	// nameOrBytes renders the argument of a setter that has a Bytes twin.
	nameOrBytes := func(i int) string {
		if strings.HasSuffix(path, "Bytes") || strings.HasSuffix(path, "BytesKV") {
			return s(i)
		}
		return a(i)
	}
	// arity checks the number of arguments.
	arity := func(n int) bool { return len(args) == n }

	switch path {
	case "Header.SetMethod":
		if arity(1) {
			op.req = "SetMethod(" + a(0) + ")"
			op.setsMethod = true
			return op, ""
		}
	case "SetRequestURI", "SetRequestURIBytes":
		if arity(1) && u.allowsURI() {
			op.req = "SetURL(" + nameOrBytes(0) + ")"
			op.setsURL = true
			op.urlArg = args[0]
			return op, ""
		}
	case "Header.Set", "Header.Add", "Header.SetBytesKV", "Header.AddBytesKV":
		if arity(2) {
			call := "SetHeader"
			if strings.HasPrefix(path, "Header.Add") {
				call = "AddHeader"
			}
			return clientHeaderOp(op, call, args[0], nameOrBytes(0), nameOrBytes(1), true)
		}
	case "Header.SetContentType", "Header.SetContentTypeBytes":
		if arity(1) {
			return clientHeaderOp(op, "SetHeader", nil, `"Content-Type"`, nameOrBytes(0), true)
		}
	case "Header.SetConnectionClose":
		if arity(0) {
			return clientHeaderOp(op, "SetHeader", nil, `"Connection"`, `"close"`, true)
		}
	case "Header.SetUserAgent", "Header.SetUserAgentBytes":
		if arity(1) {
			op.req = "SetUserAgent(" + nameOrBytes(0) + ")"
			return op, ""
		}
	case "Header.SetReferer", "Header.SetRefererBytes":
		if arity(1) {
			op.req = "SetReferer(" + nameOrBytes(0) + ")"
			return op, ""
		}
	case "Header.SetCookie":
		if arity(2) {
			return clientCookieOp(op, a(0), a(1))
		}
	case "SetBody":
		if arity(1) {
			op.req = "SetRawBody(" + a(0) + ")"
			op.body = clientBodyRaw
			return op, ""
		}
	case "SetBodyString":
		if arity(1) {
			op.req = "SetRawBody(" + f.bytesOf(args[0]) + ")"
			op.body = clientBodyRaw
			return op, ""
		}
	case "SetBodyStream":
		if arity(2) {
			op.stmt = clientBodyStreamStmt(a(0), a(1))
			op.body = clientBodyRaw
			return op, ""
		}
	case "URI().QueryArgs().Add", "URI().QueryArgs().Set":
		if arity(2) {
			call := "AddParam"
			if strings.HasSuffix(path, "Set") {
				call = "SetParam"
			}
			op.req = fmt.Sprintf("%s(%s, %s)", call, a(0), a(1))
			op.touchesURI = true
			return op, ""
		}
	case "Header.SetHost", "Header.SetHostBytes", "URI().SetHost", "URI().SetHostBytes":
		return op, clientHostReason
	}
	return op, clientUnsupported("req." + path + "()")
}

// clientBodyStreamStmt renders the statement that sets a body stream on the fasthttp
// request below a client.Request.
func clientBodyStreamStmt(stream, size string) func(recv string) string {
	return func(recv string) string {
		return recv + ".RawRequest.SetBodyStream(" + stream + ", " + size + ")"
	}
}
