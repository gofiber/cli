package v3

import (
	"fmt"
	"go/ast"
	"go/token"
	"regexp"
	"slices"
	"strconv"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// ctxReNotFound matches the body Fiber v2 answers an unmatched route with.
var ctxReNotFound = regexp.MustCompile(`^Cannot (GET|HEAD|POST|PUT|PATCH|DELETE|CONNECT|OPTIONS|TRACE) `)

// ctxPlainMediaTypes are the media types of a response whose Content-Type gains
// "; charset=utf-8" in v3 when it is sent by c.JSON, c.XML or c.Type, as plain
// constants and as literals.
var ctxPlainMediaTypes = []string{"MIMEApplicationJSON", "MIMEApplicationXML", "MIMETextHTML", "MIMETextXML"}

var ctxPlainMediaLiterals = []string{"application/json", "application/xml", "text/html", "text/xml"}

// MigrateCtxBehaviorNotes points at the places where the behavior of a response
// changed in a way that tests tend to pin, which no rewrite can follow: the body
// v2 answered an unmatched route with, and the exact Content-Type a test
// expects. It does not change any file.
func MigrateCtxBehaviorNotes(_ *cobra.Command, cwd string, _, _ *semver.Version) error {
	_, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		ctxBehaviorNotes(content, fi)
		return content
	})
	if err != nil {
		return fmt.Errorf("failed to check the behavior of responses: %w", err)
	}
	return nil
}

func ctxBehaviorNotes(content string, fi internal.FileInfo) {
	notFound := strings.Contains(content, "Cannot ")
	isTest := strings.HasSuffix(fi.Rel, "_test.go")
	if !notFound && !isTest {
		return
	}
	names := fiberNames(content)
	if len(names) == 0 {
		return
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return
	}

	g.walk(func(n ast.Node, _ []ast.Node) bool {
		switch x := n.(type) {
		case *ast.BasicLit:
			if notFound && x.Kind == token.STRING {
				if value, err := strconv.Unquote(x.Value); err == nil && ctxReNotFound.MatchString(value) {
					internal.Notef(fi, g.line(x.Pos()),
						"Fiber v3 answers an unmatched route with 404 \"Not Found\" (405 \"Method Not Allowed\" when the path exists for another method), not %q: update what this expects",
						value)
				}
			}
		case *ast.CallExpr:
			if isTest && !ctxIsPrefixCheck(x) && ctxIsContentTypeAssertion(x.Args, names) {
				internal.Notef(fi, g.line(x.Pos()), ctxCharsetMessage)
			}
		case *ast.BinaryExpr:
			if isTest && (x.Op == token.EQL || x.Op == token.NEQ) && ctxIsContentTypeAssertion([]ast.Expr{x.X, x.Y}, names) {
				internal.Notef(fi, g.line(x.Pos()), ctxCharsetMessage)
			}
		}
		return true
	})
}

// ctxCharsetMessage is the note for an exact comparison of a Content-Type.
const ctxCharsetMessage = "Fiber v3 adds \"; charset=utf-8\" to the Content-Type that c.JSON, c.XML and c.Type send, so this exact comparison can fail: compare with the ...CharsetUTF8 constant or use strings.HasPrefix"

// ctxIsPrefixCheck reports whether call is strings.HasPrefix or a like check,
// which is how the comparison should be written.
func ctxIsPrefixCheck(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && slices.Contains([]string{"HasPrefix", "Contains", "Index"}, sel.Sel.Name)
}

// ctxIsContentTypeAssertion reports whether the operands of a call or
// comparison hold a plain media type and read the Content-Type of a response.
func ctxIsContentTypeAssertion(operands []ast.Expr, names []string) bool {
	plain, header := false, false
	for _, operand := range operands {
		plain = plain || ctxIsPlainMediaType(operand, names)
		header = header || ctxReadsContentType(operand, names)
	}
	return plain && header
}

// ctxIsPlainMediaType reports whether operand is a media type constant or
// literal that v3 sends with a charset.
func ctxIsPlainMediaType(operand ast.Expr, names []string) bool {
	switch x := operand.(type) {
	case *ast.BasicLit:
		value, err := strconv.Unquote(x.Value)
		return err == nil && x.Kind == token.STRING && slices.Contains(ctxPlainMediaLiterals, value)
	case *ast.SelectorExpr:
		id, ok := x.X.(*ast.Ident)
		return ok && slices.Contains(names, id.Name) && slices.Contains(ctxPlainMediaTypes, x.Sel.Name)
	}
	return false
}

// ctxReadsContentType reports whether operand reads the Content-Type of a
// response: Header.Get("Content-Type"), GetRespHeader(fiber.HeaderContentType)
// or the ContentType() of fasthttp's headers.
func ctxReadsContentType(operand ast.Expr, names []string) bool {
	found := false
	ast.Inspect(operand, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || found {
			return !found
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		switch sel.Sel.Name {
		case "ContentType":
			found = len(call.Args) == 0
		case "Get", "GetRespHeader":
			found = len(call.Args) == 1 && ctxIsContentTypeName(call.Args[0], names)
		}
		return !found
	})
	return found
}

// ctxIsContentTypeName reports whether arg is "Content-Type" or the constant of
// the fiber package for it.
func ctxIsContentTypeName(arg ast.Expr, names []string) bool {
	switch x := arg.(type) {
	case *ast.BasicLit:
		value, err := strconv.Unquote(x.Value)
		return err == nil && strings.EqualFold(value, "Content-Type")
	case *ast.SelectorExpr:
		id, ok := x.X.(*ast.Ident)
		return ok && slices.Contains(names, id.Name) && x.Sel.Name == "HeaderContentType"
	}
	return false
}
