package v3

import (
	"go/ast"
	"go/token"
	"slices"
	"strings"
)

// appTLSMethods are the variants of Listen that only exist in v2.
var appTLSMethods = []string{"ListenTLS", "ListenTLSWithCertificate", "ListenMutualTLS", "ListenMutualTLSWithCertificate"}

// appStartMethods are the methods that start an app.
var appStartMethods = slices.Concat([]string{appNameListen, appNameListener}, appTLSMethods)

// appStart is a call that starts an app.
type appStart struct {
	file   *appFile
	call   *ast.CallExpr
	sel    *ast.SelectorExpr
	method string
}

// startsIn returns the calls in f that start an app.
func (f *appFile) startsIn() []appStart {
	var starts []appStart
	ast.Inspect(f.g.file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, isSel := call.Fun.(*ast.SelectorExpr)
		if !isSel || !slices.Contains(appStartMethods, sel.Sel.Name) {
			return true
		}
		if f.kind(sel.X) == appKindApp {
			starts = append(starts, appStart{file: f, call: call, sel: sel, method: sel.Sel.Name})
		}
		return true
	})
	return starts
}

// appListenField is a field of fiber.ListenConfig and the source text of its
// value.
type appListenField struct {
	name  string
	value string
}

// appStartRewrite is what turning a start call into its v3 form takes: edits
// of the file and the imports the new code needs.
type appStartRewrite struct {
	// problem says why the call cannot be rewritten, empty when it can.
	problem string
	edits   []textEdit
	imports appImports
	// notes are the review hints for the rewritten call.
	notes []string
}

// rewrite turns the call into its v3 form: the TLS variants become Listen with
// the matching fiber.ListenConfig, and extras (settings that used to be
// fiber.Config fields) are merged into the config of Listen and Listener. The
// result has no edits when the call needs none.
func (s appStart) rewrite(extras []appListenField, imports appImports) appStartRewrite {
	out := appStartRewrite{imports: imports}
	g := s.file.g

	if s.call.Ellipsis.IsValid() {
		out.problem = "the arguments are spread from a slice"
		return out
	}

	args := s.call.Args
	var base []appListenField
	switch s.method {
	case appNameListen, appNameListener:
		return s.rewritePlain(extras, out)
	case "ListenTLS":
		if len(args) != 3 {
			out.problem = "ListenTLS takes an address, a certificate file and a key file"
			return out
		}
		base = []appListenField{
			{"CertFile", g.text(args[1])},
			{"CertKeyFile", g.text(args[2])},
		}
		out.noteEmptyPaths(args[1:], "ListenTLS", "plain HTTP when CertFile and CertKeyFile are both empty")
	case "ListenMutualTLS":
		if len(args) != 4 {
			out.problem = "ListenMutualTLS takes an address, a certificate file, a key file and a client CA file"
			return out
		}
		base = []appListenField{
			{"CertFile", g.text(args[1])},
			{"CertKeyFile", g.text(args[2])},
			{"CertClientFile", g.text(args[3])},
		}
		out.noteEmptyPaths(args[1:], "ListenMutualTLS", "plain HTTP when CertFile and CertKeyFile are both empty and asks for no client certificate when CertClientFile is empty")
	case "ListenTLSWithCertificate":
		if len(args) != 2 {
			out.problem = "ListenTLSWithCertificate takes an address and a certificate"
			return out
		}
		tls := out.imports.qualifier(s.file, "crypto/tls", "tls")
		base = []appListenField{{"TLSConfig", "&" + tls + "Config{MinVersion: " + tls + "VersionTLS12, Certificates: []" + tls + "Certificate{" + g.text(args[1]) + "}}"}}
	case "ListenMutualTLSWithCertificate":
		if len(args) != 3 {
			out.problem = "ListenMutualTLSWithCertificate takes an address, a certificate and a client CA pool"
			return out
		}
		tls := out.imports.qualifier(s.file, "crypto/tls", "tls")
		base = []appListenField{{"TLSConfig", "&" + tls + "Config{MinVersion: " + tls + "VersionTLS12, ClientAuth: " + tls + "RequireAndVerifyClientCert, ClientCAs: " + g.text(args[2]) + ", Certificates: []" + tls + "Certificate{" + g.text(args[1]) + "}}"}}
	default:
		out.problem = "unknown start method " + s.method
		return out
	}

	fields := slices.Concat(base, extras)
	cfg := out.imports.fiberQualifier(s.file) + "ListenConfig{" + appJoinFields(fields) + "}"
	end := g.offset(args[len(args)-1].End())
	out.edits = append(out.edits, textEdit{
		start: g.offset(s.sel.Sel.Pos()),
		end:   end,
		text:  "Listen(" + g.text(args[0]) + ", " + cfg,
	})
	return out
}

