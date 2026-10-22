// Package gomod rewrites go.mod files. It reads them with
// golang.org/x/mod/modfile, so that a migration sees requirements, replace
// directives and the go line the way the go command does: block or single-line,
// with comments, CRLF line endings and in nested modules. The changes are made
// to the text of the file, not by formatting the parsed file again, so that
// everything a migration does not touch stays byte for byte as it was.
package gomod

import (
	"bytes"
	"cmp"
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
// be read freely; it keeps describing the file as it was parsed, apart from the
// requirements and replace directives that the methods of File change. Use the
// methods of File to change the file.
type File struct {
	*modfile.File
	// Path is the location of the file.
	Path string
	// Rel is Path relative to the walked directory, with forward slashes. It is
	// what notes refer to.
	Rel   string
	data  []byte
	edits []edit
}

// edit replaces data[start:end] with text.
type edit struct {
	text       string
	start, end int
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
// that fn changed is written back; the other files are not touched. A go.mod
// that does not parse is skipped and reported as a note. Edit returns whether
// any file was written.
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
		internal.AddNote(root, parseNote(rel, "skipped because go.mod does not parse", err))
		return false, nil
	}

	f := &File{File: mf, Path: path, Rel: rel, data: data}
	if err := fn(f); err != nil {
		return false, err
	}
	if len(f.edits) == 0 {
		return false, nil
	}

	out := f.apply()
	if _, err := parse(path, out); err != nil {
		internal.AddNote(root, parseNote(rel, "left unchanged because the rewritten go.mod does not parse (please report this)", err))
		return false, nil
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

// parseNote describes a go.mod that could not be parsed, at the line the
// parser stopped at when it says so.
func parseNote(rel, what string, err error) internal.Note {
	note := internal.Note{File: rel, Message: what + ": " + err.Error()}
	var list modfile.ErrorList
	if errors.As(err, &list) && len(list) > 0 {
		note.Line, note.Message = list[0].Pos.Line, what+": "+list[0].Err.Error()
	}
	return note
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

// apply returns the file with the edits made. An edit that overlaps an earlier
// one (the outer edit wins) is dropped.
func (f *File) apply() []byte {
	slices.SortStableFunc(f.edits, func(a, b edit) int {
		return cmp.Or(cmp.Compare(a.start, b.start), cmp.Compare(b.end, a.end))
	})

	out := make([]byte, 0, len(f.data))
	pos := 0
	for _, e := range f.edits {
		if e.start < pos || e.end < e.start || e.end > len(f.data) {
			continue
		}
		out = append(out, f.data[pos:e.start]...)
		out = append(out, e.text...)
		pos = e.end
	}
	return append(out, f.data[pos:]...)
}

// replace schedules the replacement of data[start:end]. An edit of exactly the
// same range as an earlier one takes its place.
func (f *File) replace(start, end int, text string) {
	for i, e := range f.edits {
		if e.start == start && e.end == end {
			f.edits[i].text = text
			return
		}
	}
	f.edits = append(f.edits, edit{start: start, end: end, text: text})
}

// newline is the line ending the file uses.
func (f *File) newline() string {
	if bytes.Contains(f.data, []byte("\r\n")) {
		return "\r\n"
	}
	return "\n"
}

// tokens returns the byte range of each token of line in the file, nil when
// the text does not hold as many tokens as the parsed line. A quoted token is
// found with its quotes, line.Token has them removed.
func (f *File) tokens(line *modfile.Line) [][2]int {
	var ranges [][2]int
	pos, end := line.Start.Byte, line.End.Byte
	for pos < end {
		if c := f.data[pos]; c == ' ' || c == '\t' || c == '\r' {
			pos++
			continue
		}

		start := pos
		if quote := f.data[pos]; quote == '"' || quote == '`' {
			for pos++; pos < end && f.data[pos] != quote; pos++ {
				if quote == '"' && f.data[pos] == '\\' {
					pos++
				}
			}
			pos++
		} else {
			for pos < end && f.data[pos] != ' ' && f.data[pos] != '\t' && f.data[pos] != '\r' {
				pos++
			}
		}
		ranges = append(ranges, [2]int{start, min(pos, end)})
	}

	if len(ranges) != len(line.Token) {
		return nil
	}
	return ranges
}

// setToken schedules the replacement of token i of line.
func (f *File) setToken(line *modfile.Line, i int, text string) {
	if ranges := f.tokens(line); i < len(ranges) {
		f.replace(ranges[i][0], ranges[i][1], text)
	}
}

// deleteLine schedules the removal of the physical line that line is on,
// including its comment. A blank line that would be left doubled, or dangling
// at the end of the file, goes with it.
func (f *File) deleteLine(line *modfile.Line) {
	start := bytes.LastIndexByte(f.data[:line.Start.Byte], '\n') + 1
	end := len(f.data)
	if nl := bytes.IndexByte(f.data[line.End.Byte:], '\n'); nl >= 0 {
		end = line.End.Byte + nl + 1
	}

	prevStart, prevBlank := 0, true
	if start > 0 {
		prevStart = bytes.LastIndexByte(f.data[:start-1], '\n') + 1
		prevBlank = len(bytes.TrimSpace(f.data[prevStart:start])) == 0
	}
	if prevBlank {
		nextEnd, nextBlank := end, false
		if end < len(f.data) {
			nextEnd = len(f.data)
			if nl := bytes.IndexByte(f.data[end:], '\n'); nl >= 0 {
				nextEnd = end + nl + 1
			}
			nextBlank = len(bytes.TrimSpace(f.data[end:nextEnd])) == 0
		}
		switch {
		case end == len(f.data) && start > 0:
			start = prevStart
		case nextBlank:
			end = nextEnd
		}
	}
	f.replace(start, end, "")
}

// SetRequire points the requirement r at module path at version vers.
func (f *File) SetRequire(r *modfile.Require, path, vers string) {
	line := r.Syntax
	pathAt := 0
	if !line.InBlock {
		pathAt = 1 // the require keyword comes first
	}
	if len(line.Token) < pathAt+2 {
		return
	}

	if r.Mod.Path != path {
		f.setToken(line, pathAt, modfile.AutoQuote(path))
	}
	if r.Mod.Version != vers {
		f.setToken(line, pathAt+1, vers)
	}
	r.Mod = module.Version{Path: path, Version: vers}
}

// DropRequire removes the requirement r.
func (f *File) DropRequire(r *modfile.Require) {
	f.deleteLine(r.Syntax)
	r.Mod = module.Version{}
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
	ranges := f.tokens(line)
	if arrow <= first || ranges == nil {
		return
	}

	if r.Old.Path != path {
		f.setToken(line, first, modfile.AutoQuote(path))
	}

	hasVers := arrow > first+1
	pathEnd := ranges[first][1]
	switch {
	case hasVers && vers != "":
		if r.Old.Version != vers {
			f.setToken(line, first+1, vers)
		}
	case hasVers:
		f.replace(pathEnd, ranges[first+1][1], "")
	case vers != "":
		f.replace(pathEnd, pathEnd, " "+vers)
	}
	r.Old = module.Version{Path: path, Version: vers}
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

	switch {
	case f.Go != nil:
		f.setToken(f.Go.Syntax, 1, minimum)
	case f.Module != nil && f.Module.Syntax != nil:
		// Put the go line after the module line, with a blank line before it.
		eol := f.newline()
		at, text := len(f.data), eol+eol+"go "+minimum+eol
		if nl := bytes.IndexByte(f.data[f.Module.Syntax.End.Byte:], '\n'); nl >= 0 {
			at, text = f.Module.Syntax.End.Byte+nl+1, eol+"go "+minimum+eol
		}
		f.replace(at, at, text)
	default:
		return false
	}

	if f.Toolchain != nil && version.Compare(f.Toolchain.Name, want) < 0 {
		f.deleteLine(f.Toolchain.Syntax)
	}
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

// Line returns the 1-based line of a statement, 0 when it is unknown.
func Line(syntax *modfile.Line) int {
	if syntax == nil {
		return 0
	}
	return syntax.Start.Line
}

// Requirements lists the requirements of f that satisfy match, skipping the
// ones that were dropped.
func (f *File) Requirements(match func(path string) bool) []*modfile.Require {
	var out []*modfile.Require
	for _, r := range f.Require {
		if r.Mod.Path != "" && match(r.Mod.Path) {
			out = append(out, r)
		}
	}
	return out
}

// Replacements lists the replace directives of f that satisfy match on the
// path they apply to.
func (f *File) Replacements(match func(path string) bool) []*modfile.Replace {
	var out []*modfile.Replace
	for _, r := range f.Replace {
		if r.Old.Path != "" && match(r.Old.Path) {
			out = append(out, r)
		}
	}
	return out
}
