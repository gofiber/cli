package v3

import (
	"go/ast"
)

// What the middleware for Fiber v3 takes from the project beyond its own API:
// contrib/v3/sentry requires sentry-go 0.50, so a module that imports it leaves
// the version of sentry-go it had (0.16 for fibersentry 1.0.4, 0.29 for 1.0.8,
// whatever the project asked for) and the code has to compile against 0.50.
// Besides the API that MigrateSentryAPI rewrites with a replacement of the
// vendor (the extras, the logger, the options of logs and metrics), 0.50 lacks
// the names below, which went in sentry-go 0.20 to 0.29 after a time as
// deprecated:
//
//   - the span options TransactionName, OpName, TransctionSource and SpanSampled,
//     which are WithTransactionName, WithOpName, WithTransactionSource and
//     WithSpanSampled since 0.21 (removed in 0.26), and the constant Version,
//     which is SDKVersion (removed in 0.25): plain renames;
//   - Scope.SetTransaction and Scope.Transaction (removed in 0.20): the name of a
//     transaction belongs to its span, so there is no call that takes their
//     place;
//   - the constant SDKIdentifier (removed in 0.25), which Client.GetSDKIdentifier
//     returns.
//
// A project that does not use the middleware keeps its version of sentry-go, so
// its code is left alone: renaming would break a build against an old SDK that
// does not have the new names.

// sentryRenamed maps the names of sentry-go that were renamed and then removed to
// their new names.
var sentryRenamed = map[string]string{
	"TransactionName":  "WithTransactionName",
	"OpName":           "WithOpName",
	"TransctionSource": "WithTransactionSource",
	"SpanSampled":      "WithSpanSampled",
	"Version":          "SDKVersion",
}

// the names of sentry-go that only this file asks about
const (
	sentrySetTransaction = "SetTransaction"
	sentryTransaction    = "Transaction"
	sentrySDKIdentifier  = "SDKIdentifier"
)

// onCurrentSDK reports whether the module of the file is on sentry-go 0.50 once
// the migration is done: the file or one of the others of its module imports the
// middleware of Fiber v3. Outside of a module only the file itself can say.
func (f *sentryFile) onCurrentSDK() bool {
	if f.names.v3 {
		return true
	}
	if f.mf == nil || f.mf.ix == nil {
		return false
	}

	ix := f.mf.ix
	if v, known := f.run.sdk[ix.mod.root]; known {
		return v
	}
	if f.run.sdk == nil {
		f.run.sdk = map[string]bool{}
	}
	// the imports are not what this migration writes: the answer holds for the run
	v := len(ix.importers(sentryContribV3Import)) > 0
	f.run.sdk[ix.mod.root] = v
	return v
}

// renamed migrates the use of a name that sentry-go renamed. A selector that is
// none of them is left alone, and so is every name of a module that keeps the
// version of sentry-go it has.
func (f *sentryFile) renamed(sel *ast.SelectorExpr) {
	if newName, ok := sentryRenamed[sel.Sel.Name]; ok && f.onCurrentSDK() {
		f.edits.replaceNode(f.g, sel.Sel, newName)
	}
}

// sdkIdentifier reports the use of sentry.SDKIdentifier.
func (f *sentryFile) sdkIdentifier(sel *ast.SelectorExpr) {
	if sel.Sel.Name != sentrySDKIdentifier || !f.onCurrentSDK() {
		return
	}
	f.rep.notef(sel.Sel, "sentry.SDKIdentifier was removed in sentry-go 0.25: Client.GetSDKIdentifier() returns it (left as it is)")
}

// transactionCall reports the calls of Scope.SetTransaction and Scope.Transaction.
func (f *sentryFile) transactionCall(call *ast.CallExpr, fun *ast.SelectorExpr, stack []ast.Node) {
	name := fun.Sel.Name
	args := 1
	if name == sentryTransaction {
		args = 0
	}
	if len(call.Args) != args || call.Ellipsis.IsValid() || !f.onCurrentSDK() {
		return
	}

	// a receiver that the file does not tell is reported with a hedge, as for the
	// extras of a scope
	hedge := ""
	switch f.kindOf(fun.X, stack) {
	case sentryScope:
	case sentryUnknown:
		if !f.names.imported() {
			return
		}
		hedge = "; if " + f.g.text(fun.X) + " is not a *sentry.Scope, ignore this"
	default:
		return
	}

	if name == sentrySetTransaction {
		f.rep.notef(fun.Sel, "Scope.SetTransaction was removed in sentry-go 0.20: the name of a transaction belongs to its span, give it where the span starts "+
			"(sentry.StartSpan(ctx, op, sentry.WithTransactionName(name))) or set Span.Name (left as it is%s)", hedge)
		return
	}
	f.rep.notef(fun.Sel, "Scope.Transaction was removed in sentry-go 0.20: the name of a transaction is the Name of its span, "+
		"read it from the span (Scope.GetSpan(), or the SamplingContext of a sampler) (left as it is%s)", hedge)
}
