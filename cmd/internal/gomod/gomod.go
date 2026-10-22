// Package gomod rewrites go.mod files with golang.org/x/mod/modfile, so that a
// migration sees requirements, replace directives and the go line as the go
// command does: block or single-line, with comments, CRLF line endings and
// in nested modules.
package gomod

import (
	"bytes"
	"errors"
	"fmt"
	"go/version"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"

	"github.com/gofiber/cli/cmd/internal"
)

// FileName is the name of a module file.
const FileName = "go.mod"

// File is a go.mod file that is being rewritten. The embedded modfile.File can
// be read freely; use the methods of File to change a requirement or a replace
// directive, so that the file is written back and its syntax stays consistent.
type File struct {
	*modfile.File
	// Path is the location of the file.
	Path string
	// Rel is Path relative to the walked directory, with forward slashes. It is
	// what notes refer to.
	Rel     string
	crlf    bool
	changed bool
}

// Paths returns the go.mod files below root in walk order. Directories named
// vendor, node_modules and .git are not entered.
func Paths(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "vendor", "node_modules", ".git":
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == FileName {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", root, err)
	}
	return paths, nil
}

// Edit parses every go.mod below root (see Paths) and calls fn with it. A file
// that fn changed is written back, with its formatting normalized the way
// `go mod edit` does and its line endings kept; the other files are not
// touched. A go.mod that does not parse is skipped and reported as a note.
// Edit returns whether any file was written.
func Edit(root string, fn func(f *File) error) (bool, error) {
	paths, err := Paths(root)
	if err != nil {
		return false, err
	}

	changed := false
	for _, path := range paths {
		written, err := editFile(root, path, fn)
		if err != nil {
			return changed, err
		}
		changed = changed || written
	}
	return changed, nil
}

func editFile(root, path string, fn func(f *File) error) (bool, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- reading a module file of the migrated project
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}

	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	rel = filepath.ToSlash(rel)

	mf, err := parse(path, data)
	if err != nil {
		note := internal.Note{File: rel, Message: "skipped because go.mod does not parse: " + err.Error()}
		var list modfile.ErrorList
		if errors.As(err, &list) && len(list) > 0 {
			note.Line, note.Message = list[0].Pos.Line, "skipped because go.mod does not parse: "+list[0].Err.Error()
		}
		internal.AddNote(root, note)
		return false, nil
	}

	f := &File{File: mf, Path: path, Rel: rel, crlf: usesCRLF(data)}
	if err := fn(f); err != nil {
		return false, err
	}
	if !f.changed {
		return false, nil
	}

	out, err := f.render()
	if err != nil {
		return false, fmt.Errorf("format %s: %w", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, fmt.Errorf("stat %s: %w", path, err)
	}
	if err := os.WriteFile(path, out, info.Mode().Perm()); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, nil
}

// parse reads a go.mod strictly and, when a directive that this version of
// modfile does not know is in the way, leniently: the unknown lines are kept
// when the file is written back.
func parse(path string, data []byte) (*modfile.File, error) {
	mf, err := modfile.Parse(path, data, nil)
	if err == nil {
		return mf, nil
	}
	lax, laxErr := modfile.ParseLax(path, data, nil)
	if laxErr != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return lax, nil
}

// usesCRLF reports whether every line of data ends in a carriage return and a
// line feed.
func usesCRLF(data []byte) bool {
	crlf := bytes.Count(data, []byte("\r\n"))
	return crlf > 0 && crlf == bytes.Count(data, []byte("\n"))
}

func (f *File) render() ([]byte, error) {
	f.Cleanup()
	out, err := f.Format()
	if err != nil {
		return nil, fmt.Errorf("format: %w", err)
	}
	if f.crlf {
		out = bytes.ReplaceAll(out, []byte("\n"), []byte("\r\n"))
	}
	return out, nil
}

// MarkChanged makes Edit write the file back after fn returns. The methods of
// File that change the file do it themselves; this is for changes made through
// the embedded modfile.File.
func (f *File) MarkChanged() {
	f.changed = true
}

// SetRequire points the requirement r at module path at version.
func (f *File) SetRequire(r *modfile.Require, path, vers string) {
	line := r.Syntax
	pathAt := 0
	if !line.InBlock {
		pathAt = 1 // the require keyword comes first
	}
	if len(line.Token) < pathAt+2 {
		return
	}
	line.Token[pathAt] = modfile.AutoQuote(path)
	line.Token[pathAt+1] = vers
	r.Mod = module.Version{Path: path, Version: vers}
	f.changed = true
}

// DropRequire removes the requirement r.
func (f *File) DropRequire(r *modfile.Require) {
	r.Syntax.Token = nil
	r.Syntax.Suffix = nil
	*r = modfile.Require{}
	f.changed = true
}

// SetReplaceOld changes the module that the replace directive r applies to.
// An empty vers makes it apply to every version.
func (f *File) SetReplaceOld(r *modfile.Replace, path, vers string) {
	line := r.Syntax
	first := 0
	if !line.InBlock {
		first = 1 // the replace keyword comes first
	}
	arrow := slices.Index(line.Token, "=>")
	if arrow <= first {
		return
	}

	tokens := slices.Clone(line.Token[:first])
	tokens = append(tokens, modfile.AutoQuote(path))
	if vers != "" {
		tokens = append(tokens, vers)
	}
	line.Token = append(tokens, line.Token[arrow:]...)
	r.Old = module.Version{Path: path, Version: vers}
	f.changed = true
}

// DedupeRequires collapses requirements of the same module into one, the
// first of them, at the highest version. Retargeting a requirement can leave
// two of them for the same module.
func (f *File) DedupeRequires() {
	first := map[string]*modfile.Require{}
	for _, r := range f.Require {
		if r.Mod.Path == "" {
			continue
		}
		kept, seen := first[r.Mod.Path]
		if !seen {
			first[r.Mod.Path] = r
			continue
		}
		if semver.Compare(r.Mod.Version, kept.Mod.Version) > 0 {
			f.SetRequire(kept, kept.Mod.Path, r.Mod.Version)
		}
		f.DropRequire(r)
	}
}

// RaiseGo makes the go directive at least minimum, adding it when the file has
// none. A newer go directive is never lowered. A toolchain directive that is
// older than the raised go directive is dropped, as `go get go@version` does.
// It reports whether the file changed.
func (f *File) RaiseGo(minimum string) bool {
	want := "go" + minimum
	if !version.IsValid(want) {
		return false
	}
	if f.Go != nil && version.Compare("go"+f.Go.Version, want) >= 0 {
		return false
	}
	if err := f.AddGoStmt(minimum); err != nil {
		return false
	}
	if f.Toolchain != nil && version.Compare(f.Toolchain.Name, want) < 0 {
		f.DropToolchainStmt()
	}
	f.changed = true
	return true
}

// Requires returns the requirement of module path, nil when there is none.
func (f *File) Requires(path string) *modfile.Require {
	for _, r := range f.Require {
		if r.Mod.Path == path {
			return r
		}
	}
	return nil
}

// Line returns the 1-based line of r, 0 when it is unknown.
func Line(syntax *modfile.Line) int {
	if syntax == nil {
		return 0
	}
	return syntax.Start.Line
}
