package v3_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func migrateRuleList(t *testing.T, src string) (string, string) {
	t.Helper()

	dir, err := os.MkdirTemp("", "mrulelist")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, src)
	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateRuleList(cmd, dir, nil, nil))

	return readFile(t, file), buf.String()
}

func Test_MigrateRuleList_Redirect(t *testing.T) {
	t.Parallel()

	content, out := migrateRuleList(t, `package main
import "github.com/gofiber/fiber/v3/middleware/redirect"
var _ = redirect.New(redirect.Config{
	Rules: map[string]string{
		"/old":   "/new",
		"/old/*": "/new/$1",
	},
	StatusCode: 301,
})`)

	assert.Contains(t, content, "RuleList: []redirect.Rule{")
	assert.Contains(t, content, `{From: "/old", To: "/new"},`)
	assert.Contains(t, content, `{From: "/old/*", To: "/new/$1"},`)
	assert.NotContains(t, content, "map[string]string")
	assert.Contains(t, content, "StatusCode: 301")
	assert.Contains(t, out, "Migrating redirect and rewrite Rules maps to RuleList")
}

func Test_MigrateRuleList_Rewrite(t *testing.T) {
	t.Parallel()

	content, _ := migrateRuleList(t, `package main
import "github.com/gofiber/fiber/v3/middleware/rewrite"
var _ = rewrite.New(rewrite.Config{
	Rules: map[string]string{"/js/*": "/public/javascript/$1"},
})`)

	assert.Contains(t, content, "RuleList: []rewrite.Rule{")
	assert.Contains(t, content, `{From: "/js/*", To: "/public/javascript/$1"},`)
}

func Test_MigrateRuleList_KeepsTheOrderTheMapAnswered(t *testing.T) {
	t.Parallel()

	// A list is tried top to bottom, so emitting these as written would put the
	// catch-all first and change which rule answers "/api/users".
	content, _ := migrateRuleList(t, `package main
import "github.com/gofiber/fiber/v3/middleware/redirect"
var _ = redirect.New(redirect.Config{
	Rules: map[string]string{
		"/api/*":     "/v2/$1",
		"/api/users": "/v2/users",
	},
})`)

	users := strings.Index(content, `{From: "/api/users"`)
	catchAll := strings.Index(content, `{From: "/api/*"`)
	require.NotEqual(t, -1, users)
	require.NotEqual(t, -1, catchAll)
	assert.Less(t, users, catchAll, "the specific rule must be tried first")
}

func Test_MigrateRuleList_ImportAlias(t *testing.T) {
	t.Parallel()

	content, _ := migrateRuleList(t, `package main
import rd "github.com/gofiber/fiber/v3/middleware/redirect"
var _ = rd.New(rd.Config{
	Rules: map[string]string{"/old": "/new"},
})`)

	assert.Contains(t, content, "RuleList: []rd.Rule{")
}

func Test_MigrateRuleList_LeavesWhatItCannotRead(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
	}{
		{
			name: "a variable for the map",
			src: `package main
import "github.com/gofiber/fiber/v3/middleware/redirect"
var rules = map[string]string{"/old": "/new"}
var _ = redirect.New(redirect.Config{Rules: rules})`,
		},
		{
			name: "a computed value",
			src: `package main
import "github.com/gofiber/fiber/v3/middleware/redirect"
var target = "/new"
var _ = redirect.New(redirect.Config{
	Rules: map[string]string{"/old": target},
})`,
		},
		{
			name: "another package's Config",
			src: `package main
import "github.com/gofiber/fiber/v3/middleware/cache"
var _ = cache.New(cache.Config{
	Rules: map[string]string{"/old": "/new"},
})`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			content, out := migrateRuleList(t, tc.src)
			assert.Contains(t, content, "map[string]string")
			assert.NotContains(t, content, "RuleList")
			assert.Empty(t, out)
		})
	}
}

func Test_MigrateRuleList_IsIdempotent(t *testing.T) {
	t.Parallel()

	src := `package main
import "github.com/gofiber/fiber/v3/middleware/redirect"
var _ = redirect.New(redirect.Config{
	Rules: map[string]string{"/old": "/new"},
})`

	once, _ := migrateRuleList(t, src)
	twice, out := migrateRuleList(t, once)

	assert.Equal(t, once, twice)
	assert.Empty(t, out, "a second run must report no change")
}
