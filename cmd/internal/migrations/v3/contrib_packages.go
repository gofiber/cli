package v3

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

var (
	contribImportRe = regexp.MustCompile(`(["\x60])github\.com/gofiber/contrib/([^"\x60\s]+)`)
	contribModRe    = regexp.MustCompile(`github\.com/gofiber/contrib/[^\s]+`)
)

const (
	contribPrefix   = "github.com/gofiber/contrib/"
	contribV3Prefix = "github.com/gofiber/contrib/v3/"
)

// MigrateContribPackages updates imports and module requirements that reference
// github.com/gofiber/contrib to use the v3 module path.
func MigrateContribPackages(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changedImports, err := internal.ChangeFileContent(cwd, func(content string) string {
		return contribImportRe.ReplaceAllStringFunc(content, func(match string) string {
			sub := contribImportRe.FindStringSubmatch(match)
			if len(sub) != 3 {
				return match
			}
			rest := sub[2]
			if hasVersionPrefix(rest) {
				return match
			}
			return sub[1] + contribV3Prefix + rest
		})
	})
	if err != nil {
		return fmt.Errorf("failed to migrate contrib imports: %w", err)
	}

	modChanged := false
	walkErr := filepath.WalkDir(cwd, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if d.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "go.mod" {
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
		content := string(b)
		matches := contribModRe.FindAllString(content, -1)
		if len(matches) == 0 {
			return nil
		}

		updated := content
		versions := make(map[string]string)

		for _, match := range matches {
			rest := strings.TrimPrefix(match, contribPrefix)
			if rest == match || hasVersionPrefix(rest) {
				continue
			}

			version, ok := versions[rest]
			if !ok {
				version, err = contribV3Version(rest)
				if err != nil {
					return fmt.Errorf("fetch contrib %s version: %w", rest, err)
				}
				versions[rest] = version
			}

			updated = updateGoModModule(updated, match, contribV3Prefix+rest, version)
		}

		if updated == content {
			return nil
		}
		if err := os.WriteFile(path, []byte(updated), info.Mode().Perm()); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		modChanged = true
		return nil
	})
	if walkErr != nil {
		return fmt.Errorf("failed to migrate contrib modules: %w", walkErr)
	}

	if !changedImports && !modChanged {
		return nil
	}

	cmd.Println("Migrating contrib packages")
	return nil
}

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
