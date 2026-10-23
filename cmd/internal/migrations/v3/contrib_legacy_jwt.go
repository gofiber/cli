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

// legacyJWTModules returns the paths of the standalone jwt module that a go.mod requires or
// replaces.
func legacyJWTModules(goMod string) []string {
	file, err := modfile.ParseLax("go.mod", []byte(goMod), nil)
	if err != nil {
		return nil
	}

	var paths []string
	add := func(path string) {
		if legacyJWTPath.MatchString(path) && !slices.Contains(paths, path) {
			paths = append(paths, path)
		}
	}
	for _, req := range file.Require {
		add(req.Mod.Path)
	}
	for _, rep := range file.Replace {
		add(rep.Old.Path)
	}
	return paths
}
