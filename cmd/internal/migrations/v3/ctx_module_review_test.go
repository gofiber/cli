package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_Review_ContextsOfOtherPackages(t *testing.T) {
	t.Parallel()

	notes := reviewNotes(t, map[string]string{
		"go.mod": "module example.com/app\n\ngo 1.25\n",
		"internal/web/web.go": `package web

import "github.com/gofiber/fiber/v3"

type Context struct{ fiber.Ctx }

type Ctx = fiber.Ctx

type Plain struct{}

func (Plain) QueryInt(string) int { return 0 }
`,
		"internal/handlers/handlers.go": `package handlers

import "example.com/app/internal/web"

func page(c *web.Context, a web.Ctx, p web.Plain) int {
	return c.QueryInt("a") + a.QueryInt("b") + p.QueryInt("c")
}
`,
		"internal/other/other.go": `package other

type reader struct{}

func (reader) QueryInt(string) int { return 0 }

func read(r reader) int { return r.QueryInt("a") }
`,
	})

	// the wrapper is told how to call the replacement, the alias is the context
	assert.ElementsMatch(t, []string{
		"internal/handlers/handlers.go:6: QueryInt was removed in Fiber v3: use fiber.Query[int](c, \"name\"); " +
			"c is not a fiber.Ctx but embeds one, so if it declares a method of the replacement's name, call the replacement on c.Ctx",
		"internal/handlers/handlers.go:6: QueryInt was removed in Fiber v3: use fiber.Query[int](c, \"name\")",
	}, notes)
}
