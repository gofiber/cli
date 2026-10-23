package v3_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// QueryFloat of v2 read every number strconv.ParseFloat reads, and ParamsInt worded its error
// "failed to convert: ...": the helpers of v3 do neither, so the calls that can show it go to
// the QueryFloat and the ParamsInt of the module package, which are the methods of v2.

// gettersModule makes the directory of a case a module, which can have the package.
var gettersModule = map[string]string{"go.mod": "module example.com/app\n\ngo 1.25\n"}

func Test_MigrateGenericHelpers_QueryFloatKeepsTheNumbersOfV2(t *testing.T) {
	t.Parallel()

	const msg = "Migrating generic helpers"
	ctxRunCases(t, v3.MigrateGenericHelpers, []ctxCase{
		{
			name:   "calls, with and without defaults",
			others: gettersModule,
			in: `package main

import "github.com/gofiber/fiber/v2"

func floats(c *fiber.Ctx) float64 {
	a := c.QueryFloat("a")
	b := c.QueryFloat("b", 1.5)
	defaults := []float64{1, 2}
	d := c.QueryFloat("d", defaults...)
	return a + b + d + c.Status(fiber.StatusOK).QueryFloat("e")
}
`,
			want: `package main

import (
	"example.com/app/internal/fibermigrate"
	"github.com/gofiber/fiber/v2"
)

func floats(c *fiber.Ctx) float64 {
	a := fibermigrate.QueryFloat(c, "a")
	b := fibermigrate.QueryFloat(c, "b", 1.5)
	defaults := []float64{1, 2}
	d := fibermigrate.QueryFloat(c, "d", defaults...)
	return a + b + d + fibermigrate.QueryFloat(c.Status(fiber.StatusOK), "e")
}
`,
			msg: msg,
		},
		{
			name:   "the other getters read what they did",
			others: gettersModule,
			in: `package main

import "github.com/gofiber/fiber/v2"

func ints(c *fiber.Ctx) (int, bool) { return c.QueryInt("n", 3), c.QueryBool("flag") }
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func ints(c *fiber.Ctx) (int, bool) { return fiber.Query[int](c, "n", 3), fiber.Query[bool](c, "flag") }
`,
			msg: msg,
		},
		{
			name:   "as a value",
			others: gettersModule,
			in: `package main

import "github.com/gofiber/fiber/v2"

type handlers struct {
	score func(string, ...float64) float64
}

func values(c *fiber.Ctx) float64 {
	f := c.QueryFloat
	h := handlers{score: c.QueryFloat}
	_ = h
	_ = (*fiber.Ctx).QueryFloat
	return f("v", 2)
}
`,
			want: `package main

import (
	"example.com/app/internal/fibermigrate"
	"github.com/gofiber/fiber/v2"
)

type handlers struct {
	score func(string, ...float64) float64
}

func values(c *fiber.Ctx) float64 {
	f := func(key string, defaultValue ...float64) float64 {
		return fibermigrate.QueryFloat(c, key, defaultValue...)
	}
	h := handlers{score: func(key string, defaultValue ...float64) float64 {
		return fibermigrate.QueryFloat(c, key, defaultValue...)
	}}
	_ = h
	_ = func(c fiber.Ctx, key string, defaultValue ...float64) float64 {
		return fibermigrate.QueryFloat(c, key, defaultValue...)
	}
	return f("v", 2)
}
`,
			msg: msg,
		},
		{
			name:   "a type that embeds the context hands it over",
			others: gettersModule,
			in: `package main

import "github.com/gofiber/fiber/v2"

type wrapper struct{ *fiber.Ctx }

func (w wrapper) Score() float64 { return w.QueryFloat("score") }
`,
			want: `package main

import (
	"example.com/app/internal/fibermigrate"
	"github.com/gofiber/fiber/v2"
)

type wrapper struct{ *fiber.Ctx }

func (w wrapper) Score() float64 { return fibermigrate.QueryFloat(w.Ctx, "score") }
`,
			msg: msg,
		},
		{
			name:   "a name of the file that means the package",
			others: gettersModule,
			in: `package main

import "github.com/gofiber/fiber/v2"

func shadow(c *fiber.Ctx) float64 {
	fibermigrate := 1
	_ = fibermigrate
	return c.QueryFloat("f")
}
`,
			want: `package main

import (
	fibermigrateshim "example.com/app/internal/fibermigrate"
	"github.com/gofiber/fiber/v2"
)

func shadow(c *fiber.Ctx) float64 {
	fibermigrate := 1
	_ = fibermigrate
	return fibermigrateshim.QueryFloat(c, "f")
}
`,
			msg: msg,
		},
		{
			name:   "other receivers are left alone",
			others: gettersModule,
			in: `package main

import "github.com/gofiber/fiber/v2"

type reader struct{}

func (reader) QueryFloat(key string) float64 { return 0 }

func other(c *fiber.Ctx, r reader) float64 { return r.QueryFloat("x") + float64(len(c.Path())) }
`,
		},
	})
}

