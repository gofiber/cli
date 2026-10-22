package v3_test

import (
	"testing"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// ParamsInt(key, defaults...) answers the first default and no error when the
// parameter is no integer and there is a default, and the error when there is
// none: which of the two happens is only known when the call runs, so it becomes
// a function literal that is called at once and has the two results of the call
// wherever the call was used.
func Test_MigrateGenericHelpers_SpreadDefaults(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateGenericHelpers, []ctxCase{
		{
			name: "everywhere both results are used",
			in: `package main

import "github.com/gofiber/fiber/v2"

func pair(a int, err error) {}

func userID(c *fiber.Ctx, defaults []int) (int, error) {
	return c.ParamsInt("id", defaults...)
}

func handler(c *fiber.Ctx, defaults ...int) error {
	id, err := c.ParamsInt("id", defaults...)
	if err != nil {
		return err
	}
	var other int
	other, err = c.ParamsInt("other", defaults...)
	var x, y = c.ParamsInt("x", defaults...)
	pair(c.ParamsInt("p", defaults...))
	if v, err := c.ParamsInt("v", defaults...); err != nil {
		return err
	} else if v < 0 {
		return fiber.ErrBadRequest
	}
	_, _, _, _ = id, other, x, y
	return err
}
`,
			want: `package main

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
)

func pair(a int, err error) {}

func userID(c *fiber.Ctx, defaults []int) (int, error) {
	return func() (int, error) {
		value, err := strconv.Atoi(c.Params("id"))
		if err != nil {
			if len(defaults) > 0 {
				return defaults[0], nil
			}
			return 0, err
		}
		return value, nil
	}()
}

func handler(c *fiber.Ctx, defaults ...int) error {
	id, err := func() (int, error) {
		value, err := strconv.Atoi(c.Params("id"))
		if err != nil {
			if len(defaults) > 0 {
				return defaults[0], nil
			}
			return 0, err
		}
		return value, nil
	}()
	if err != nil {
		return err
	}
	var other int
	other, err = func() (int, error) {
		value, err := strconv.Atoi(c.Params("other"))
		if err != nil {
			if len(defaults) > 0 {
				return defaults[0], nil
			}
			return 0, err
		}
		return value, nil
	}()
	var x, y = func() (int, error) {
		value, err := strconv.Atoi(c.Params("x"))
		if err != nil {
			if len(defaults) > 0 {
				return defaults[0], nil
			}
			return 0, err
		}
		return value, nil
	}()
	pair(func() (int, error) {
		value, err := strconv.Atoi(c.Params("p"))
		if err != nil {
			if len(defaults) > 0 {
				return defaults[0], nil
			}
			return 0, err
		}
		return value, nil
	}())
	if v, err := func() (int, error) {
		value, err := strconv.Atoi(c.Params("v"))
		if err != nil {
			if len(defaults) > 0 {
				return defaults[0], nil
			}
			return 0, err
		}
		return value, nil
	}(); err != nil {
		return err
	} else if v < 0 {
		return fiber.ErrBadRequest
	}
	_, _, _, _ = id, other, x, y
	return err
}
`,
			msg: "Migrating generic helpers",
		},
		{
			name: "aliased imports and a multi-line call",
			in: `package main

import (
	sc "strconv"

	f "github.com/gofiber/fiber/v2"
)

type server struct {
	ctx      *f.Ctx
	defaults []int
}

func (s *server) id() (int, error) {
	return s.ctx.ParamsInt(
		"id", // the parameter
		s.defaults...,
	)
}

var _ = sc.Itoa
`,
			want: `package main

import (
	sc "strconv"

	f "github.com/gofiber/fiber/v2"
)

type server struct {
	ctx      *f.Ctx
	defaults []int
}

func (s *server) id() (int, error) {
	return func() (int, error) {
		value, err := sc.Atoi(s.ctx.Params("id"))
		if err != nil {
			if len(s.defaults) > 0 {
				return s.defaults[0], nil
			}
			return 0, err
		}
		return value, nil
	}() /* the parameter */
}

var _ = sc.Itoa
`,
			msg: "Migrating generic helpers",
		},
		{
			name: "operands that do something are evaluated once, before the literal runs",
			in: `package main

import "github.com/gofiber/fiber/v2"

func key() string { return "id" }

func defaults() []int { return nil }

func handler(c *fiber.Ctx) (int, error) {
	return c.Status(200).ParamsInt(key(), defaults()...)
}
`,
			want: `package main

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
)

func key() string { return "id" }

func defaults() []int { return nil }

func handler(c *fiber.Ctx) (int, error) {
	return func(c2 fiber.Ctx, key2 string, defaults2 ...int) (int, error) {
		value, err := strconv.Atoi(c2.Params(key2))
		if err != nil {
			if len(defaults2) > 0 {
				return defaults2[0], nil
			}
			return 0, err
		}
		return value, nil
	}(c.Status(200), key(), defaults()...)
}
`,
			msg: "Migrating generic helpers",
		},
		{
			name: "the names of the literal stay clear of the ones the call uses",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx, err []int) (int, error) {
	return c.ParamsInt(fiber.MIMEApplicationJSON, err...)
}
`,
			want: `package main

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
)

func handler(c *fiber.Ctx, err []int) (int, error) {
	return func() (int, error) {
		value, err2 := strconv.Atoi(c.Params(fiber.MIMEApplicationJSON))
		if err2 != nil {
			if len(err) > 0 {
				return err[0], nil
			}
			return 0, err2
		}
		return value, nil
	}()
}
`,
			msg: "Migrating generic helpers",
		},
		{
			name: "dot import",
			in: `package main

import . "github.com/gofiber/fiber/v2"

func handler(c *Ctx, defaults []int) (int, error) {
	return c.Status(200).ParamsInt("id", defaults...)
}
`,
			want: `package main

import (
	"strconv"

	. "github.com/gofiber/fiber/v2"
)

func handler(c *Ctx, defaults []int) (int, error) {
	return func(c2 Ctx) (int, error) {
		value, err := strconv.Atoi(c2.Params("id"))
		if err != nil {
			if len(defaults) > 0 {
				return defaults[0], nil
			}
			return 0, err
		}
		return value, nil
	}(c.Status(200))
}
`,
			msg: "Migrating generic helpers",
		},
		{
			name: "a strconv that a variable hides is reported",
			in: `package main

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
)

func handler(c *fiber.Ctx, defaults []int) (int, error) {
	strconv := strconv.Itoa(1)
	_ = strconv
	return c.ParamsInt("id", defaults...)
}
`,
			notes: []string{"main.go:12: c.ParamsInt was removed in Fiber v3"},
		},
	})
}
