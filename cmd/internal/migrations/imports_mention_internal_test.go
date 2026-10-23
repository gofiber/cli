package migrations

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// importsStaleFiberMention names the first core fiber module path of a line whose major version
// is not the target: the digits after the "v" are the version, all of them.
func Test_importsStaleFiberMention(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line string
		want string
	}{
		{"another major", "//go:generate go run github.com/gofiber/fiber/v2/internal/gen -o out.go", "github.com/gofiber/fiber/v2"},
		{"the target major", "//go:generate go run github.com/gofiber/fiber/v3/internal/gen", ""},
		{"two digits", "go get github.com/gofiber/fiber/v12@latest", "github.com/gofiber/fiber/v12"},
		{"a major that starts like the target", "github.com/gofiber/fiber/v30", "github.com/gofiber/fiber/v30"},
		{"the second mention is stale", "github.com/gofiber/fiber/v3 github.com/gofiber/fiber/v2", "github.com/gofiber/fiber/v2"},
		{"the first stale mention is returned", "github.com/gofiber/fiber/v1 github.com/gofiber/fiber/v2", "github.com/gofiber/fiber/v1"},
		{"no version", "github.com/gofiber/fiber/vX github.com/gofiber/fiber/v", ""},
		{"no version, then a stale one", "github.com/gofiber/fiber/v github.com/gofiber/fiber/v2", "github.com/gofiber/fiber/v2"},
		{"the end of the line", "github.com/gofiber/fiber/v2", "github.com/gofiber/fiber/v2"},
		{"another module", "github.com/gofiber/utils/v2", ""},
		{"no mention", "go test ./...", ""},
		{"no line", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, importsStaleFiberMention(tt.line, 3))
		})
	}
}
