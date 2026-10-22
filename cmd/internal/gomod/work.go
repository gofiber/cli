package gomod

import (
	"fmt"
	"go/version"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"

	"github.com/gofiber/cli/cmd/internal"
)

// WorkFileName is the name of a workspace file.
const WorkFileName = "go.work"

// WorkFile is a go.work file that is being rewritten.
type WorkFile struct {
	*modfile.WorkFile
	// Path is the location of the file.
	Path string
	// Rel is Path relative to the walked directory, with forward slashes.
	Rel string
	source
}

// RaiseGo makes the go directive at least minimum, adding it when the file has
// none. A newer go directive is never lowered. It reports whether the file
// changed.
func (w *WorkFile) RaiseGo(minimum string) bool {
	return w.raiseGo(w.Go, w.Toolchain, minimum, func() (at int, text string, ok bool) {
		eol := w.newline()
		return 0, "go " + minimum + eol + eol, true
	})
}

// SyncWork raises the go line of every go.work file below root to the highest go
// line of the modules it uses: the go command refuses a workspace whose go line
// is lower than the one of a module in it, and the migrations raise the go line
// of the modules. A workspace that does not parse is skipped with a note. It
// returns whether any file was written.
//
// When root is a module of a workspace whose go.work is in a directory above it,
// the file is not the CLI's to change, but a note says that it is behind.
func SyncWork(root string) (bool, error) {
	paths, err := findFiles(root, WorkFileName)
	if err != nil {
		return false, err
	}

	changed := false
	for _, path := range paths {
		written, err := syncWorkFile(root, path)
		if err != nil {
			return changed, err
		}
		changed = changed || written
	}

	if !slices.ContainsFunc(paths, func(p string) bool { return filepath.Dir(p) == filepath.Clean(root) }) {
		noteWorkAbove(root)
	}
	return changed, nil
}

// noteWorkAbove reports the go.work file that the go command would use for root,
// the nearest one in a directory above it, when it uses a module below root and
// its go line is behind the one of its modules.
func noteWorkAbove(root string) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return
	}

	for dir := filepath.Dir(abs); ; dir = filepath.Dir(dir) {
		path := filepath.Join(dir, WorkFileName)
		if b, err := os.ReadFile(path); err == nil { // #nosec G304 -- the workspace file above the migrated directory
			noteWorkBehind(root, abs, path, b)
			return
		}
		if filepath.Dir(dir) == dir {
			return
		}
	}
}

// noteWorkBehind adds the note for the workspace file path, whose content is b.
func noteWorkBehind(root, abs, path string, b []byte) {
	work, err := modfile.ParseWork(path, b, nil)
	if err != nil || work.Go == nil {
		return
	}

	usesRoot := slices.ContainsFunc(work.Use, func(use *modfile.Use) bool {
		dir := use.Path
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(filepath.Dir(path), dir)
		}
		rel, err := filepath.Rel(abs, dir)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	})
	need := workGoVersion(path, work)
	if !usesRoot || need == "" || version.Compare("go"+work.Go.Version, "go"+need) >= 0 {
		return
	}

	rel, err := filepath.Rel(abs, path)
	if err != nil {
		rel = path
	}
	internal.AddNote(root, internal.Note{
		File: filepath.ToSlash(rel),
		Line: Line(work.Go.Syntax),
		Message: fmt.Sprintf("the go line of this workspace (go %s) is behind the modules it uses (go %s), and the go command refuses to build such a workspace; "+
			"run `go work use` or raise the line by hand. The CLI only changes the go.work files below the directory it runs in.", work.Go.Version, need),
	})
}

func syncWorkFile(root, path string) (bool, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- reading a workspace file of the migrated project
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	rel = filepath.ToSlash(rel)

	work, err := modfile.ParseWork(path, data, nil)
	if err != nil {
		internal.AddNote(root, parseNote(rel, "skipped because go.work does not parse", err))
		return false, nil
	}

	need := workGoVersion(path, work)
	if need == "" {
		return false, nil
	}

	w := &WorkFile{WorkFile: work, Path: path, Rel: rel, source: source{data: data}}
	if !w.RaiseGo(need) {
		return false, nil
	}

	out := w.apply()
	if _, err := modfile.ParseWork(path, out, nil); err != nil {
		internal.AddNote(root, parseNote(rel, "left unchanged because the rewritten go.work does not parse (please report this)", err))
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

// workGoVersion returns the highest go line among the modules the workspace
// uses, "" when none of them says.
func workGoVersion(path string, work *modfile.WorkFile) string {
	highest := ""
	for _, use := range work.Use {
		dir := use.Path
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(filepath.Dir(path), dir)
		}
		b, err := os.ReadFile(filepath.Join(dir, FileName)) // #nosec G304 -- a module of the workspace
		if err != nil {
			continue
		}
		mf, err := modfile.ParseLax(filepath.Join(dir, FileName), b, nil)
		if err != nil || mf.Go == nil {
			continue
		}
		if highest == "" || version.Compare("go"+mf.Go.Version, "go"+highest) > 0 {
			highest = mf.Go.Version
		}
	}
	return highest
}
