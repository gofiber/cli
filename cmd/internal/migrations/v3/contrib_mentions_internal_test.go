package v3

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The migrations ask mentionsPath before they parse a file: a path that continues
// (jwtx, jwt_x, jwt2) is another package, the paths below it are not.
func Test_mentionsPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		text  string
		paths []string
		want  bool
	}{
		{"import of the v2 era", `import "github.com/gofiber/contrib/jwt"`, contribJWTPaths, true},
		{"import of v3", `import "github.com/gofiber/contrib/v3/jwt"`, contribJWTPaths, true},
		{"standalone module", `import "github.com/gofiber/jwt/v3"`, contribJWTPaths, true},
		{"a package below", `import "github.com/gofiber/contrib/jwt/internal/x"`, contribJWTPaths, true},
		{"end of the text", "github.com/gofiber/contrib/jwt", contribJWTPaths, true},
		{"a longer name", `import "github.com/gofiber/contrib/jwtx"`, contribJWTPaths, false},
		{"an underscore continues the name", "github.com/gofiber/contrib/jwt_x", contribJWTPaths, false},
		{"a digit continues the name", "github.com/gofiber/contrib/jwt2", contribJWTPaths, false},
		{"a longer name, then the package", "github.com/gofiber/contrib/jwtx github.com/gofiber/contrib/jwt\n", contribJWTPaths, true},
		{"another package", `import "github.com/gofiber/contrib/paseto"`, contribJWTPaths, false},
		{"the standalone websocket", `import "github.com/gofiber/websocket/v2"`, contribWebsocketPaths, true},
		{"the websocket of contrib", `import "github.com/gofiber/contrib/v3/websocket"`, contribWebsocketPaths, true},
		{"i18n under its second name", `import "github.com/gofiber/contrib/v3/i18n"`, contribI18nPaths, true},
		{"zerolog under its second name", `import "github.com/gofiber/contrib/zerolog"`, contribZerologPaths, true},
		{"zerolog under its first name", `import "github.com/gofiber/contrib/fiberzerolog"`, contribZerologPaths, true},
		{"paseto", `import "github.com/gofiber/contrib/v3/paseto"`, contribPasetoPaths, true},
		{"no paths", "github.com/gofiber/contrib/jwt", nil, false},
		{"no text", "", contribJWTPaths, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, mentionsPath(tt.text, tt.paths...))
		})
	}
}

func Test_contribMentioned(t *testing.T) {
	t.Parallel()

	for _, text := range []string{
		"require github.com/gofiber/contrib/jwt v1.0.0",
		"replace github.com/gofiber/contrib => ../contrib",
		`import "github.com/gofiber/swagger"`,
		`import "github.com/gofiber/jwt"`,
		`import "github.com/gofiber/websocket/v2"`,
	} {
		assert.True(t, contribMentioned(text), text)
	}

	for _, text := range []string{
		`import "github.com/gofiber/fiber/v2"`,
		`import "github.com/gofiber/storage/redis"`,
		"",
	} {
		assert.False(t, contribMentioned(text), text)
	}
}

func Test_contribPathsOf(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{
		"github.com/gofiber/contrib/a", "github.com/gofiber/contrib/v3/a",
		"github.com/gofiber/contrib/b", "github.com/gofiber/contrib/v3/b",
	}, contribPathsOf("a", "b"))
	assert.Empty(t, contribPathsOf())

	// the standalone modules that were folded into contrib are there next to their new paths
	assert.Equal(t, []string{
		"github.com/gofiber/contrib/jwt", "github.com/gofiber/contrib/v3/jwt", "github.com/gofiber/jwt",
	}, contribJWTPaths)
	assert.Equal(t, []string{
		"github.com/gofiber/contrib/websocket", "github.com/gofiber/contrib/v3/websocket", "github.com/gofiber/websocket",
	}, contribWebsocketPaths)
}
