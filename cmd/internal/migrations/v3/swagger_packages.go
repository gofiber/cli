package v3

import (
	"fmt"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"
)

const (
	contribSwaggerOld = "github.com/gofiber/contrib/swagger"
	contribSwaggerNew = "github.com/gofiber/contrib/v3/swaggerui"
	fiberSwaggerOld   = "github.com/gofiber/swagger"
	fiberSwaggerNew   = "github.com/gofiber/contrib/v3/swaggo"
)

// Names shared with the other migrations that read go.mod files.
const (
	goModVersionPattern = `v[a-zA-Z0-9.+-]+`
	vendorDirName       = "vendor"
	goModFileName       = "go.mod"
)

// MigrateSwaggerPackages moves the two swagger modules (github.com/gofiber/swagger
// and github.com/gofiber/contrib/swagger) to contrib/v3/swaggo and
// contrib/v3/swaggerui. Both v3 packages are not called swagger, so an import
// without a name gets the name swagger, which keeps the code that uses it as it
// is. The rest of the work is shared with MigrateContribPackages.
func MigrateSwaggerPackages(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	changed, err := migrateContrib(cmd, cwd, true)
	if err != nil {
		return fmt.Errorf("failed to migrate swagger packages: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating swagger packages")
	return nil
}
