package v3

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// ctxTagRenames are the struct tag keys the v2 parsers read and the keys the v3
// binders read instead: ParamsParser read "params", ReqHeaderParser read
// "reqHeader", Bind().URI() and Bind().Header() read "uri" and "header".
var ctxTagRenames = map[string]string{
	"params":    "uri",
	"reqHeader": ctxTagHeader,
}

// ctxTagHeader is the key the binder of the request headers reads.
const ctxTagHeader = "header"

// MigrateParamsTagKeys renames the struct tags the parsers of v2 read to the
// ones the binders of v3 read. A struct is bound in code that may sit in another
// file or package, so every struct tag of the project is looked at; the keys
// are specific to Fiber's parsers.
func MigrateParamsTagKeys(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := internal.ChangeFileContentInfo(cwd, rewriteParamsTagKeys)
	if err != nil {
		return fmt.Errorf("failed to migrate params tag keys: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating struct tag keys of the parsers")
	return nil
}

func rewriteParamsTagKeys(content string, fi internal.FileInfo) string {
	if !strings.Contains(content, `params:"`) && !strings.Contains(content, `reqHeader:"`) &&
		!strings.Contains(content, `params:\"`) && !strings.Contains(content, `reqHeader:\"`) {
		return content
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return content
	}

	var edits editSet
	g.walk(func(n ast.Node, _ []ast.Node) bool {
		field, ok := n.(*ast.Field)
		if !ok || field.Tag == nil || field.Tag.Kind != token.STRING {
			return true
		}

		tag, err := strconv.Unquote(field.Tag.Value)
		if err != nil {
			return true
		}
		renamed, conflicts := ctxRenameTagKeys(tag)
		for _, key := range conflicts {
			internal.Notef(fi, g.line(field.Tag.Pos()),
				"the struct tag has a %q and a %q key; Fiber v3 reads %q, so check which one should stay (the %q key was not renamed)",
				key, ctxTagRenames[key], ctxTagRenames[key], key)
		}
		if renamed == tag {
			return true
		}

		literal := strconv.Quote(renamed)
		if strings.HasPrefix(field.Tag.Value, "`") {
			literal = "`" + renamed + "`"
		}
		edits.replaceNode(g, field.Tag, literal)
		return true
	})

	return edits.apply(content)
}

// ctxRenameTagKeys renames the keys of a struct tag listed in ctxTagRenames and
// leaves everything else of the tag as it is. A key is not renamed when the tag
// has the new key already; it is returned in conflicts instead.
func ctxRenameTagKeys(tag string) (renamed string, conflicts []string) {
	type keySpan struct {
		name       string
		start, end int
	}

	var keys []keySpan
	for pos := 0; pos < len(tag); {
		for pos < len(tag) && tag[pos] == ' ' {
			pos++
		}
		start := pos
		for pos < len(tag) && tag[pos] > ' ' && tag[pos] != ':' && tag[pos] != '"' && tag[pos] != 0x7f {
			pos++
		}
		if pos == start || pos+1 >= len(tag) || tag[pos] != ':' || tag[pos+1] != '"' {
			break
		}
		keys = append(keys, keySpan{name: tag[start:pos], start: start, end: pos})

		// skip the quoted value
		pos += 2
		for pos < len(tag) && tag[pos] != '"' {
			if tag[pos] == '\\' {
				pos++
			}
			pos++
		}
		pos++
	}

	has := func(name string) bool {
		for _, k := range keys {
			if k.name == name {
				return true
			}
		}
		return false
	}

	var b strings.Builder
	last := 0
	for _, k := range keys {
		to, rename := ctxTagRenames[k.name]
		if !rename {
			continue
		}
		if has(to) {
			conflicts = append(conflicts, k.name)
			continue
		}
		b.WriteString(tag[last:k.start]) //nolint:errcheck // WriteString never returns an error
		b.WriteString(to)                //nolint:errcheck // WriteString never returns an error
		last = k.end
	}
	b.WriteString(tag[last:]) //nolint:errcheck // WriteString never returns an error
	return b.String(), conflicts
}
