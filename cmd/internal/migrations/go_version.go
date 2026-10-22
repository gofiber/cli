package migrations

import (
	"fmt"
	"go/version"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"
	"golang.org/x/mod/modfile"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/gomod"
)

// importsFiberGoVersions lists the go directive that Fiber v3 releases declare in
// their go.mod, as the first release of each value. A release needs the Go
// version of the newest row it is at or after; a release newer than the table
// is assumed to need the last one (MigrateDependencies reads the real value
// from the release once it is downloaded).
var importsFiberGoVersions = []struct{ since, goVersion string }{
	{since: "3.0.0-0", goVersion: "1.21"},
	{since: "3.0.0-beta.4", goVersion: "1.23"},
	{since: "3.0.0-beta.5", goVersion: "1.24.0"},
	{since: "3.0.0-rc.1", goVersion: "1.25.0"},
	{since: "3.6.0", goVersion: "1.26.0"},
}

// importsWorkVersionMessage is printed when the go line of a workspace was raised.
const importsWorkVersionMessage = "Ensuring the go version of go.work is not behind its modules"

// importsReFiberCore matches the module path of the core fiber module at any major.
var importsReFiberCore = regexp.MustCompile(`^github\.com/gofiber/fiber(/v\d+)?$`)

// importsMinimumGoVersion returns the Go version that the Fiber release target needs,
// "" when the target is not a Fiber v3 release.
func importsMinimumGoVersion(target *semver.Version) string {
	minimum := ""
	for _, row := range importsFiberGoVersions {
		since, err := semver.NewVersion(row.since)
		if err != nil || target.Compare(since) < 0 {
			break
		}
		minimum = row.goVersion
	}
	return minimum
}

// importsGoJumpMinimum is the number of Go releases a raised go directive has to
// skip for the migration to tell the user what the new directive switches on.
const importsGoJumpMinimum = 3

// importsGoNoGoLine is the language version of a module whose go.mod has no go
// directive, which the go command takes to be go 1.16.
const importsGoNoGoLine = "1.16"

// importsGoSemantics lists what a module gets by declaring a newer go directive
// and cannot see in the code: the first minor version of Go 1 whose directive
// switches it on, and what it does.
var importsGoSemantics = []struct {
	text  string
	minor int
}{
	{minor: 22, text: "each iteration of a for loop has variables of its own, so a closure or goroutine that captured a loop variable sees another value than before"},
	{minor: 23, text: "timer and ticker channels are unbuffered (GODEBUG asynctimerchan)"},
	{minor: 24, text: "go vet reports a call of a printf-like function with a non-constant format string and no other argument, and go test runs that check, so a package that passed go test can fail it"},
	{minor: 25, text: "GOMAXPROCS follows the CPU limit of the container (GODEBUG containermaxprocs and updatemaxprocs)"},
}

// importsGoMinor returns n for a go directive of Go 1.n.
func importsGoMinor(directive string) (int, bool) {
	minor, ok := strings.CutPrefix(version.Lang("go"+directive), "go1.")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(minor)
	return n, err == nil
}

// importsGoJumpNote tells what the go directive switches on when it moves from one
// version to another that is several releases further. It returns "" when the
// directive moves by a few releases only, and for a version it cannot read.
func importsGoJumpNote(from, to string) string {
	fromMinor, okFrom := importsGoMinor(from)
	toMinor, okTo := importsGoMinor(to)
	if !okFrom || !okTo || toMinor-fromMinor < importsGoJumpMinimum {
		return ""
	}

	var crossed []string
	for _, row := range importsGoSemantics {
		if fromMinor < row.minor && row.minor <= toMinor {
			crossed = append(crossed, "since go 1."+strconv.Itoa(row.minor)+" "+row.text)
		}
	}
	if len(crossed) == 0 {
		return ""
	}
	return "the go directive moved from " + from + " to " + to + ", which Fiber v3 needs. A newer directive also changes how the whole module is compiled and run: " +
		strings.Join(crossed, "; ") + ". The GODEBUG defaults follow the directive as well (see go.dev/doc/godebug)"
}

// MigrateGoVersion raises the go directive of every module that requires Fiber
// to the Go version the target release needs. A newer directive is kept. The go
// line of a workspace that uses such a module follows, the go command refuses a
// workspace that is behind one of its modules. A directive that moves by several
// releases gets a note, because it changes the semantics of the code.
func MigrateGoVersion(cmd *cobra.Command, cwd string, _, target *semver.Version) error {
	minimum := importsMinimumGoVersion(target)
	if minimum == "" {
		return nil
	}

	changed, err := gomod.Edit(cwd, func(f *gomod.File) error {
		if !importsRequiresFiber(f.File) {
			return nil
		}
		from, line := importsGoNoGoLine, 0
		if f.Go != nil {
			from, line = f.Go.Version, f.Go.Syntax.Start.Line
		}
		if f.RaiseGo(minimum) {
			if msg := importsGoJumpNote(from, minimum); msg != "" {
				internal.AddNote(cwd, internal.Note{File: f.Rel, Line: line, Message: msg})
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("raise go version: %w", err)
	}
	workChanged, err := gomod.SyncWork(cwd)
	if err != nil {
		return fmt.Errorf("raise go version of the workspace: %w", err)
	}
	if changed {
		cmd.Printf("Ensuring go version >= %s\n", minimum)
	}
	if workChanged {
		cmd.Println(importsWorkVersionMessage)
	}
	return nil
}

// importsRequiresFiber reports whether the module requires the core fiber module.
func importsRequiresFiber(f *modfile.File) bool {
	for _, r := range f.Require {
		if importsReFiberCore.MatchString(r.Mod.Path) {
			return true
		}
	}
	return false
}

// fiberModuleDirs returns directories under root containing a go.mod file that
// requires github.com/gofiber/fiber. vendor directories are skipped, and so is
// a go.mod that does not parse (the migration that edits it reports it).
func fiberModuleDirs(root string) ([]string, error) {
	paths, err := gomod.Paths(root)
	if err != nil {
		return nil, fmt.Errorf("find go.mod files: %w", err)
	}

	var dirs []string
	for _, path := range paths {
		b, err := os.ReadFile(path) // #nosec G304 -- reading module file
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		mf, err := modfile.ParseLax(path, b, nil)
		if err != nil {
			continue
		}
		if importsRequiresFiber(mf) {
			dirs = append(dirs, filepath.Dir(path))
		}
	}
	return dirs, nil
}
