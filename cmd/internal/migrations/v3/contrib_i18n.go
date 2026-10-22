package v3

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// contribI18nMentionRe finds the files that import the i18n middleware.
var contribI18nMentionRe = regexp.MustCompile(`github\.com/gofiber/contrib/(?:v3/)?(?:fiberi18n|i18n)\b`)

// i18nVarNames are the names the translator variable is tried with.
var i18nVarNames = []string{"translator", "i18nTranslator", "i18nBundle", "localizer"}

// MigrateI18nTranslator moves the code that uses the i18n module from the
// middleware of v2 to the translator of v3.
//
// v2: fiberi18n.New returned a middleware that stored the config in the locals,
// and the package functions Localize and MustLocalize read it from there. v3:
// New returns a translator (*I18n) and Localize and MustLocalize are its methods.
// A translator is not a handler, but app.Use(translator) still compiles, since
// Use takes any value, and panics when the program starts.
//
// Every app.Use(fiberi18n.New(...)) of a package becomes an assignment to a
// package level variable, declared next to it, and the calls of Localize and
// MustLocalize of that package become calls on the variable. Config.Next is gone
// with the middleware. What does not fit that pattern (several registrations,
// calls in a package that never registers, a translator that is passed around as
// a handler) is left as it is and reported.
func MigrateI18nTranslator(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	plan := &i18nPlan{pkgs: map[string]*i18nPackage{}}

	// Pass one reads the packages; nothing is written.
	_, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		plan.scan(content, fi)
		return content
	})
	if err != nil {
		return fmt.Errorf("failed to read i18n usage: %w", err)
	}
	plan.decide()

	changed, err := internal.ChangeFileContentInfo(cwd, plan.rewrite)
	if err != nil {
		return fmt.Errorf("failed to migrate i18n usage: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating i18n middleware to the translator")
	return nil
}

// i18nSite is an app.Use(fiberi18n.New(...)) statement.
type i18nSite struct {
	stmt *ast.ExprStmt
	// call is the call of New, which has nothing in front of it but the app and
	// maybe a path, and nothing behind it but a comma.
	call *ast.CallExpr
	// pkg is the name the file has for the i18n package.
	pkg string
}

// i18nUsage is what one file does with the i18n package.
type i18nUsage struct {
	sites []i18nSite
	// calls are the selectors fiberi18n.Localize and fiberi18n.MustLocalize.
	calls []*ast.SelectorExpr
	// odd are the calls of New whose result is used in a way that cannot be moved.
	odd   []*ast.CallExpr
	names []string
}

// i18nPackage is what the files of one Go package do with the i18n package.
type i18nPackage struct {
	// idents are all the identifiers used in the package, which the name of the
	// translator has to stay clear of.
	idents map[string]bool
	// sites and calls count, per file, the registrations and the calls.
	sites map[string]int
	calls map[string]int
	// tests are the files of the package that hold tests.
	tests map[string]bool
	// declFile is the file that declares the translator, name the translator.
	declFile string
	name     string
	// migrate is set when the package is moved to the translator.
	migrate bool
}

// i18nPlan is what pass one found out.
type i18nPlan struct {
	pkgs map[string]*i18nPackage
}

// packageKey identifies the Go package a file belongs to.
func i18nPackageKey(fi internal.FileInfo, pkgName string) string {
	return filepath.Dir(fi.Path) + "\x00" + pkgName
}

// scan records what the file does with the i18n package.
func (p *i18nPlan) scan(content string, fi internal.FileInfo) {
	// Identifiers are collected from every file of the package, whether or not
	// the file mentions the module.
	g, ok := parseSource(content)
	if !ok {
		return
	}
	key := i18nPackageKey(fi, g.file.Name.Name)
	pkg := p.pkgs[key]
	if pkg == nil {
		pkg = &i18nPackage{idents: map[string]bool{}, sites: map[string]int{}, calls: map[string]int{}, tests: map[string]bool{}}
		p.pkgs[key] = pkg
	}
	for name := range contribIdentNames(g.file) {
		pkg.idents[name] = true
	}
	if !contribI18nMentionRe.MatchString(content) {
		return
	}

	u := i18nAnalyze(g)
	pkg.sites[fi.Path] = len(u.sites)
	pkg.calls[fi.Path] = len(u.calls)
	pkg.tests[fi.Path] = strings.HasSuffix(fi.Path, "_test.go")
}

