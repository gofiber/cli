package v3_test

import (
	"maps"
	"testing"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// mw3SessionChain is the session migrations in the order the command runs them.
var mw3SessionChain = []mw3Migration{
	v3.MigrateSessionConfig,
	v3.MigrateSessionExtractor,
	v3.MigrateSessionStore,
	v3.MigrateSessionRelease,
	v3.MigrateSessionMethods,
}

// mw3RunModule is mw3Run for a project that is a Go module: it gets a go.mod,
// unless the files have one, so that the packages of the project can see each
// other.
func mw3RunModule(t *testing.T, files map[string]string, migrations ...mw3Migration) mw3Result {
	t.Helper()

	if _, ok := files["go.mod"]; !ok {
		files = maps.Clone(files)
		files["go.mod"] = "module example.com/app\n\ngo 1.25\n"
	}
	return mw3Run(t, files, migrations...)
}
