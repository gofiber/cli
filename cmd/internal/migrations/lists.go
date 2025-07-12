package migrations

import (
	"fmt"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	v3migrations "github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// MigrationFn is a function that will be called during migration
type MigrationFn func(cmd *cobra.Command, cwd string, curr, target *semver.Version) error

// Migration is a single migration
type Migration struct {
	From      string
	To        string
	Functions []MigrationFn
}

// Migrations is a list of all migrations
// Example structure:
// {"from": ">=2.0.0", "to": "<=3.*.*", "fn": [MigrateFN, MigrateFN]}
var Migrations = []Migration{
	{
		From: ">=2.0.0",
		To:   "<4.0.0-0",
		Functions: []MigrationFn{
			v3migrations.MigrateHandlerSignatures,
			v3migrations.MigrateParserMethods,
			v3migrations.MigrateRedirectMethods,
			v3migrations.MigrateGenericHelpers,
		},
	},
	{From: ">=1.0.0", To: ">=0.0.0-0", Functions: []MigrationFn{MigrateGoPkgs}},
}

// DoMigration runs all migrations
// It will run all migrations that match the current and target version
func DoMigration(cmd *cobra.Command, cwd string, curr, target *semver.Version) error {
	for _, m := range Migrations {
		toC, err := semver.NewConstraint(m.To)
		if err != nil {
			return fmt.Errorf("parse to constraint %s: %w", m.To, err)
		}
		fromC, err := semver.NewConstraint(m.From)
		if err != nil {
			return fmt.Errorf("parse from constraint %s: %w", m.From, err)
		}

		if fromC.Check(curr) && toC.Check(target) {
			for _, fn := range m.Functions {
				if err := fn(cmd, cwd, curr, target); err != nil {
					return err
				}
			}
		} else {
			cmd.Printf("Skipping migration from %s to %s\n", m.From, m.To)
		}
	}

	return nil
}
