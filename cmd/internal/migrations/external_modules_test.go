package migrations_test

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations"
)

const (
	externalSharedV2    = "module example.com/shared\n\ngo 1.25\n\nrequire github.com/gofiber/fiber/v2 v2.52.15\n"
	externalSharedPlain = "module example.com/shared\n\ngo 1.25\n"
)

func Test_MigrateExternalModules_ReplaceOfAModuleOutsideTheDirectory(t *testing.T) {
	t.Parallel()

	dir := importsWriteTree(t, map[string]string{
		"app/go.mod": "module example.com/app\n\ngo 1.25\n\nrequire (\n\tgithub.com/gofiber/fiber/v2 v2.52.15\n\texample.com/shared v0.0.0\n\texample.com/plain v0.0.0\n)\n\n" +
			"replace example.com/shared => ../shared\n\nreplace example.com/plain => ../plain\n\nreplace example.com/gone => ../gone\n\n" +
			"replace example.com/versioned => example.org/other v1.2.3\n",
		"shared/go.mod": externalSharedV2,
		"plain/go.mod":  "module example.com/plain\n\ngo 1.25\n",
	})
	app := filepath.Join(dir, "app")

	var buf bytes.Buffer
	require.NoError(t, migrations.MigrateExternalModules(newCmd(&buf), app, nil, nil))

	assert.Empty(t, buf.String(), "it only reports")
	notes := importsNotes(app)
	require.Len(t, notes, 1, "%v", notes)
	assert.Contains(t, notes[0], "go.mod:11: the replace directive for example.com/shared uses "+filepath.Join(dir, "shared")+", a module outside of the directory this command runs in")
	assert.Contains(t, notes[0], "requires github.com/gofiber/fiber/v2 v2.52.15")
	assert.Contains(t, notes[0], "run fiber migrate in it as well")
}

func Test_MigrateExternalModules_ModulesBelowTheDirectoryAreMigratedWithIt(t *testing.T) {
	t.Parallel()

	dir := importsWriteTree(t, map[string]string{
		"go.mod":        "module example.com/app\n\ngo 1.25\n\nreplace example.com/shared => ./shared\n",
		"shared/go.mod": externalSharedV2,
	})

	require.NoError(t, migrations.MigrateExternalModules(newCmd(&bytes.Buffer{}), dir, nil, nil))

	assert.Empty(t, importsNotes(dir))
}

func Test_MigrateExternalModules_UseOfAWorkspace(t *testing.T) {
	t.Parallel()

	dir := importsWriteTree(t, map[string]string{
		"app/go.mod":    "module example.com/app\n\ngo 1.25\n",
		"app/go.work":   "go 1.25\n\nuse (\n\t.\n\t../shared\n\t../plain\n)\n",
		"shared/go.mod": externalSharedV2,
		"plain/go.mod":  externalSharedPlain,
	})
	app := filepath.Join(dir, "app")

	require.NoError(t, migrations.MigrateExternalModules(newCmd(&bytes.Buffer{}), app, nil, nil))

	notes := importsNotes(app)
	require.Len(t, notes, 1, "%v", notes)
	assert.Contains(t, notes[0], "go.work:5: the workspace uses "+filepath.Join(dir, "shared"))
}

func Test_MigrateExternalModules_AnAbsolutePath(t *testing.T) {
	t.Parallel()

	shared := importsWriteTree(t, map[string]string{"go.mod": externalSharedV2})
	app := importsWriteTree(t, map[string]string{
		"go.mod": "module example.com/app\n\ngo 1.25\n\nreplace example.com/shared => " + filepath.ToSlash(shared) + "\n",
	})

	require.NoError(t, migrations.MigrateExternalModules(newCmd(&bytes.Buffer{}), app, nil, nil))

	notes := importsNotes(app)
	require.Len(t, notes, 1, "%v", notes)
	assert.Contains(t, notes[0], "go.mod:5: the replace directive for example.com/shared uses "+shared)
}
