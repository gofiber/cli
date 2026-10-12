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

func Test_MigrateRuleList_EntriesSharingALine(t *testing.T) {
	t.Parallel()

	content, _ := migrateRuleList(t, `package main
import "github.com/gofiber/fiber/v3/middleware/redirect"
var _ = redirect.New(redirect.Config{
	Rules: map[string]string{"/old": "/new", "/old/*": "/new/$1"},
})`)

	assert.Contains(t, content, `{From: "/old", To: "/new"},`)
	assert.Contains(t, content, `{From: "/old/*", To: "/new/$1"},`)
	assert.NotContains(t, content, "map[string]string")
}

func Test_MigrateRuleList_KeepsCommentsOut(t *testing.T) {
	t.Parallel()

	content, _ := migrateRuleList(t, `package main
import "github.com/gofiber/fiber/v3/middleware/redirect"
var _ = redirect.New(redirect.Config{
	Rules: map[string]string{
		// the old docs path
		"/old": "/new",
	},
})`)

	assert.Contains(t, content, `{From: "/old", To: "/new"},`)
	assert.NotContains(t, content, "map[string]string")
}

func Test_MigrateRuleList_LeavesACommentedOutEntryOut(t *testing.T) {
	t.Parallel()

	content, _ := migrateRuleList(t, `package main
import "github.com/gofiber/fiber/v3/middleware/redirect"
var _ = redirect.New(redirect.Config{
	Rules: map[string]string{
		"/old": "/new", // was "/older": "/new"
		// "/legacy/*": "/v1/$1",
	},
})`)

	assert.Contains(t, content, `{From: "/old", To: "/new"},`)
	assert.NotContains(t, content, `{From: "/older"`)
	assert.NotContains(t, content, `{From: "/legacy/*"`)
	assert.NotContains(t, content, "map[string]string")
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

func Test_MigrateRuleList_RanksAKeyAsItsMiddlewareReadsIt(t *testing.T) {
	t.Parallel()

	// redirect compiles a key as a regexp, so its "." pins nothing and the two
	// keys tie on their prefix; rewrite reads the same "." as path text.
	tests := []struct {
		pkg   string
		first string
		then  string
	}{
		{pkg: "redirect", first: "/docs/v2*/changelog", then: "/docs/v2.1/*"},
		{pkg: "rewrite", first: "/docs/v2.1/*", then: "/docs/v2*/changelog"},
	}

	for _, tc := range tests {
		t.Run(tc.pkg, func(t *testing.T) {
			t.Parallel()

			content, _ := migrateRuleList(t, `package main
import "github.com/gofiber/fiber/v3/middleware/`+tc.pkg+`"
var _ = `+tc.pkg+`.New(`+tc.pkg+`.Config{
	Rules: map[string]string{
		"/docs/v2.1/*":        "/archive/$1",
		"/docs/v2*/changelog": "/changelog",
	},
})`)

			first := strings.Index(content, `{From: "`+tc.first+`"`)
			then := strings.Index(content, `{From: "`+tc.then+`"`)
			require.NotEqual(t, -1, first)
			require.NotEqual(t, -1, then)
			assert.Less(t, first, then)
		})
	}
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