// decide picks the packages that are moved and the name of their translators.
func (p *i18nPlan) decide() {
	for _, pkg := range p.pkgs {
		var plain, tests []string
		plainSites := 0
		for path, n := range pkg.sites {
			switch {
			case n == 0:
			case pkg.tests[path]:
				tests = append(tests, path)
			default:
				plain = append(plain, path)
				plainSites += n
			}
		}
		slices.Sort(plain)
		slices.Sort(tests)

		callsOutsideTests := false
		for path, n := range pkg.calls {
			callsOutsideTests = callsOutsideTests || (n > 0 && !pkg.tests[path])
		}

		// One registration in the code of the program (and any number in its
		// tests) is something every call can be pointed at.
		switch {
		case plainSites == 1:
			pkg.declFile = plain[0]
		case plainSites == 0 && len(tests) > 0 && !callsOutsideTests:
			pkg.declFile = tests[0]
		default:
			continue
		}
		pkg.migrate = true
		pkg.name = i18nFreeName(pkg.idents)
	}
}

// i18nFreeName returns the first name for the translator that nothing uses.
func i18nFreeName(used map[string]bool) string {
	for _, name := range i18nVarNames {
		if !used[name] {
			return name
		}
	}
	for i := 2; ; i++ {
		if name := i18nVarNames[0] + strconv.Itoa(i); !used[name] {
			return name
		}
	}
}

// rewrite moves one file.
func (p *i18nPlan) rewrite(content string, fi internal.FileInfo) string {
	if !contribI18nMentionRe.MatchString(content) {
		return content
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return content
	}
	names := contribNames(g, "i18n")
	if len(names) == 0 {
		return content
	}

	f := newContribFile(g, fi)
	for _, c := range f.findConfigs(names) {
		if c.get("Next") != nil {
			f.notef(c.get("Next").pos(), "i18n Config.Next is gone, the translator is not a middleware that could be skipped; the function was removed")
			c.remove("Next")
		}
		c.finish()
	}

	u := i18nAnalyze(g)
	pkg := p.pkgs[i18nPackageKey(fi, g.file.Name.Name)]
	switch {
	case pkg != nil && pkg.migrate:
		f.i18nMove(u, pkg, fi.Path == pkg.declFile)
	default:
		f.i18nReport(u, pkg)
	}
	for _, call := range u.odd {
		f.notef(call.Pos(), "%s", i18nOddNote)
	}
	return contribDropOrphans(g, f.apply(), "i18n")
}

const i18nOddNote = "i18n New returns a translator now, not a handler: do not register it with Use (it panics when the program starts), " +
	"keep it in a variable and call its Localize and MustLocalize methods"

// i18nMove points the registrations and the calls of the file at the translator
// variable, which the file at declFile declares.
func (f *contribFile) i18nMove(u i18nUsage, pkg *i18nPackage, declares bool) {
	for _, s := range u.sites {
		// app.Use(path, fiberi18n.New(...)) -> translator = fiberi18n.New(...)
		f.edits.replace(f.g.offset(s.stmt.Pos()), f.g.offset(s.call.Pos()), pkg.name+" = ")
		f.edits.replace(f.g.offset(s.call.End()), f.g.offset(s.stmt.End()), "")
	}

	for _, sel := range u.calls {
		f.edits.replaceNode(f.g, sel.X, pkg.name)
	}

	if declares && len(u.sites) > 0 {
		at := f.g.offset(f.importsEnd())
		f.edits.replace(at, at, "\n\n// "+pkg.name+" serves the messages: v3 has a translator where v2 had a middleware.\nvar "+pkg.name+" *"+u.sites[0].pkg+".I18n")
	}
}

// importsEnd returns the end of the last import declaration, or of the package
// clause when there is none.
func (f *contribFile) importsEnd() token.Pos {
	end := f.g.file.Name.End()
	for _, d := range f.g.file.Decls {
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
			end = gd.End()
		}
	}
	return end
}

