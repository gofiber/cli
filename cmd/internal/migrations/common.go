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
	// importsReFiberImport matches the import path of the core fiber module or one of
	// its packages, at any major version.
	importsReFiberImport = regexp.MustCompile(`^github\.com/gofiber/fiber/v(\d+)(/.*)?$`)
	// importsReFiberModulePath matches the module path of the core fiber module with a
	// major version.
	importsReFiberModulePath = regexp.MustCompile(`^github\.com/gofiber/fiber/v\d+$`)
)

// importsFiberModuleStem is what the path of the core fiber module reads up to its major
// version.
const importsFiberModuleStem = "github.com/gofiber/fiber/v"

// MigrateGoPkgs moves the project to the module path and version of the target
// Fiber release: the imports of the Go files and the requirement in every go.mod
// below the working directory. Places it cannot rewrite safely, such as build
// scripts and replace directives, are left as they are and reported.
func MigrateGoPkgs(cmd *cobra.Command, cwd string, _, target *semver.Version) error {
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return importsRewriteFiberImports(content, fi, target.Major())
	})
	if err != nil {
		return fmt.Errorf("failed to migrate Go packages: %w", err)
	}

	modChanged, err := gomod.Edit(cwd, func(f *gomod.File) error {
		importsRetargetFiber(cwd, f, target)
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to migrate go.mod: %w", err)
	}

	if err := importsNoteFiberReferences(cwd, target.Major()); err != nil {
		return err
	}

	if !changed && !modChanged {
		return nil
	}

	cmd.Println("Migrating Go packages")

	return nil
}

// importsRewriteFiberImports moves the import paths of the core fiber module in one Go
// file to the given major version. Only import declarations are changed, not
// strings, struct tags or comments that happen to contain the path.
func importsRewriteFiberImports(content string, fi internal.FileInfo, major uint64) string {
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
		m := importsReFiberImport.FindStringSubmatch(path)
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

	importsNoteGenerate(out, fi, major)

	return out
}

// importsNoteGenerate reports //go:generate lines that name the core fiber
// module at another major than the target. The directive is a comment as far
// as the compiler is concerned, so it cannot be rewritten like an import.
func importsNoteGenerate(content string, fi internal.FileInfo, major uint64) {
	if !strings.Contains(content, "//go:generate") {
		return
	}

	for i, line := range strings.Split(content, "\n") {
		if !strings.HasPrefix(line, "//go:generate") {
			continue
		}
		if path := importsStaleFiberMention(line, major); path != "" {
			internal.Notef(fi, i+1, "`//go:generate` runs %s, which is not the migrated Fiber module; update the directive by hand.", path)
		}
	}
}

// importsStaleFiberMention returns the first core fiber module path in line whose major
// version is not the given one.
func importsStaleFiberMention(line string, major uint64) string {
	want := strconv.FormatUint(major, 10)
	for from := 0; ; {
		i := strings.Index(line[from:], importsFiberModuleStem)
		if i < 0 {
			return ""
		}
		start := from + i
		digits := start + len(importsFiberModuleStem)
		end := digits
		for end < len(line) && line[end] >= '0' && line[end] <= '9' {
			end++
		}
		if end > digits && line[digits:end] != want {
			return line[start:end]
		}
		from = end
	}
}

// importsRetargetFiber points the requirements of the core fiber module in one
// go.mod at the target release.
func importsRetargetFiber(cwd string, f *gomod.File, target *semver.Version) {
	path := fmt.Sprintf("github.com/gofiber/fiber/v%d", target.Major())
	vers := "v" + target.String()

	for _, r := range f.Requirements(importsReFiberModulePath.MatchString) {
		f.SetRequire(r, path, vers)
	}
	f.DedupeRequires(importsReFiberModulePath.MatchString)

	for _, rep := range f.Replacements(importsReFiberModulePath.MatchString) {
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

// importsNoteFiberReferences reports files other than Go source that still name the
// core fiber module at another major version than the target: build scripts,
// Dockerfiles, CI workflows. They are not rewritten, a script may pin the old
// version on purpose.
func importsNoteFiberReferences(cwd string, major uint64) error {
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
		if !importsMentionsModules(d.Name()) {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > importsMaxReferenceSize {
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
			if mention := importsStaleFiberMention(line, major); mention != "" {
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

// importsMaxReferenceSize bounds the files importsNoteFiberReferences reads.
const importsMaxReferenceSize = 1 << 20

// importsMentionsModules reports whether a file of this name is a build script, a
// container file or a CI definition, the places where a module path is spelled
// out for the go tool.
func importsMentionsModules(name string) bool {
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
