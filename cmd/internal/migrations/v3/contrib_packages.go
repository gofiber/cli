package v3

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"
	"golang.org/x/mod/modfile"

	"github.com/gofiber/cli/cmd/internal"
)

const (
	contribPrefix   = "github.com/gofiber/contrib/"
	contribV3Prefix = "github.com/gofiber/contrib/v3/"
)

var (
	// contribMentionRe finds the files and go.mod files worth a closer look.
	contribMentionRe = regexp.MustCompile(`github\.com/gofiber/(?:contrib|swagger|jwt|websocket)`)

	// The standalone modules that were folded into gofiber/contrib.
	contribLegacyJWTRe       = regexp.MustCompile(`^github\.com/gofiber/jwt(?:/v\d+)?$`)
	contribLegacyWebsocketRe = regexp.MustCompile(`^github\.com/gofiber/websocket(?:/v\d+)?$`)
)

// contribSocketioNote explains the import that replaces contrib/socketio.
const contribSocketioNote = "contrib/socketio was a plain WebSocket event bus, contrib/v3/socketio speaks Engine.IO/Socket.IO; " +
	"the import now points at contrib/v3/socketio/legacy, which keeps the plain WebSocket protocol " +
	"(contrib/v3/websocket/event has the same API without the deprecation)"

// contribTarget is where a module of the v2 era goes in v3.
type contribTarget struct {
	// importPath is the import path after the migration.
	importPath string
	// modulePath is the module that provides importPath.
	modulePath string
	// short is the name of the v3 module, which is what its version is looked up by.
	short string
	// name is the identifier the code uses for the package of the old import,
	// "" for a sub-package whose name is not known.
	name string
	// alias is the name a migrated import must carry when it had none: the
	// package of the new module is called something else than name, and the
	// code still says name.Something.
	alias string
	// note is shown to the user next to the import.
	note string
	// swagger tells the swagger migration's modules from the others.
	swagger bool
}

// contribTargetOf returns where the import path of a v2 era contrib module (or a
// module path of go.mod) moves to. ok is false for anything that does not move:
// paths of other projects and paths that already are v3.
func contribTargetOf(path string) (t contribTarget, ok bool) {
	switch {
	case path == fiberSwaggerOld:
		return contribSwaggerTarget(fiberSwaggerNew, "swaggo"), true
	case path == contribSwaggerOld:
		return contribSwaggerTarget(contribSwaggerNew, "swaggerui"), true
	case contribLegacyJWTRe.MatchString(path):
		return contribModuleTarget("jwt", "jwtware"), true
	case contribLegacyWebsocketRe.MatchString(path):
		return contribModuleTarget("websocket", "websocket"), true
	}

	rest, ok := strings.CutPrefix(path, contribPrefix)
	if !ok || rest == "" || hasVersionPrefix(rest) {
		return contribTarget{}, false
	}

	// The major version element of fiberzap/v2 is gone in v3, and so are the old
	// names of the renamed modules.
	short, sub, hasSub := strings.Cut(internal.NormalizeContribModule(rest), "/")
	if short == "swagger" {
		return contribTarget{}, false
	}

	t = contribModuleTarget(short, "")
	if hasSub {
		t.importPath += "/" + sub
		return t, true
	}

	t.name = internal.ContribPackageName(rest)
	if short == "socketio" {
		t.importPath = contribV3Prefix + "socketio/legacy"
		t.note = contribSocketioNote
	}
	t.alias = contribAliasFor(t.name, t.importPath)
	return t, true
}

// contribModuleTarget is the target of a module that moves to
// github.com/gofiber/contrib/v3/<short>.
func contribModuleTarget(short, name string) contribTarget {
	t := contribTarget{
		importPath: contribV3Prefix + short,
		modulePath: contribV3Prefix + short,
		short:      short,
		name:       name,
	}
	t.alias = contribAliasFor(name, t.importPath)
	return t
}

func contribSwaggerTarget(importPath, short string) contribTarget {
	t := contribModuleTarget(short, "swagger")
	t.importPath = importPath
	t.swagger = true
	t.alias = contribAliasFor(t.name, importPath)
	return t
}

// contribAliasFor returns the name an import of path needs when the code calls
// the package name. Without it goimports, which takes the last element of the
// path for the package name when it cannot load the package, would drop the
// import as unused; with the name of the package being renamed (fiberzap ->
// zap) there would be no identifier left at all.
func contribAliasFor(name, importPath string) string {
	if name == "" || name == defaultImportName(importPath) {
		return ""
	}
	return name
}

// hasVersionPrefix reports whether rest, the part of a contrib path after
// github.com/gofiber/contrib/, starts with a major version element: that is the
// v3 layout of the repository.
func hasVersionPrefix(rest string) bool {
	if len(rest) < 2 || rest[0] != 'v' || rest[1] < '0' || rest[1] > '9' {
		return false
	}
	i := 2
	for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
		i++
	}
	if i == len(rest) {
		return true
	}
	return rest[i] == '/'
}

