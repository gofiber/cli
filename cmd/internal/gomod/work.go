package gomod

import (
	"fmt"
	"go/version"
	"os"
	"path/filepath"

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
	return changed, nil
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
