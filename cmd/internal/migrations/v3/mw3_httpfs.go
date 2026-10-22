package v3

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofiber/cli/cmd/internal"
)

// The adapter that serves an http.FileSystem as the fs.FS v3 takes. The
// standard library has the other direction, http.FS, but not this one, and the
// file systems of packr, pkger, rice or statik, or one the project made, are
// http.FileSystems.
const (
	mw3HTTPFSType   = "fiberHTTPFS"
	mw3HTTPFSFile   = "fiber_migrate_httpfs.go"
	mw3HTTPFSMarker = `This file was added by "fiber migrate"`
)

const mw3HTTPFSSource = `// ` + mw3HTTPFSMarker + `: Fiber v3 serves an fs.FS, where the
// filesystem middleware of v2 served an http.FileSystem. Serving an embed.FS or
// os.DirFS(dir) instead makes this file unnecessary.

package %s

import (
	"io/fs"
	"net/http"
)

// fiberHTTPFS adapts an http.FileSystem (packr, pkger, rice, statik, a type of
// the project) to the fs.FS that static.Config.FS and fiber.SendFile.FS take.
type fiberHTTPFS struct{ http.FileSystem }

// Open implements fs.FS: the names of an fs.FS have no leading slash, those of
// an http.FileSystem have one.
func (a fiberHTTPFS) Open(name string) (fs.File, error) {
	if name == "." {
		name = "/"
	} else {
		name = "/" + name
	}

	f, err := a.FileSystem.Open(name)
	if err != nil {
		return nil, err
	}
	return fiberHTTPFile{f}, nil
}

// fiberHTTPFile gives an http.File the ReadDir of an fs.ReadDirFile, which is
// how a directory listing reads it.
type fiberHTTPFile struct{ http.File }

// ReadDir implements fs.ReadDirFile.
func (f fiberHTTPFile) ReadDir(n int) ([]fs.DirEntry, error) {
	infos, err := f.Readdir(n)
	entries := make([]fs.DirEntry, len(infos))
	for i, info := range infos {
		entries[i] = fs.FileInfoToDirEntry(info)
	}
	return entries, err
}
`

// ensureHTTPFSAdapter makes sure the package has the adapter, adding the file
// that declares it on first use. It reports false when the package cannot have
// it: the file of an external test package cannot see what the package declares,
// and a name of the project's own is not ours to use.
func (p *mw3Pkgs) ensureHTTPFSAdapter(fi internal.FileInfo, pkgName string) bool {
	if strings.HasSuffix(pkgName, "_test") {
		return false
	}
	pkg := p.of(fi, pkgName)
	if pkg.httpFS {
		return true
	}

	path := filepath.Join(filepath.Dir(fi.Path), mw3HTTPFSFile)
	data, err := os.ReadFile(path) // #nosec G304 -- next to a file of the project being migrated
	_, declared := pkg.declared()[mw3HTTPFSType]
	switch {
	case err == nil && bytes.Contains(data, []byte(mw3HTTPFSMarker)):
		// Added by an earlier run.
	case err == nil || declared:
		return false
	default:
		source := fmt.Sprintf(mw3HTTPFSSource, pkgName)
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil { //nolint:gosec // a source file of the project, not a secret
			return false
		}
	}

	pkg.declared()[mw3HTTPFSType] = struct{}{}
	pkg.httpFS = true
	return true
}
