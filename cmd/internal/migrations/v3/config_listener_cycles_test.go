package v3_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// cycleTimeLimit is far above what the migration needs for the projects below (a
// fraction of a second) and far below what it would need if it followed every
// way round a cycle for as long as the resolvers allow.
const cycleTimeLimit = 30 * time.Second

// migrateWithin runs the migration of the listener settings on the project and
// fails the test when it does not come back in time.
func migrateWithin(t *testing.T, dir string) {
	t.Helper()

	var buf bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- v3.MigrateConfigListenerFields(newCmd(&buf), dir, nil, nil) }()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(cycleTimeLimit):
		t.Fatalf("the migration is still looking for where the app comes from after %s", cycleTimeLimit)
	}
}

// Functions that return one another, each in several ways, lead back to themselves.
// The migration cannot tell which of them makes the app, so the settings go through
// the registry, and it gets there without following every way round the cycle.
func Test_MigrateConfigListenerFields_FunctionsThatReturnEachOther(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

func a(n int) *fiber.App {
	if n == 0 {
		return b(n - 1)
	}
	if n == 1 {
		return c(n - 1)
	}
	if n == 2 {
		return d(n - 1)
	}
	return fiber.New(fiber.Config{DisableStartupMessage: true})
}

func b(n int) *fiber.App {
	if n == 0 {
		return a(n)
	}
	if n == 1 {
		return c(n)
	}
	return d(n)
}

func c(n int) *fiber.App {
	if n == 0 {
		return a(n)
	}
	if n == 1 {
		return b(n)
	}
	return d(n)
}

func d(n int) *fiber.App {
	if n == 0 {
		return a(n)
	}
	if n == 1 {
		return b(n)
	}
	return c(n)
}

func main() {
	app := a(3)
	_ = app.Listen(":3000")
}
`)

	migrateWithin(t, dir)

	got := appRead(t, dir, "main.go")
	assert.Contains(t, got, "return fibermigrate.NewApp(fiber.Config{}, fiber.ListenConfig{DisableStartupMessage: true})")
	assert.Contains(t, got, `app.Listen(":3000", fibermigrate.ListenConfig(app))`)
}

// Types that embed one another through pointers lead back to themselves as well: the
// receiver of the first Listen is no app, and the second one is the app of the file.
func Test_MigrateConfigListenerFields_TypesThatEmbedEachOther(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

type A struct {
	*B
	*C
}

type B struct {
	*A
	*C
}

type C struct {
	*A
	*B
}

func run(a *A) error {
	return a.Listen(":3000")
}

func main() {
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	_ = run(&A{})
	_ = app.Listen(":3001")
}
`)

	migrateWithin(t, dir)

	got := appRead(t, dir, "main.go")
	assert.Contains(t, got, `return a.Listen(":3000")`)
	assert.Contains(t, got, `app.Listen(":3001", fiber.ListenConfig{DisableStartupMessage: true})`)
}
