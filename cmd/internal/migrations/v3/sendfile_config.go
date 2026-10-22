package v3

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// ctxDownload is the Ctx method that sends a file as an attachment.
const ctxDownload = "Download"

// MigrateSendFileConfig turns the optional compress flag of Ctx.SendFile into
// the SendFile config struct. v2 always served byte ranges, v3 only does with
// ByteRange set, so the migrated calls set it to keep range requests (media
// seeking, resumed downloads) working. Download has no config in v3 and so no
// byte ranges either: it is reported.
func MigrateSendFileConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	run := newCtxRun()
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return migrateSendFileContent(content, fi, run)
	})
	run.report(cwd)
	if err != nil {
		return fmt.Errorf("failed to migrate SendFile calls: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating SendFile compress flag")
	return nil
}

func migrateSendFileContent(content string, fi internal.FileInfo, run *ctxRun) string {
	if !strings.Contains(content, "SendFile") && !strings.Contains(content, ctxDownload) {
		return content
	}
	g, env, ok := run.open(fi, content)
	if !ok {
		return content
	}
	noter := newCtxNoter(fi, g, run)
	var edits editSet
	var values ctxPlan
	g.walk(func(n ast.Node, stack []ast.Node) bool {
		if sel, isSel := n.(*ast.SelectorExpr); isSel {
			ctxQueueSendFileValue(&values, env, sel, stack)
			return true
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || env.isCtxType(sel.X, stack) {
			return true
		}

		switch sel.Sel.Name {
		case mw3SendFile:
			switch env.receiverKind(sel, stack) {
			case ctxYes:
				rewriteSendFileCall(env, &edits, fi, call, stack)
			case ctxUnknown:
				if len(call.Args) == 2 && isBoolLiteral(call.Args[1]) {
					internal.Notef(fi, g.line(call.Pos()),
						"SendFile(..., %s): if the receiver is a fiber context, the flag is now %s{Compress: %s, ByteRange: true}",
						g.text(call.Args[1]), env.spell("SendFile"), g.text(call.Args[1]))
				}
			case ctxNo:
			}
		case ctxDownload:
			if env.receiverKind(sel, stack) == ctxYes {
				noter.oncef("download", sel.Pos(),
					"%s no longer serves byte ranges (resumable downloads) in Fiber v3. Where that matters, call %s.Attachment(name), then %s.SendFile(path, %sSendFile{ByteRange: true})",
					g.text(sel), g.text(sel.X), g.text(sel.X), env.spell(""))
			}
		}
		return true
	})

	values.run(&edits)
	return env.finish(content, edits.apply(content))
}

// ctxQueueSendFileValue queues the replacement of c.SendFile used as a value,
// without being called, with a function literal that has the signature of the v2
// method: the compress flag moves into the config (see ctxValueBuilder).
func ctxQueueSendFileValue(plan *ctxPlan, env *ctxEnv, sel *ast.SelectorExpr, stack []ast.Node) {
	if sel.Sel.Name != mw3SendFile {
		return
	}

	// A method expression, (*fiber.Ctx).SendFile, is a value even when it is
	// called: the context is its first argument.
	if !env.isCtxType(sel.X, stack) {
		if _, isCall := ctxParentCall(sel, stack); isCall || env.receiverKind(sel, stack) != ctxYes {
			return
		}
	}
	b := env.newValueBuilder(sel, stack, nil)
	plan.add(func(e *editSet) { e.replaceNode(env.g, sel, b.text(e, b.sendFile())) })
}

// rewriteSendFileCall schedules the edits that move the arguments of a
// Ctx.SendFile call to the config struct.
func rewriteSendFileCall(env *ctxEnv, edits *editSet, fi internal.FileInfo, call *ast.CallExpr, stack []ast.Node) {
	g := env.g

	switch {
	case len(call.Args) == 0:
	case call.Ellipsis.IsValid() || len(call.Args) > 2:
		internal.Notef(fi, g.line(call.Pos()),
			"SendFile takes a single %s config now, convert the compress flags by hand (set ByteRange: true to keep range requests)", env.spell("SendFile"))
	case len(call.Args) == 1:
		end := g.offset(call.Args[0].End())
		edits.replace(end, end, ", "+env.qual(call.Pos(), "SendFile")+"{ByteRange: true}")
	case !isSendFileConfig(call.Args[1], env.names, stack, 0):
		cfgType := env.qual(call.Pos(), "SendFile")
		compress := g.text(call.Args[1])
		cfg := cfgType + "{Compress: " + compress + ", ByteRange: true}"
		if compress == litFalse {
			cfg = cfgType + "{ByteRange: true}"
		}
		edits.replaceNode(g, call.Args[1], cfg)
	}
}

// maxSendFileConfigDepth bounds how far isSendFileConfig follows variables.
const maxSendFileConfigDepth = 3

// isSendFileConfig reports whether e already is a SendFile config, which means
// the call was migrated before: a literal of the type, a pointer to one, or a
// variable declared as one.
func isSendFileConfig(e ast.Expr, names []string, stack []ast.Node, depth int) bool {
	if depth > maxSendFileConfigDepth {
		return false
	}

	switch x := e.(type) {
	case *ast.ParenExpr:
		return isSendFileConfig(x.X, names, stack, depth+1)
	case *ast.UnaryExpr:
		return x.Op == token.AND && isSendFileConfig(x.X, names, stack, depth+1)
	case *ast.CompositeLit:
		return x.Type != nil && ctxIsFiberType(x.Type, names, "SendFile")
	case *ast.Ident:
		d, ok := resolveIdent(x.Name, x.Pos(), stack)
		if !ok {
			return false
		}
		if d.typ != nil {
			return ctxIsFiberType(d.typ, names, "SendFile")
		}
		return d.init != nil && isSendFileConfig(d.init, names, stack, depth+1)
	}
	return false
}

// The predeclared boolean identifiers.
const (
	litTrue  = "true"
	litFalse = "false"
)

// isBoolLiteral reports whether e is the identifier true or false.
func isBoolLiteral(e ast.Expr) bool {
	return isIdentNamed(e, litTrue) || isIdentNamed(e, litFalse)
}