func Test_MigrateGenericHelpers_ParamsIntKeepsTheErrorOfV2(t *testing.T) {
	t.Parallel()

	const msg = "Migrating generic helpers"
	ctxRunCases(t, v3.MigrateGenericHelpers, []ctxCase{
		{
			name:   "an error whose text is shown or handed on",
			others: gettersModule,
			in: `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func shown(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString(err.Error())
	}
	return c.JSON(id)
}

func handedOn(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return err
	}
	return c.JSON(id)
}

func blankValue(c *fiber.Ctx) error {
	_, err := c.ParamsInt("id")
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return nil
}

func initialization(c *fiber.Ctx) error {
	if id, err := c.ParamsInt("id"); err != nil {
		return fmt.Errorf("bad id: %w", err)
	} else {
		return c.JSON(id)
	}
}
`,
			want: `package main

import (
	"fmt"

	"example.com/app/internal/fibermigrate"
	"github.com/gofiber/fiber/v2"
)

func shown(c *fiber.Ctx) error {
	id, err := fibermigrate.ParamsInt(c, "id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString(err.Error())
	}
	return c.JSON(id)
}

func handedOn(c *fiber.Ctx) error {
	id, err := fibermigrate.ParamsInt(c, "id")
	if err != nil {
		return err
	}
	return c.JSON(id)
}

func blankValue(c *fiber.Ctx) error {
	_, err := fibermigrate.ParamsInt(c, "id")
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return nil
}

func initialization(c *fiber.Ctx) error {
	if id, err := fibermigrate.ParamsInt(c, "id"); err != nil {
		return fmt.Errorf("bad id: %w", err)
	} else {
		return c.JSON(id)
	}
}
`,
			msg: msg,
		},
		{
			name:   "an error that is only compared with nil",
			others: gettersModule,
			in: `package main

import "github.com/gofiber/fiber/v2"

func guard(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return fiber.ErrBadRequest
	}
	return c.JSON(id)
}

func blankValue(c *fiber.Ctx) error {
	_, err := c.ParamsInt("id")
	if err != nil && c.Path() != "/" {
		return fiber.ErrBadRequest
	}
	return nil
}

func initialization(c *fiber.Ctx) error {
	if id, err := c.ParamsInt("id"); err != nil {
		return fiber.ErrBadRequest
	} else {
		return c.JSON(id)
	}
}
`,
			want: `package main

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
)

func guard(c *fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return fiber.ErrBadRequest
	}
	return c.JSON(id)
}

func blankValue(c *fiber.Ctx) error {
	_, err := strconv.Atoi(c.Params("id"))
	if err != nil && c.Path() != "/" {
		return fiber.ErrBadRequest
	}
	return nil
}

func initialization(c *fiber.Ctx) error {
	if id, err := strconv.Atoi(c.Params("id")); err != nil {
		return fiber.ErrBadRequest
	} else {
		return c.JSON(id)
	}
}
`,
			msg: msg,
		},
		{
			name:   "an error that was declared before can be read by code the statements do not show",
			others: gettersModule,
			in: `package main

import "github.com/gofiber/fiber/v2"

func named(c *fiber.Ctx) (id int, err error) {
	id, err = c.ParamsInt("id")
	if err != nil {
		return
	}
	return id, nil
}
`,
			want: `package main

import (
	"example.com/app/internal/fibermigrate"
	"github.com/gofiber/fiber/v2"
)

func named(c *fiber.Ctx) (id int, err error) {
	id, err = fibermigrate.ParamsInt(c, "id")
	if err != nil {
		return
	}
	return id, nil
}
`,
			msg: msg,
		},
		{
			name:   "the error is checked and the code goes on, a result, a spread of defaults, a call in a call",
			others: gettersModule,
			in: `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func reset(c *fiber.Ctx) error {
	page, err := c.ParamsInt("page")
	if err != nil {
		page = 1
	}
	return c.JSON(page)
}

func returned(c *fiber.Ctx) (int, error) {
	return c.ParamsInt("id")
}

func spread(c *fiber.Ctx, defaults ...int) (int, error) {
	return c.ParamsInt("id", defaults...)
}

func shown(c *fiber.Ctx) {
	fmt.Println(c.ParamsInt("id"))
}
`,
			want: `package main

import (
	"fmt"

	"example.com/app/internal/fibermigrate"
	"github.com/gofiber/fiber/v2"
)

func reset(c *fiber.Ctx) error {
	page, err := fibermigrate.ParamsInt(c, "page")
	if err != nil {
		page = 1
	}
	return c.JSON(page)
}

func returned(c *fiber.Ctx) (int, error) {
	return fibermigrate.ParamsInt(c, "id")
}

func spread(c *fiber.Ctx, defaults ...int) (int, error) {
	return fibermigrate.ParamsInt(c, "id", defaults...)
}

func shown(c *fiber.Ctx) {
	fmt.Println(fibermigrate.ParamsInt(c, "id"))
}
`,
			msg: msg,
		},
		{
			name:   "no error to show: a default, or the error is dropped",
			others: gettersModule,
			in: `package main

import "github.com/gofiber/fiber/v2"

func fallback(c *fiber.Ctx) (int, error) {
	id, err := c.ParamsInt("id", 7)
	return id, err
}

func dropped(c *fiber.Ctx) int {
	id, _ := c.ParamsInt("id")
	return id
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func fallback(c *fiber.Ctx) (int, error) {
	id, err := fiber.Params[int](c, "id", 7), error(nil)
	return id, err
}

func dropped(c *fiber.Ctx) int {
	id := fiber.Params[int](c, "id")
	return id
}
`,
			msg: msg,
		},
	})
}