// noteEmptyPaths records the review hint for TLS files that are not literals:
// the v2 method refused to start when they were empty, and fiber.ListenConfig
// quietly does something else.
func (r *appStartRewrite) noteEmptyPaths(paths []ast.Expr, method, instead string) {
	for _, p := range paths {
		lit, ok := p.(*ast.BasicLit)
		if ok && lit.Kind == token.STRING && lit.Value != `""` && lit.Value != "``" {
			continue
		}
		r.notes = append(r.notes, method+" returned an error for an empty certificate path, fiber.ListenConfig serves "+
			instead+": check that the paths are set before the call")
		return
	}
}

// rewritePlain handles Listen and Listener, which only gain the extra fields.
func (s appStart) rewritePlain(extras []appListenField, out appStartRewrite) appStartRewrite {
	g := s.file.g
	args := s.call.Args

	switch len(args) {
	case 1:
		if len(extras) == 0 {
			return out
		}
		cfg := out.imports.fiberQualifier(s.file) + "ListenConfig{" + appJoinFields(extras) + "}"
		at := g.offset(args[0].End())
		out.edits = append(out.edits, textEdit{start: at, end: at, text: ", " + cfg})
	case 2:
		if len(extras) == 0 {
			return out
		}
		lit, ok := args[1].(*ast.CompositeLit)
		if !ok || lit.Type == nil || s.file.fiberTypeName(lit.Type) != appNameListenConfig {
			out.problem = "its fiber.ListenConfig is not a literal the settings can be added to"
			return out
		}
		var add []appListenField
		for _, e := range extras {
			if idx, _ := appKeyedElement(lit, e.name); idx >= 0 {
				out.problem = "its fiber.ListenConfig already sets " + e.name
				return out
			}
			add = append(add, e)
		}
		if len(lit.Elts) == 0 {
			at := g.offset(lit.Lbrace) + 1
			out.edits = append(out.edits, textEdit{start: at, end: at, text: appJoinFields(add)})
		} else {
			at := g.offset(lit.Elts[len(lit.Elts)-1].End())
			out.edits = append(out.edits, textEdit{start: at, end: at, text: ", " + appJoinFields(add)})
		}
	default:
		out.problem = "it does not take an address and an optional fiber.ListenConfig"
	}
	return out
}

func appJoinFields(fields []appListenField) string {
	parts := make([]string, len(fields))
	for i, f := range fields {
		parts[i] = f.name + ": " + f.value
	}
	return strings.Join(parts, ", ")
}

// appListenerFieldNames maps the fiber.Config fields that v3 moved to
// fiber.ListenConfig to their new names, in the order they are written.
var appListenerFieldNames = []struct{ v2, v3 string }{
	{"Prefork", "EnablePrefork"},
	{"Network", "ListenerNetwork"},
	{appFieldQuiet, appFieldQuiet},
	{appFieldPrintRoutes, appFieldPrintRoutes},
}

// The settings of fiber.Config that only change what Listen prints.
const (
	appFieldQuiet       = "DisableStartupMessage"
	appFieldPrintRoutes = "EnablePrintRoutes"
)

// appListenerFieldV3 returns the ListenConfig name of a fiber.Config field that
// moved, empty for every other field.
func appListenerFieldV3(v2 string) string {
	for _, f := range appListenerFieldNames {
		if f.v2 == v2 {
			return f.v3
		}
	}
	return ""
}
