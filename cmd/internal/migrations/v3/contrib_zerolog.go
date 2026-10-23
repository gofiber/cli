package v3

import (
	"fmt"
	"go/ast"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// contribZerologPaths are the paths the zerolog middleware is imported by.
var contribZerologPaths = contribPathsOf("fiberzerolog", "zerolog")

// MigrateZerologConfig replaces the fields of the zerolog Config that v3 dropped.
//
//   - SkipURIs, a list of paths that were not logged, is part of Next: the
//     middleware did not run for them, which is what Next does.
//   - SkipBody and SkipResBody decided whether the body and the response body
//     were logged. SkipField decides that for every field, given its name.
func MigrateZerologConfig(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContentInfo(cwd, migrateZerologContent)
	if err != nil {
		return fmt.Errorf("failed to migrate zerolog config: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating zerolog configs")
	return nil
}

func migrateZerologContent(content string, fi internal.FileInfo) string {
	if !mentionsPath(content, contribZerologPaths...) {
		return content
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return content
	}
	names := contribNames(g, "zerolog")
	if len(names) == 0 {
		return content
	}

	f := newContribFile(g, fi)
	for _, c := range f.findConfigs(names) {
		f.zerologConfig(c)
	}
	return f.apply()
}

// zerologConfig migrates one zerolog Config.
func (f *contribFile) zerologConfig(c *contribConfig) {
	defer c.finish()

	uris, body, resBody := c.get("SkipURIs"), c.get("SkipBody"), c.get("SkipResBody")
	if uris == nil && body == nil && resBody == nil {
		return
	}
	if !c.settled("SkipURIs", "SkipBody", "SkipResBody", "Next") {
		at := uris
		if at == nil {
			at = body
		}
		if at == nil {
			at = resBody
		}
		f.notef(at.pos(), "zerolog Config: SkipURIs, SkipBody or SkipResBody is set conditionally; move it to Next and SkipField by hand")
		return
	}

	if uris != nil {
		f.zerologSkipURIs(c, uris)
	}
	if body != nil || resBody != nil {
		f.zerologSkipFields(c, body, resBody)
	}
}

// zerologSkipURIs moves the SkipURIs of the config to its Next.
func (f *contribFile) zerologSkipURIs(c *contribConfig, uris *contribSlot) {
	// Nothing to skip.
	if lit, ok := uris.value.(*ast.CompositeLit); (ok && len(lit.Elts) == 0) || contribIsNil(uris.value) {
		c.remove("SkipURIs")
		return
	}

	// The list is read for every request now, so it has to be a value, not a call.
	switch uris.value.(type) {
	case *ast.CompositeLit, *ast.Ident, *ast.SelectorExpr:
	default:
		f.notef(uris.pos(), "zerolog Config: SkipURIs is gone, add its paths to Next "+
			"(return slices.Contains(paths, c.Path())); %s is a call, which v2 evaluated once", f.g.text(uris.value))
		return
	}
	slicesPkg, ok := f.stdName("slices")
	if !ok {
		f.notef(uris.pos(), "zerolog Config: SkipURIs is gone, add its paths to Next (return slices.Contains(paths, c.Path()))")
		return
	}
	paths := f.g.text(uris.value)
	check := func(param string) string { return slicesPkg + ".Contains(" + paths + ", " + param + ".Path())" }

	next := c.get("Next")
	if next == nil || contribIsNil(next.value) {
		param := contribFreeName(uris.value, "c", "ctx", "fctx")
		closure := "func(" + param + " " + f.fiberName() + ".Ctx) bool { return " + check(param) + " }"
		if next == nil {
			c.replace("SkipURIs", "Next", closure)
			return
		}
		c.remove("SkipURIs")
		f.edits.replaceNode(f.g, next.value, closure)
		return
	}

	// v2 ran the Next function first and looked at the paths when it said no.
	if p, ok := contribPredicateOf(next.value); ok && p.param != "" {
		f.edits.replaceNode(f.g, p.ret.Results[0], f.g.text(p.ret.Results[0])+" || "+check(p.param))
	} else {
		param := contribFreeName(next.value, "c", "ctx", "fctx")
		f.edits.replaceNode(f.g, next.value,
			"func("+param+" "+f.fiberName()+".Ctx) bool { return "+contribCallText(f.g, next.value)+"("+param+") || "+check(param)+" }")
	}
	c.remove("SkipURIs")
}

// zerologSkipFields moves SkipBody and SkipResBody to one SkipField function.
func (f *contribFile) zerologSkipFields(c *contribConfig, body, resBody *contribSlot) {
	if c.get("SkipField") != nil {
		return
	}

	type skip struct {
		slot  *contribSlot
		field string
	}
	var skips []skip
	for _, s := range []skip{{body, "FieldBody"}, {resBody, "FieldResBody"}} {
		if s.slot != nil && !contribIsNil(s.slot.value) {
			skips = append(skips, s)
		}
	}
	if len(skips) == 0 {
		c.remove("SkipBody")
		c.remove("SkipResBody")
		return
	}
	if c.pkg == "" {
		return
	}

	// The context parameter of the first function that has a name is the one the
	// function we write uses, so that the body of that function can be inlined.
	param := ""
	for _, s := range skips {
		if p, ok := contribPredicateOf(s.slot.value); ok && p.param != "" && p.param != "field" {
			param = p.param
			break
		}
	}
	if param == "" {
		param = contribFreeName(skips[0].slot.value, "c", "ctx", "fctx")
		for _, s := range skips[1:] {
			param = contribFreeName(s.slot.value, param, "ctx", "fctx")
		}
	}

	cases := make([]string, 0, len(skips))
	for _, s := range skips {
		var result string
		if p, ok := contribPredicateOf(s.slot.value); ok && p.param == param {
			result = f.g.text(p.ret.Results[0])
		} else {
			result = contribCallText(f.g, s.slot.value) + "(" + param + ")"
		}
		cases = append(cases, "case "+c.pkg+"."+s.field+":\n\t\treturn "+result)
	}
	closure := "func(field string, " + param + " " + f.fiberName() + ".Ctx) bool {\n\tswitch field {\n\t" +
		strings.Join(cases, "\n\t") + "\n\t}\n\treturn false\n}"

	switch {
	case body != nil:
		c.replace("SkipBody", "SkipField", closure)
		c.remove("SkipResBody")
	default:
		c.replace("SkipResBody", "SkipField", closure)
	}
}
