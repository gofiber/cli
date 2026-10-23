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

// The logger tags this migration rewrites.
const (
	mw2LoggerHeaderTag    = "${header:"
	mw2LoggerReqHeaderTag = "${reqHeader:"
	mw2LoggerLocalsTag    = "${locals:"
)

// MigrateLoggerTags renames what v3 changed in the logger's template language:
//   - The TagHeader constant is TagReqHeader, and the ${header:X} tag of a format
//     string is ${reqHeader:X}. v3 does not know the old tag and logger.New panics
//     at startup on it ("unknown template tag").
//   - ${locals:requestid}, the example of the v2 documentation, prints nothing in
//     v3, because the middleware keep their values under private keys now. The
//     middleware register tags of their own: ${requestid}, ${api-key},
//     ${username} and ${csrf-token}. A ${locals:key} tag becomes the tag of the
//     middleware that stores a value under the key; the keys the application
//     writes itself are left alone.
//
// The constant is found through the logger's import name. Format strings are
// rewritten in the files of every module that imports the logger, because a
// format kept in a constant often lives in a file of its own.
func MigrateLoggerTags(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	index := newMW2LocalsIndex(cwd)
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return migrateLoggerTagsContent(content, fi, index)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate logger tags: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating logger tag constants")
	return nil
}

func migrateLoggerTagsContent(content string, fi internal.FileInfo, index *mw2LocalsIndex) string {
	hasConstant := strings.Contains(content, "TagHeader")
	hasHeader := strings.Contains(content, mw2LoggerHeaderTag)
	hasLocals := strings.Contains(content, mw2LoggerLocalsTag)
	if !hasConstant && !hasHeader && !hasLocals {
		return content
	}

	names := importNames(content, mw2Logger, "logger")
	if len(names) == 0 && ((!hasHeader && !hasLocals) || !index.modules.imports(fi.Path, mw2Logger)) {
		return content
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return content
	}

	r := &mw2TagRewriter{g: g, fi: fi, index: index, names: names}
	g.walk(r.visit)
	return r.edits.apply(content)
}

// mw2TagRewriter rewrites the tags of one file.
type mw2TagRewriter struct {
	g     *goSource
	index *mw2LocalsIndex
	edits editSet
	// names are the identifiers the file binds to the logger package.
	names []string
	fi    internal.FileInfo
}

func (r *mw2TagRewriter) visit(n ast.Node, _ []ast.Node) bool {
	switch x := n.(type) {
	case *ast.SelectorExpr:
		if mw2IsSelector(x, r.names, "TagHeader") {
			r.edits.replaceNode(r.g, x.Sel, "TagReqHeader")
		}
	case *ast.BasicLit:
		if x.Kind == token.STRING {
			r.headerTags(x)
			r.localsTags(x)
		}
	}
	return true
}

// headerTags rewrites the ${header:X} tags of a string literal.
func (r *mw2TagRewriter) headerTags(lit *ast.BasicLit) {
	start := r.g.offset(lit.Pos())
	for i := 0; ; {
		idx := strings.Index(lit.Value[i:], mw2LoggerHeaderTag)
		if idx < 0 {
			return
		}
		at := start + i + idx
		r.edits.replace(at, at+len(mw2LoggerHeaderTag), mw2LoggerReqHeaderTag)
		i += idx + len(mw2LoggerHeaderTag)
	}
}

// localsTags rewrites the ${locals:key} tags of a string literal that name a key
// a middleware stores a value under.
func (r *mw2TagRewriter) localsTags(lit *ast.BasicLit) {
	m := r.index.module(r.fi.Path)
	start := r.g.offset(lit.Pos())
	for i := 0; ; {
		idx := strings.Index(lit.Value[i:], mw2LoggerLocalsTag)
		if idx < 0 {
			return
		}
		tagStart := i + idx
		end := strings.IndexByte(lit.Value[tagStart:], '}')
		i = tagStart + len(mw2LoggerLocalsTag)
		if end < 0 {
			return
		}

		name := lit.Value[tagStart+len(mw2LoggerLocalsTag) : tagStart+end]
		role, ok := m.owner(mw2Key{val: name})
		if !ok {
			continue
		}
		line := r.g.line(lit.Pos())
		if role.logTag == "" {
			internal.Notef(r.fi, line,
				"%s%s} prints nothing in v3, where %s keeps its value under a private key. Register a tag that calls %s (logger.Config.CustomTags).",
				mw2LoggerLocalsTag, name, role.pkg, mw2Readers([]*mw2Role{role}))
			continue
		}

		r.edits.replace(start+tagStart, start+tagStart+end+1, "${"+role.logTag+"}")
		if caveat := r.tagCaveat(role); caveat != "" {
			internal.Notef(r.fi, line, "%s%s} became ${%s}.%s", mw2LoggerLocalsTag, name, role.logTag, caveat)
		}
	}
}

// tagCaveat says what differs for the tag of a middleware in v3.
func (r *mw2TagRewriter) tagCaveat(role *mw2Role) string {
	caveat := ""
	if role.masked {
		caveat = " v3 prints a masked prefix, not the whole value."
	}
	if !r.constructedFirst(role) {
		caveat += fmt.Sprintf(" The tag is filled once %s.New() has run: construct it before logger.New().", role.pkg)
	}
	return caveat
}

// constructedFirst reports whether the file calls the constructor of the role's
// middleware before the logger's: the tag is only known to the logger then.
func (r *mw2TagRewriter) constructedFirst(role *mw2Role) bool {
	roleNames := importNames(r.g.src, role.re, role.pkg)
	if len(roleNames) == 0 || len(r.names) == 0 {
		return false
	}

	first := func(names []string) int {
		at := -1
		r.g.walk(func(n ast.Node, _ []ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && mw2IsSelector(sel, names, mw2New) && (at < 0 || r.g.offset(sel.Pos()) < at) {
				at = r.g.offset(sel.Pos())
			}
			return true
		})
		return at
	}
	middleware, logger := first(roleNames), first(r.names)
	return middleware >= 0 && logger >= 0 && middleware < logger
}
