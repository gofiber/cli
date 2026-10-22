// Package gomod rewrites go.mod and go.work files. It reads them with
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
	Rel string
	source
}

// source is the text of a file together with the edits scheduled for it. The
// edits are made to the original bytes, so the offsets that modfile reports for
// its syntax stay valid while a migration schedules them.
type source struct {
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
	return findFiles(root, FileName)
}

// findFiles returns the files called name below root.
func findFiles(root, name string) ([]string, error) {
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
		if d.Name() == name {
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

	f := &File{File: mf, Path: path, Rel: rel, source: source{data: data}}
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
func (s *source) apply() []byte {
	slices.SortStableFunc(s.edits, func(a, b edit) int {
		return cmp.Or(cmp.Compare(a.start, b.start), cmp.Compare(b.end, a.end))
	})

	out := make([]byte, 0, len(s.data))
	pos := 0
	for _, e := range s.edits {
		if e.start < pos || e.end < e.start || e.end > len(s.data) {
			continue
		}
		out = append(out, s.data[pos:e.start]...)
		out = append(out, e.text...)
		pos = e.end
	}
	return append(out, s.data[pos:]...)
}

// replace schedules the replacement of data[start:end]. An edit of exactly the
// same range as an earlier one takes its place.
func (s *source) replace(start, end int, text string) {
	for i, e := range s.edits {
		if e.start == start && e.end == end {
			s.edits[i].text = text
			return
		}
	}
	s.edits = append(s.edits, edit{start: start, end: end, text: text})
}

// newline is the line ending the file uses.
func (s *source) newline() string {
	if bytes.Contains(s.data, []byte("\r\n")) {
		return "\r\n"
	}
	return "\n"
}

// tokens returns the byte range of each token of line in the file, nil when
// the text does not hold as many tokens as the parsed line. A quoted token is
// found with its quotes, line.Token has them removed.
func (s *source) tokens(line *modfile.Line) [][2]int {
	var ranges [][2]int
	pos, end := line.Start.Byte, line.End.Byte
	for pos < end {
		if c := s.data[pos]; c == ' ' || c == '\t' || c == '\r' {
			pos++
			continue
		}

		start := pos
		if quote := s.data[pos]; quote == '"' || quote == '`' {
			for pos++; pos < end && s.data[pos] != quote; pos++ {
				if quote == '"' && s.data[pos] == '\\' {
					pos++
				}
			}
			pos++
		} else {
			for pos < end && s.data[pos] != ' ' && s.data[pos] != '\t' && s.data[pos] != '\r' {
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
func (s *source) setToken(line *modfile.Line, i int, text string) {
	if ranges := s.tokens(line); i < len(ranges) {
		s.replace(ranges[i][0], ranges[i][1], text)
	}
}

// deleteLine schedules the removal of the physical line that line is on,
// including its comment. A blank line that would be left doubled, or dangling
// at the end of the file, goes with it.
func (s *source) deleteLine(line *modfile.Line) {
	start := bytes.LastIndexByte(s.data[:line.Start.Byte], '\n') + 1
	end := len(s.data)
	if nl := bytes.IndexByte(s.data[line.End.Byte:], '\n'); nl >= 0 {
		end = line.End.Byte + nl + 1
	}

	prevStart, prevBlank := 0, true
	if start > 0 {
		prevStart = bytes.LastIndexByte(s.data[:start-1], '\n') + 1
		prevBlank = len(bytes.TrimSpace(s.data[prevStart:start])) == 0
	}
	if prevBlank {
		nextEnd, nextBlank := end, false
		if end < len(s.data) {
			nextEnd = len(s.data)
			if nl := bytes.IndexByte(s.data[end:], '\n'); nl >= 0 {
				nextEnd = end + nl + 1
			}
			nextBlank = len(bytes.TrimSpace(s.data[end:nextEnd])) == 0
		}
		switch {
		case end == len(s.data) && start > 0:
			start = prevStart
		case nextBlank:
			end = nextEnd
		}
	}
	s.replace(start, end, "")
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
	return f.raiseGo(f.Go, f.Toolchain, minimum, func() (at int, text string, ok bool) {
		if f.Module == nil || f.Module.Syntax == nil {
			return 0, "", false
		}

		// Put the go line after the module line, with a blank line before it.
		eol := f.newline()
		if nl := bytes.IndexByte(f.data[f.Module.Syntax.End.Byte:], '\n'); nl >= 0 {
			return f.Module.Syntax.End.Byte + nl + 1, eol + "go " + minimum + eol, true
		}
		return len(f.data), eol + eol + "go " + minimum + eol, true
	})
}

// raiseGo is RaiseGo for the go and toolchain statements of a go.mod or go.work
// file. insert says where a file without a go statement gets one.
func (s *source) raiseGo(goStmt *modfile.Go, toolchain *modfile.Toolchain, minimum string, insert func() (int, string, bool)) bool {
	want := "go" + minimum
	if !version.IsValid(want) {
		return false
	}
	if goStmt != nil && version.Compare("go"+goStmt.Version, want) >= 0 {
		return false
	}

	if goStmt != nil {
		s.setToken(goStmt.Syntax, 1, minimum)
	} else {
		at, text, ok := insert()
		if !ok {
			return false
		}
		s.replace(at, at, text)
	}

	if toolchain != nil && version.Compare(toolchain.Name, want) < 0 {
		s.deleteLine(toolchain.Syntax)
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
