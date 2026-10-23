package v3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// textEdit replaces content[start:end] with text.
type textEdit struct {
	start, end int
	text       string
}

// sourceEdits is a parsed Go file and the edits a migration makes to it. The edits are
// applied to the text of the file, so that everything a migration does not touch stays as
// its author wrote it.
type sourceEdits struct {
	content string
	fset    *token.FileSet
	file    *ast.File
	edits   []textEdit
}

// parseForEdits parses content. It reports false when the content does not parse, and the
// migration leaves the file alone.
func parseForEdits(content string) (*sourceEdits, bool) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", content, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, false
	}
	return &sourceEdits{content: content, fset: fset, file: file}, true
}

func (s *sourceEdits) offset(pos token.Pos) int {
	return s.fset.Position(pos).Offset
}

// text returns the source text of the node.
func (s *sourceEdits) text(n ast.Node) string {
	return s.content[s.offset(n.Pos()):s.offset(n.End())]
}

// replace replaces the text of the node.
func (s *sourceEdits) replace(n ast.Node, text string) {
	s.edits = append(s.edits, textEdit{start: s.offset(n.Pos()), end: s.offset(n.End()), text: text})
}

// insertAfter inserts text right behind the node.
func (s *sourceEdits) insertAfter(n ast.Node, text string) {
	end := s.offset(n.End())
	s.edits = append(s.edits, textEdit{start: end, end: end, text: text})
}

// apply returns the content with the edits made. Of edits that overlap, the one that starts
// first is made.
func (s *sourceEdits) apply() string {
	edits := slices.Clone(s.edits)
	slices.SortStableFunc(edits, func(a, b textEdit) int {
		if a.start != b.start {
			return a.start - b.start
		}
		return b.end - a.end
	})

	var b strings.Builder
	last := 0
	for _, e := range edits {
		if e.start < last {
			continue
		}
		b.WriteString(s.content[last:e.start])
		b.WriteString(e.text)
		last = e.end
	}
	b.WriteString(s.content[last:])
	return b.String()
}

// importName returns the name the file gives the package whose import path matches re, or
// "" when the file does not import it (or only for its side effects).
func (s *sourceEdits) importName(re *regexp.Regexp) string {
	for _, spec := range s.file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || !re.MatchString(path) {
			continue
		}
		if spec.Name != nil {
			if spec.Name.Name == "_" || spec.Name.Name == "." {
				continue
			}
			return spec.Name.Name
		}
		return packageNameOf(path)
	}
	return ""
}

// packageNameOf returns the name a package of Fiber has, which is the last element of its
// import path before a major version.
func packageNameOf(path string) string {
	parts := strings.Split(path, "/")
	last := parts[len(parts)-1]
	if len(parts) > 1 && len(last) > 1 && last[0] == 'v' && strings.Trim(last[1:], "0123456789") == "" {
		last = parts[len(parts)-2]
	}
	return last
}

// middlewarePath matches the import path of a middleware of Fiber, at any major version.
func middlewarePath(name string) *regexp.Regexp {
	return regexp.MustCompile(`^github\.com/gofiber/fiber/v\d+/middleware/` + regexp.QuoteMeta(name) + `$`)
}
