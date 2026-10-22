package v3

import (
	"regexp"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"

	"github.com/gofiber/cli/cmd/internal/gomod"
)

// importsDestination says where one module line of a family goes.
type importsDestination struct {
	// path is the module path the imports and the requirement end up at, empty
	// for a line that is left alone.
	path string
	// minimum is the lowest version of that line that works with Fiber v3,
	// empty when it is not known.
	minimum string
	// known is false for a module that the tables of the family do not list.
	known bool
}

// importsFamily is a set of modules published from one repository of
// github.com/gofiber with module paths of the form <prefix><name>[/vN]: the
// storage drivers and the template engines. Their major version moved with
// Fiber v3, and so did the lowest release that works with it.
type importsFamily struct {
	// re matches the import path of a package of the family: the name, the major
	// version path element and the rest of the path.
	re *regexp.Regexp
	// module matches the module path of the family.
	module *regexp.Regexp
	// resolve says where the line name/major goes. major is "" for the module
	// path without a major version suffix.
	resolve func(name, major string) importsDestination
	prefix  string
}

func newImportsFamily(prefix string, resolve func(name, major string) importsDestination) importsFamily {
	const name = `([a-zA-Z0-9_-]+)(?:/(v[0-9]+))?`
	return importsFamily{
		prefix:  prefix,
		re:      regexp.MustCompile(`^` + regexp.QuoteMeta(prefix) + name + `(/.*)?$`),
		module:  regexp.MustCompile(`^` + regexp.QuoteMeta(prefix) + name + `$`),
		resolve: resolve,
	}
}

// path returns the module path of the line name/major.
func (fam importsFamily) path(name, major string) string {
	return importsModulePath(fam.prefix, name, major)
}

// importsModulePath returns the module path <prefix><name>[/major].
func importsModulePath(prefix, name, major string) string {
	if major == "" {
		return prefix + name
	}
	return prefix + name + "/" + major
}

// matches reports whether the import path is in the family.
func (fam importsFamily) matches(importPath string) bool {
	return fam.re.MatchString(importPath)
}

// importsMove is an import that was pointed at another module line.
type importsMove struct {
	spec importsSpec
	// name is the module within the family, from the major version it was on
	// ("" for none) to the module path it is on now.
	name string
	from string
	to   string
}

// moveImports schedules the rewrite of the imports of the family in g to the
// lines they move to, and returns the imports it moved. Imports of a module
// that the family does not list, or of a line that is newer than the one the
// family moves to, are left alone.
func (fam importsFamily) moveImports(g *goSource, edits *editSet) []importsMove {
	var moves []importsMove
	for _, spec := range importsOf(g, fam.matches) {
		m := fam.re.FindStringSubmatch(spec.path)
		name, major, rest := m[1], m[2], m[3]

		dest := fam.resolve(name, major)
		if dest.path == "" || dest.path == fam.path(name, major) {
			continue
		}
		importsRewritePath(g, edits, spec, dest.path+rest)
		moves = append(moves, importsMove{spec: spec, name: name, from: major, to: dest.path})
	}
	return moves
}

// retarget moves the requirements and replace directives of the family in one
// go.mod to the lines they move to, and raises a requirement that is below the
// lowest version that works with Fiber v3. A version is never lowered.
// unknown is called for every requirement of a module that the family does not
// list; it may be nil.
func (fam importsFamily) retarget(f *gomod.File, unknown func(r *modfile.Require, name, major string)) {
	for _, r := range f.Requirements(fam.module.MatchString) {
		m := fam.module.FindStringSubmatch(r.Mod.Path)
		name, major := m[1], m[2]

		dest := fam.resolve(name, major)
		switch {
		case !dest.known:
			if unknown != nil {
				unknown(r, name, major)
			}
		case dest.path == "":
		case dest.path != r.Mod.Path && dest.minimum != "":
			f.SetRequire(r, dest.path, dest.minimum)
		case dest.path != r.Mod.Path:
			f.SetRequire(r, dest.path, r.Mod.Version)
		case dest.minimum != "" && semver.Compare(r.Mod.Version, dest.minimum) < 0:
			f.SetRequire(r, r.Mod.Path, dest.minimum)
		}
	}

	f.DedupeRequires(fam.module.MatchString)

	for _, rep := range f.Replacements(fam.module.MatchString) {
		m := fam.module.FindStringSubmatch(rep.Old.Path)
		dest := fam.resolve(m[1], m[2])
		if !dest.known || dest.path == "" || dest.path == rep.Old.Path {
			continue
		}

		// A replace for one version follows the requirement to the version it ended
		// up at.
		vers := ""
		if rep.Old.Version != "" {
			vers = dest.minimum
			if r := f.Requires(dest.path); r != nil {
				vers = r.Mod.Version
			}
		}
		f.SetReplaceOld(rep, dest.path, vers)
	}
}

// importsMajorRank orders the major version path elements of a module: no
// suffix is the v0 and v1 era, which counts as 1.
func importsMajorRank(major string) int {
	if major == "" {
		return 1
	}

	rank := 0
	for _, c := range strings.TrimPrefix(major, "v") {
		rank = rank*10 + int(c-'0')
	}
	return rank
}