// i18nReport tells the user what was left of a package that is not moved.
func (f *contribFile) i18nReport(u i18nUsage, pkg *i18nPackage) {
	if len(u.calls) > 0 {
		reason := "no code of this package registers fiberi18n.New"
		if pkg != nil {
			sites := 0
			for _, n := range pkg.sites {
				sites += n
			}
			if sites > 1 {
				reason = "this package registers fiberi18n.New more than once"
			}
		}
		f.notef(u.calls[0].Pos(), "i18n Localize and MustLocalize are methods of the translator that New returns, "+
			"and %s; call them on a translator that is handed to this code", reason)
	}
	for _, s := range u.sites {
		f.notef(s.stmt.Pos(), "%s; keep the translator in a variable that the calls of Localize and MustLocalize can reach", i18nOddNote)
	}
}

// i18nAnalyze finds the registrations, the calls and the odd uses of the i18n
// package in a file.
func i18nAnalyze(g *goSource) i18nUsage {
	u := i18nUsage{names: contribNames(g, "i18n")}
	if len(u.names) == 0 {
		return u
	}

	g.walk(func(n ast.Node, stack []ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok && slices.Contains(u.names, id.Name) && (x.Sel.Name == "Localize" || x.Sel.Name == "MustLocalize") {
				u.calls = append(u.calls, x)
			}
		case *ast.CallExpr:
			sel, ok := x.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "New" {
				return true
			}
			id, ok := sel.X.(*ast.Ident)
			if !ok || !slices.Contains(u.names, id.Name) {
				return true
			}
			if site, ok := i18nSiteOf(x, id.Name, stack); ok {
				u.sites = append(u.sites, site)
			} else if !i18nIsTranslatorVar(g, x, stack) {
				u.odd = append(u.odd, x)
			}
		}
		return true
	})
	return u
}

// i18nSiteOf recognizes the call of New in app.Use(New(...)) and
// app.Use("/path", New(...)), as a statement.
func i18nSiteOf(call *ast.CallExpr, pkg string, stack []ast.Node) (site i18nSite, ok bool) {
	if len(stack) < 2 {
		return i18nSite{}, false
	}
	use, ok := stack[len(stack)-1].(*ast.CallExpr)
	if !ok {
		return i18nSite{}, false
	}
	stmt, ok := stack[len(stack)-2].(*ast.ExprStmt)
	if !ok || stmt.X != ast.Expr(use) {
		return i18nSite{}, false
	}
	if sel, isSel := use.Fun.(*ast.SelectorExpr); !isSel || sel.Sel.Name != "Use" {
		return i18nSite{}, false
	}

	switch {
	case len(use.Args) == 1 && use.Args[0] == ast.Expr(call):
	case len(use.Args) == 2 && use.Args[1] == ast.Expr(call) && i18nIsPath(use.Args[0]):
	default:
		return i18nSite{}, false
	}
	return i18nSite{stmt: stmt, call: call, pkg: pkg}, true
}

// i18nIsPath reports whether e could be the path of a Use call.
func i18nIsPath(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.BasicLit:
		return x.Kind == token.STRING
	case *ast.Ident:
		return true
	}
	return false
}

// i18nIsTranslatorVar reports whether the call of New is assigned to a variable
// that is used as a translator (the code of v3, or this migration's own output)
// or not used at all, which is nothing to report.
func i18nIsTranslatorVar(g *goSource, call *ast.CallExpr, stack []ast.Node) bool {
	if len(stack) == 0 {
		return false
	}

	var name string
	switch p := stack[len(stack)-1].(type) {
	case *ast.AssignStmt:
		if i := slices.Index(p.Rhs, ast.Expr(call)); i >= 0 && len(p.Lhs) == len(p.Rhs) {
			if id, ok := p.Lhs[i].(*ast.Ident); ok {
				name = id.Name
			}
		}
	case *ast.ValueSpec:
		if i := slices.Index(p.Values, ast.Expr(call)); i >= 0 && i < len(p.Names) {
			name = p.Names[i].Name
		}
	}
	if name == "" || name == "_" {
		return false
	}

	// A variable that is handed to something is a v2 handler; one that
	// Localize is called on is a translator.
	handler := false
	translator := false
	ast.Inspect(g.file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if isIdentNamed(x.X, name) && (x.Sel.Name == "Localize" || x.Sel.Name == "MustLocalize") {
				translator = true
			}
		case *ast.CallExpr:
			for _, arg := range x.Args {
				if isIdentNamed(arg, name) {
					handler = true
				}
			}
		}
		return true
	})
	return translator || !handler
}
