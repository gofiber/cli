package migrations_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations"
)

func Test_MigrateReplaceDirectives(t *testing.T) {
	t.Parallel()

	gomod := `module example

go 1.26.0

require (
	github.com/gofiber/fiber/v3 v3.6.1
	github.com/gofiber/contrib/v3/jwt v1.2.6
	github.com/gofiber/storage/redis/v3 v3.4.1 // indirect
	github.com/gofiber/template/html/v3 v3.0.0
)

replace (
	github.com/gofiber/contrib/websocket => ../websocket
	github.com/gofiber/contrib/v3/jwt v1.2.6 => ../jwt
	github.com/gofiber/storage/redis/v3 => github.com/me/redis/v3 v3.9.9
	github.com/gofiber/template/html/v2 v2.1.3 => ../html
	github.com/gofiber/fiber/v2 => ../fiber-fork
	github.com/other/module => ../other
)

replace github.com/gofiber/utils => ../utils
`
	dir := writeTree(t, map[string]string{"go.mod": gomod})

	var buf bytes.Buffer
	require.NoError(t, migrations.MigrateReplaceDirectives(newCmd(&buf), dir, nil, nil))

	assert.Empty(t, buf.String(), "it only reports")
	assert.Equal(t, gomod, readFile(t, dir+"/go.mod"), "and leaves go.mod alone")
	assert.Equal(t, []string{
		"go.mod:13: the replace directive for github.com/gofiber/contrib/websocket applies to no requirement of this go.mod, the module may have moved to another path in Fiber v3; add a replace directive for the new path if you still want to use ../websocket.",
		"go.mod:16: the replace directive for github.com/gofiber/template/html/v2 applies to no requirement of this go.mod, the module may have moved to another path in Fiber v3; add a replace directive for the new path if you still want to use ../html.",
		"go.mod:21: the replace directive for github.com/gofiber/utils applies to no requirement of this go.mod, the module may have moved to another path in Fiber v3; add a replace directive for the new path if you still want to use ../utils.",
	}, importsNotes(dir))
}

func Test_MigrateReplaceDirectives_EveryModuleOfARepository(t *testing.T) {
	t.Parallel()

	dir := writeTree(t, map[string]string{
		"go.mod":         "module a\r\n\r\nrequire github.com/gofiber/contrib/v3/jwt v1.2.6\r\n\r\nreplace github.com/gofiber/contrib/v3/jwt => ../jwt\r\n",
		"svc/api/go.mod": "module a/svc/api\n\nrequire github.com/gofiber/contrib/v3/zap v1.1.1\n\nreplace github.com/gofiber/contrib/zap => ../zap\n",
		"svc/bad/go.mod": "module {{ .Name }}\n",
		"vendor/go.mod":  "module vendor\n\nreplace github.com/gofiber/contrib/zap => ../zap\n",
	})

	require.NoError(t, migrations.MigrateReplaceDirectives(newCmd(&bytes.Buffer{}), dir, nil, nil))

	notes := importsNotes(dir)
	require.Len(t, notes, 2)
	assert.Contains(t, notes[0], "svc/api/go.mod:5: the replace directive for github.com/gofiber/contrib/zap ")
	assert.Contains(t, notes[1], "svc/bad/go.mod:", "a go.mod that does not parse is reported, not fatal")
}

func Test_MigrateReplaceDirectives_NothingToReport(t *testing.T) {
	t.Parallel()

	dir := writeTree(t, map[string]string{
		"go.mod": "module a\n\nrequire (\n\tgithub.com/gofiber/fiber/v3 v3.6.1\n\tgithub.com/gofiber/utils/v2 v2.6.2 // indirect\n)\n\nreplace github.com/gofiber/utils/v2 => ../utils\n",
	})

	require.NoError(t, migrations.MigrateReplaceDirectives(newCmd(&bytes.Buffer{}), dir, nil, nil))

	assert.Empty(t, importsNotes(dir))
}
