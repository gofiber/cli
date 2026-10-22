package v3

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofiber/cli/cmd/internal"
)

// ctxRun collects what a migration has to say once for the whole project
// rather than at each place: a behavior change that applies to every use of an
// API is reported at its first use, with the number of the others.
type ctxRun struct {
	kinds map[string]*ctxKindNote
	// pkgs are the parsed files of the directories looked at, by directory.
	pkgs  map[string][]ctxPackageFile
	order []string
}

// ctxPackageFile is a parsed file of a directory.
type ctxPackageFile struct {
	file  *ast.File
	path  string
	names []string
}

// ctxKindNote is the first place of a kind of note, and how many places there are.
type ctxKindNote struct {
	file    string
	message string
	line    int
	places  int
}

func newCtxRun() *ctxRun {
	return &ctxRun{kinds: map[string]*ctxKindNote{}, pkgs: map[string][]ctxPackageFile{}}
}

// ctxIsIgnored reports whether file is excluded from the build with a
// "//go:build ignore" line, which makes it no part of its package.
func ctxIsIgnored(file *ast.File) bool {
	for _, group := range file.Comments {
		if group.End() > file.Package {
			break
		}
		for _, c := range group.List {
			if constraint, ok := strings.CutPrefix(c.Text, "//go:build "); ok && strings.TrimSpace(constraint) == "ignore" {
				return true
			}
		}
	}
	return false
}

// siblings returns the other files of the package of the file fi, g, which
// declare the types, functions and methods the file may use. The files of a
// directory are read once per run.
func (r *ctxRun) siblings(fi internal.FileInfo, g *goSource) []ctxSibling {
	dir := filepath.Dir(fi.Path)
	files, loaded := r.pkgs[dir]
	if !loaded {
		files = ctxParseDir(dir)
		r.pkgs[dir] = files
	}

	var siblings []ctxSibling
	for _, f := range files {
		if f.path != fi.Path && f.file.Name.Name == g.file.Name.Name {
			siblings = append(siblings, ctxSibling{file: f.file, names: f.names})
		}
	}
	return siblings
}

// ctxParseDir parses the Go files of dir, without those that do not parse.
func ctxParseDir(dir string) []ctxPackageFile {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var files []ctxPackageFile
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		src, err := os.ReadFile(path) // #nosec G304 -- a file of the project that is migrated
		if err != nil {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, src, parser.SkipObjectResolution|parser.ParseComments)
		if err != nil || ctxIsIgnored(file) {
			continue
		}
		files = append(files, ctxPackageFile{file: file, path: path, names: fiberNames(string(src))})
	}
	return files
}

// report records the collected notes for the project in root.
func (r *ctxRun) report(root string) {
	for _, kind := range r.order {
		k := r.kinds[kind]
		msg := k.message
		if k.places > 1 {
			msg += fmt.Sprintf(" (%d more in this project)", k.places-1)
		}
		internal.AddNote(root, internal.Note{File: k.file, Line: k.line, Message: msg})
	}
}

// ctxNoter hands the notes of one file to the internal package.
type ctxNoter struct {
	g   *goSource
	run *ctxRun
	fi  internal.FileInfo
}

func newCtxNoter(fi internal.FileInfo, g *goSource, run *ctxRun) *ctxNoter {
	return &ctxNoter{fi: fi, g: g, run: run}
}

// notef records a note for the construct at pos.
func (n *ctxNoter) notef(pos token.Pos, format string, args ...any) {
	internal.Notef(n.fi, n.g.line(pos), format, args...)
}

// oncef records a note of the given kind for the construct at pos. Only the first
// place of a kind in the project is reported, with the number of the others.
func (n *ctxNoter) oncef(kind string, pos token.Pos, format string, args ...any) {
	if k, seen := n.run.kinds[kind]; seen {
		k.places++
		return
	}
	n.run.kinds[kind] = &ctxKindNote{file: n.fi.Rel, line: n.g.line(pos), message: fmt.Sprintf(format, args...), places: 1}
	n.run.order = append(n.run.order, kind)
}
