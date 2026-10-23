package v3

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"
)

// legacyJWTPath matches the standalone jwt module of the v2 era, github.com/gofiber/jwt/v3
// (the v3 is the major version of that module), which was folded into contrib.
var legacyJWTPath = regexp.MustCompile(`^github\.com/gofiber/jwt(?:/v\d+)?$`)

// legacyJWTName is the name of the package of the standalone module, which the code of a
// file that imported it without a name still uses.
const legacyJWTName = "jwtware"

// migrateLegacyJWTImport moves the import of the standalone jwt module to the jwt package
// of contrib for v3. An import without a name gets the one the package had, so that the
// code keeps compiling whatever name the new package is known by and goimports does not
// take the import for an unused one.
func migrateLegacyJWTImport(content string) string {
	if !strings.Contains(content, "gofiber/jwt") {
		return content
	}
	src, ok := parseForEdits(content)
	if !ok {
		return content
	}

	for _, spec := range src.file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || !legacyJWTPath.MatchString(path) {
			continue
		}

		target := strconv.Quote(contribV3Prefix + "jwt")
		if spec.Name == nil {
			target = legacyJWTName + " " + target
		}
		src.replace(spec.Path, target)
	}

	return src.apply()
}

// legacyJWTModules returns the paths of the standalone jwt module that a go.mod requires.
func legacyJWTModules(goMod string) []string {
	file, err := modfile.ParseLax("go.mod", []byte(goMod), nil)
	if err != nil {
		return nil
	}

	var paths []string
	for _, req := range file.Require {
		if legacyJWTPath.MatchString(req.Mod.Path) && !slices.Contains(paths, req.Mod.Path) {
			paths = append(paths, req.Mod.Path)
		}
	}
	return paths
}

// updateLegacyJWTRequirement moves the requirement of the standalone jwt module to the jwt
// module of contrib. The requirement is the only directive that moves: a replace of the old
// module points at a copy or a fork of the old module, which cannot stand in for the new
// one, and it applies to nothing once the requirement is gone, and an exclude names a
// version of the old module. The requirement is found by the parser, because a line of a
// block reads the same in a require, a replace and an exclude block.
func updateLegacyJWTRequirement(goMod, oldPath, version string) string {
	file, err := modfile.ParseLax("go.mod", []byte(goMod), nil)
	if err != nil {
		return goMod
	}

	// from the last requirement to the first, so that the offsets of the others hold
	reqs := slices.SortedFunc(slices.Values(file.Require), func(a, b *modfile.Require) int {
		return b.Syntax.Start.Byte - a.Syntax.Start.Byte
	})
	for _, req := range reqs {
		if req.Mod.Path != oldPath {
			continue
		}
		verb := ""
		if !req.Syntax.InBlock {
			verb = "require "
		}
		goMod = goMod[:req.Syntax.Start.Byte] + verb + contribV3Prefix + "jwt " + version + goMod[req.Syntax.End.Byte:]
	}
	return goMod
}
