package migrations

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/gomod"
)

var (
	// reFiberImport matches the import path of the core fiber module or one of
	// its packages, at any major version.
	reFiberImport = regexp.MustCompile(`^github\.com/gofiber/fiber/v(\d+)(/.*)?$`)
	// reFiberModulePath matches the module path of the core fiber module with a
	// major version.
	reFiberModulePath = regexp.MustCompile(`^github\.com/gofiber/fiber/v\d+$`)
	// reFiberMention finds the core fiber module path in a line of text.
	reFiberMention = regexp.MustCompile(`github\.com/gofiber/fiber/v(\d+)`)
)

// MigrateGoPkgs moves the project to the module path and version of the target
// Fiber release: the imports of the Go files and the requirement in every go.mod
// below the working directory. Places it cannot rewrite safely, such as build
// scripts and replace directives, are left as they are and reported.
func MigrateGoPkgs(cmd *cobra.Command, cwd string, _, target *semver.Version) error {
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return rewriteFiberImports(content, fi, target.Major())
	})
	if err != nil {
		return fmt.Errorf("failed to migrate Go packages: %w", err)
	}

	modChanged, err := gomod.Edit(cwd, func(f *gomod.File) error {
		retargetFiberModule(cwd, f, target)
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to migrate go.mod: %w", err)
	}

	if err := noteFiberReferences(cwd, target.Major()); err != nil {
		return err
	}

	if !changed && !modChanged {
		return nil
	}

	cmd.Println("Migrating Go packages")

	return nil
}

// rewriteFiberImports moves the import paths of the core fiber module in one Go
// file to the given major version. Only import declarations are changed, not
// strings, struct tags or comments that happen to contain the path.
func rewriteFiberImports(content string, fi internal.FileInfo, major uint64) string {
	if !strings.Contains(content, "github.com/gofiber/fiber/") {
		return content
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, fi.Path, content, parser.ImportsOnly|parser.SkipObjectResolution)
	if err != nil {
		internal.NoteParseError(fi, err)
		return content
	}

	out := content
	// Back to front, so that the offsets of the imports still to do stay valid.
	for _, spec := range slices.Backward(file.Imports) {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		m := reFiberImport.FindStringSubmatch(path)
		if len(m) == 0 || m[1] == strconv.FormatUint(major, 10) {
			continue
		}

		quote := `"`
		if strings.HasPrefix(spec.Path.Value, "`") {
			quote = "`"
		}
		start, end := fset.Position(spec.Path.Pos()).Offset, fset.Position(spec.Path.End()).Offset
		out = out[:start] + quote + fmt.Sprintf("github.com/gofiber/fiber/v%d%s", major, m[2]) + quote + out[end:]
	}

	noteGenerateDirectives(out, fi, major)

	return out
}

// noteGenerateDirectives reports //go:generate lines that name the core fiber
// module at another major than the target. The directive is a comment as far
// as the compiler is concerned, so it cannot be rewritten like an import.
func noteGenerateDirectives(content string, fi internal.FileInfo, major uint64) {
	if !strings.Contains(content, "//go:generate") {
		return
	}

	for i, line := range strings.Split(content, "\n") {
		if !strings.HasPrefix(line, "//go:generate") {
			continue
		}
		if path := staleFiberMention(line, major); path != "" {
			internal.Notef(fi, i+1, "`//go:generate` runs %s, which is not the migrated Fiber module; update the directive by hand.", path)
		}
	}
}

// staleFiberMention returns the first core fiber module path in line whose major
// version is not the given one.
func staleFiberMention(line string, major uint64) string {
	for _, m := range reFiberMention.FindAllStringSubmatch(line, -1) {
		if m[1] != strconv.FormatUint(major, 10) {
			return m[0]
		}
	}
	return ""
}

// retargetFiberModule points the requirements of the core fiber module in one
// go.mod at the target release.
func retargetFiberModule(cwd string, f *gomod.File, target *semver.Version) {
	path := fmt.Sprintf("github.com/gofiber/fiber/v%d", target.Major())
	vers := "v" + target.String()

	for _, r := range f.Requirements(reFiberModulePath.MatchString) {
		f.SetRequire(r, path, vers)
	}
	f.DedupeRequires()

	for _, rep := range f.Replacements(reFiberModulePath.MatchString) {
		if rep.Old.Path != path {
			internal.AddNote(cwd, internal.Note{
				File: f.Rel,
				Line: gomod.Line(rep.Syntax),
				Message: fmt.Sprintf("the replace directive for %s does not apply to %s; add one for the new module path if you still need it.",
					rep.Old.Path, path),
			})
		}
	}
}

// noteFiberReferences reports files other than Go source that still name the
// core fiber module at another major version than the target: build scripts,
// Dockerfiles, CI workflows. They are not rewritten, a script may pin the old
// version on purpose.
func noteFiberReferences(cwd string, major uint64) error {
	err := filepath.WalkDir(cwd, func(path string, d fs.DirEntry, err error) error {
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
		if !mentionsModules(d.Name()) {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > maxReferenceFileSize {
			return nil //nolint:nilerr // a file we cannot stat is not worth failing the migration for
		}
		b, err := os.ReadFile(path) // #nosec G304 -- reading a text file of the migrated project
		if err != nil {
			return nil //nolint:nilerr // unreadable files are skipped
		}

		rel, err := filepath.Rel(cwd, path)
		if err != nil {
			rel = path
		}
		for i, line := range strings.Split(string(b), "\n") {
			if mention := staleFiberMention(line, major); mention != "" {
				internal.AddNote(cwd, internal.Note{
					File: filepath.ToSlash(rel),
					Line: i + 1,
					Message: fmt.Sprintf("mentions %s; it is github.com/gofiber/fiber/v%d now. The CLI does not edit this kind of file, update it by hand.",
						mention, major),
				})
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("scan for references to the fiber module: %w", err)
	}
	return nil
}

// maxReferenceFileSize bounds the files noteFiberReferences reads.
const maxReferenceFileSize = 1 << 20

// mentionsModules reports whether a file of this name is a build script, a
// container file or a CI definition, the places where a module path is spelled
// out for the go tool.
func mentionsModules(name string) bool {
	lower := strings.ToLower(name)
	switch lower {
	case "makefile", "gnumakefile", "justfile", "go.work":
		return true
	}
	if strings.HasPrefix(lower, "dockerfile") || strings.HasPrefix(lower, "containerfile") {
		return true
	}
	switch filepath.Ext(lower) {
	case ".mk", ".yml", ".yaml", ".sh", ".bash", ".zsh", ".toml", ".cmd", ".bat", ".ps1", ".dockerfile":
		return true
	}
	return false
}