// The file is added once, whichever of the two functions the project calls, and says what it
// holds; a second run leaves it and the code as they are.
func Test_MigrateGenericHelpers_TheGettersFile(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{"main.go": `package main

import "github.com/gofiber/fiber/v2"

func score(c *fiber.Ctx) float64 { return c.QueryFloat("score") }

func page(c *fiber.Ctx) (int, error) { return c.ParamsInt("page") }
`})
	assert.Contains(t, appRun(t, dir, v3.MigrateGenericHelpers), "Migrating generic helpers")

	shim := appRead(t, dir, "internal/fibermigrate/getters.go")
	assert.Contains(t, shim, `This file was added by "fiber migrate": ParamsInt and QueryFloat of Fiber v2`)
	assert.Contains(t, shim, "package fibermigrate\n")
	assert.Contains(t, shim, "func ParamsInt(c fiber.Ctx, key string, defaultValue ...int) (int, error) {")
	assert.Contains(t, shim, "func QueryFloat(c fiber.Ctx, key string, defaultValue ...float64) float64 {")
	assert.NotContains(t, shim, "gettershim")
	assert.Empty(t, internal.DrainNotes(dir), "the migration says nothing about calls it made exact")

	before := appRead(t, dir, "main.go")
	assert.Empty(t, appRun(t, dir, v3.MigrateGenericHelpers))
	assert.Equal(t, before, appRead(t, dir, "main.go"))
	assert.Equal(t, shim, appRead(t, dir, "internal/fibermigrate/getters.go"))
}

// A module cannot have the package when it is not one, or when its directory holds code of its
// own: the calls become the helpers of v3 and the notes say what they read differently.
func Test_MigrateGenericHelpers_WithoutTheGettersFile(t *testing.T) {
	t.Parallel()

	const src = `package main

import "github.com/gofiber/fiber/v2"

func score(c *fiber.Ctx) float64 { return c.QueryFloat("score") }

func page(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString(err.Error())
	}
	return c.JSON(id)
}
`
	for name, files := range map[string]map[string]string{
		"no module":            {"main.go": src},
		"a package of its own": {"main.go": src, "internal/fibermigrate/mine.go": "package fibermigrate\n\nfunc Mine() {}\n"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var dir string
			if name == "no module" {
				dir = t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o600))
			} else {
				dir = appProject(t, files)
			}
			assert.Contains(t, appRun(t, dir, v3.MigrateGenericHelpers), "Migrating generic helpers")

			out := appRead(t, dir, "main.go")
			assert.Contains(t, out, `fiber.Query[float64](c, "score")`)
			assert.Contains(t, out, `strconv.Atoi(c.Params("id"))`)
			assert.NoFileExists(t, filepath.Join(dir, "internal", "fibermigrate", "getters.go"))

			notes := internal.DrainNotes(dir)
			require.Len(t, notes, 2)
			var texts []string
			for _, n := range notes {
				texts = append(texts, n.String())
			}
			text := strings.Join(texts, "\n")
			assert.Contains(t, text, "QueryFloat is fiber.Query[float64] now")
			assert.Contains(t, text, "ParamsInt without a default value is strconv.Atoi")
		})
	}
}