// MigrateContribPackages moves the imports and the go.mod requirements of the
// github.com/gofiber/contrib modules (and of the standalone jwt and websocket
// modules that were folded into it) to their v3 modules.
//
// The identifier an import binds is kept: the v3 module of fiberzap is called
// zap, so an import without a name gets the name fiberzap. The version of every
// module is looked up before anything is written, a module whose version cannot
// be found is left alone completely (imports and go.mod) and reported, and with
// --skip_go_mod nothing is looked up and go.mod is left to `go mod tidy`.
func MigrateContribPackages(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := migrateContrib(cmd, cwd, false)
	if err != nil {
		return fmt.Errorf("failed to migrate contrib packages: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating contrib packages")
	return nil
}

// contribSkipGoMod reports whether the migration runs with --skip_go_mod.
func contribSkipGoMod(cmd *cobra.Command) bool {
	skip, err := cmd.Flags().GetBool("skip_go_mod")
	return err == nil && skip
}

// contribRun is one run of the contrib package migration over a directory.
type contribRun struct {
	// versions maps the short name of a module to its latest version.
	versions map[string]string
	// skipped holds the modules that are left alone because their version could
	// not be looked up.
	skipped map[string]bool
	cwd     string
	swagger bool
}

// migrateContrib moves the imports and go.mod files below cwd; swagger picks the
// two swagger modules or everything else.
func migrateContrib(cmd *cobra.Command, cwd string, swagger bool) (bool, error) {
	run := &contribRun{cwd: cwd, swagger: swagger, versions: map[string]string{}, skipped: map[string]bool{}}

	mods, err := run.scanGoMods()
	if err != nil {
		return false, err
	}
	skipGoMod := contribSkipGoMod(cmd)
	if !skipGoMod {
		run.resolve(mods)
	}

	changed, err := internal.ChangeFileContentInfo(cwd, run.rewriteImports)
	if err != nil {
		return false, fmt.Errorf("rewrite imports: %w", err)
	}
	if skipGoMod {
		return changed, nil
	}

	modChanged, err := run.writeGoMods(mods)
	if err != nil {
		return false, err
	}
	return changed || modChanged, nil
}

// contribGoMod is a go.mod file that requires modules which move.
type contribGoMod struct {
	file    *modfile.File
	path    string
	content string
	mode    fs.FileMode
}

// contribModLine is a require or replace line of a go.mod file that names a
// module which moves.
type contribModLine struct {
	line    *modfile.Line
	path    string
	version string
	target  contribTarget
	replace bool
}

// scanGoMods reads the go.mod files below the root that mention a module which
// moves. A go.mod that cannot be parsed is reported and left alone.
func (r *contribRun) scanGoMods() ([]*contribGoMod, error) {
	var mods []*contribGoMod
	err := filepath.WalkDir(r.cwd, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if d.Name() == vendorDirName {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != goModFileName {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", path, err)
		}
		b, err := os.ReadFile(path) // #nosec G304 -- reading module file
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		if !contribMentionRe.Match(b) {
			return nil
		}

		file, err := modfile.Parse(path, b, nil)
		if err != nil {
			r.noteGoModf(path, 0, "skipped because it does not parse (%v); move its github.com/gofiber/contrib requirements to the v3 modules by hand", err)
			return nil
		}
		mod := &contribGoMod{path: path, content: string(b), file: file, mode: info.Mode().Perm()}
		if len(r.lines(mod)) > 0 {
			mods = append(mods, mod)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to scan go.mod files: %w", err)
	}
	return mods, nil
}

// lines returns the lines of mod that this run migrates, requirements first.
func (r *contribRun) lines(mod *contribGoMod) []contribModLine {
	var lines []contribModLine
	for _, req := range mod.file.Require {
		if t, ok := contribTargetOf(req.Mod.Path); ok && t.swagger == r.swagger {
			lines = append(lines, contribModLine{line: req.Syntax, path: req.Mod.Path, version: req.Mod.Version, target: t})
		}
	}
	for _, rep := range mod.file.Replace {
		if t, ok := contribTargetOf(rep.Old.Path); ok && t.swagger == r.swagger {
			lines = append(lines, contribModLine{line: rep.Syntax, path: rep.Old.Path, version: rep.Old.Version, target: t, replace: true})
		}
	}
	return lines
}

// noteGoMod reports something about a go.mod file.
func (r *contribRun) noteGoModf(path string, line int, format string, args ...any) {
	rel, err := filepath.Rel(r.cwd, path)
	if err != nil {
		rel = path
	}
	internal.AddNote(r.cwd, internal.Note{File: rel, Line: line, Message: fmt.Sprintf(format, args...)})
}

// resolve looks up the latest version of every module the go.mod files require
// before anything is rewritten. The lookups run side by side; the modules that
// fail are skipped as a whole and reported.
func (r *contribRun) resolve(mods []*contribGoMod) {
	type lookup struct {
		err     error
		mod     *contribGoMod
		short   string
		version string
		path    string
		row     int
	}

	var lookups []*lookup
	for _, mod := range mods {
		for _, l := range r.lines(mod) {
			if !slices.ContainsFunc(lookups, func(x *lookup) bool { return x.short == l.target.short }) {
				lookups = append(lookups, &lookup{short: l.target.short, path: l.target.modulePath, mod: mod, row: l.line.Start.Line})
			}
		}
	}

	var wg sync.WaitGroup
	for _, l := range lookups {
		wg.Go(func() {
			l.version, l.err = contribV3Version(l.short)
		})
	}
	wg.Wait()

	for _, l := range lookups {
		if l.err == nil {
			r.versions[l.short] = l.version
			continue
		}
		r.skipped[l.short] = true
		reason := strings.Join(strings.Fields(l.err.Error()), " ")
		reason = strings.TrimSuffix(strings.TrimPrefix(reason, "fetch contrib version: "), ": exit status 1")
		r.noteGoModf(l.mod.path, l.row,
			"could not look up the latest version of %s (%s); its imports and go.mod entries were left on the v2 module, run the migration again once the Go module proxy is reachable",
			l.path, reason)
	}
}

// moves reports whether the target is migrated by this run.
func (r *contribRun) moves(t contribTarget) bool {
	return t.swagger == r.swagger && !r.skipped[t.short]
}

// rewriteImports moves the contrib imports of one Go file.
func (r *contribRun) rewriteImports(content string, fi internal.FileInfo) string {
	if !contribMentionRe.MatchString(content) {
		return content
	}
	g, ok := parseFile(fi, content)
	if !ok {
		return content
	}

	f := newContribFile(g, fi)
	legacyJWT := false
	for _, spec := range g.file.Imports {
		target, ok := contribTargetOf(contribUnquote(spec.Path.Value))
		if !ok || !r.moves(target) {
			continue
		}

		path := `"` + target.importPath + `"`
		if strings.HasPrefix(spec.Path.Value, "`") {
			path = "`" + target.importPath + "`"
		}
		if spec.Name == nil && target.alias != "" {
			path = target.alias + " " + path
		}
		f.edits.replaceNode(g, spec.Path, path)

		if target.note != "" {
			internal.Notef(fi, g.line(spec.Pos()), "%s", target.note)
		}

		// The standalone jwt module has a config of its own, which only the
		// import can tell from the config of contrib/v3/jwt.
		legacyJWT = legacyJWT || contribLegacyJWTRe.MatchString(contribUnquote(spec.Path.Value))
	}
	if legacyJWT {
		f.jwtLegacy()
	}
	return f.apply()
}

// writeGoMods rewrites the go.mod files; every module is already resolved.
func (r *contribRun) writeGoMods(mods []*contribGoMod) (bool, error) {
	changed := false
	for _, mod := range mods {
		updated := r.rewriteGoMod(mod)
		if updated == mod.content {
			continue
		}
		if err := os.WriteFile(mod.path, []byte(updated), mod.mode); err != nil {
			return changed, fmt.Errorf("write %s: %w", mod.path, err)
		}
		changed = true
	}
	return changed, nil
}

// rewriteGoMod returns the content of mod with its requirements and replace
// directives on the v3 modules.
func (r *contribRun) rewriteGoMod(mod *contribGoMod) string {
	required := map[string]bool{}
	for _, req := range mod.file.Require {
		required[req.Mod.Path] = true
	}

	var edits editSet
	for _, l := range r.lines(mod) {
		version, ok := r.versions[l.target.short]
		if !r.moves(l.target) || !ok {
			continue
		}

		start, end := l.line.Start.Byte, l.line.End.Byte
		switch {
		case l.replace:
			edits.replace(start, end, contribRewriteModLine(mod.content[start:end], l, l.target.modulePath, version))
			r.noteGoModf(mod.path, l.line.Start.Line,
				"the replace directive for %s now applies to %s; make sure it points at the v3 version of the module", l.path, l.target.modulePath)
		case required[l.target.modulePath]:
			// The v3 module is required already: the old line has nothing left to say.
			edits.replace(contribLineStart(mod.content, start), contribLineEnd(mod.content, end), "")
		default:
			required[l.target.modulePath] = true
			edits.replace(start, end, contribRewriteModLine(mod.content[start:end], l, l.target.modulePath, version))
		}
	}
	return edits.apply(mod.content)
}

// contribRewriteModLine returns the text of a require or replace line moved to
// the module newPath at version. A replace directive without a version keeps
// not having one.
func contribRewriteModLine(text string, l contribModLine, newPath, version string) string {
	at := strings.Index(text, l.path)
	if at < 0 {
		return text
	}
	head, tail := text[:at], text[at+len(l.path):]
	if l.version != "" {
		if vi := strings.Index(tail, l.version); vi >= 0 {
			tail = tail[:vi] + version + tail[vi+len(l.version):]
		}
	}
	return head + newPath + tail
}
