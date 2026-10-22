package v3_test

import (
	"testing"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// v2 returned 0 with the error of ParamsInt, strconv.Atoi returns the largest int for a number
// that does not fit: the migrated call has the 0 wherever the code can see the value.
func Test_MigrateGenericHelpers_ParamsIntError(t *testing.T) {
	t.Parallel()

	const msg = "Migrating generic helpers"
	note := []string{"ParamsInt without a default value"}
	ctxRunCases(t, v3.MigrateGenericHelpers, []ctxCase{
		{
			name: "an error that is dropped leaves the helper, which reads 0 for a number it cannot read",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	id, _ := c.ParamsInt("id")
	var page, _ = c.ParamsInt("page")
	var limit int
	limit, _ = c.ParamsInt("limit")
	_, _ = c.ParamsInt("ignored")
	c.ParamsInt("discarded")
	_, _, _ = id, page, limit
	return nil
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	id := fiber.Params[int](c, "id")
	var page = fiber.Params[int](c, "page")
	var limit int
	limit = fiber.Params[int](c, "limit")
	_ = fiber.Params[int](c, "ignored")
	fiber.Params[int](c, "discarded")
	_, _, _ = id, page, limit
	return nil
}
`,
			msg: msg,
		},
		{
			name: "an error that the code checks and leaves on keeps Atoi",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return err
	}
	size := c.QueryInt("size", 10)
	page, err := c.ParamsInt("page")
	println(size)
	if err != nil {
		panic(err)
	}
	if limit, err := c.ParamsInt("limit"); err != nil {
		return fiber.ErrBadRequest
	} else if limit < 0 {
		return fiber.ErrBadRequest
	}
	switch c.Method() {
	case "GET":
		offset, err := c.ParamsInt("offset")
		if err != nil {
			return err
		}
		_ = offset
	}
	_, err = c.ParamsInt("ignored value")
	_, _ = id, page
	return err
}
`,
			want: `package main

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
)

func handler(c *fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return err
	}
	size := fiber.Query[int](c, "size", 10)
	page, err := strconv.Atoi(c.Params("page"))
	println(size)
	if err != nil {
		panic(err)
	}
	if limit, err := strconv.Atoi(c.Params("limit")); err != nil {
		return fiber.ErrBadRequest
	} else if limit < 0 {
		return fiber.ErrBadRequest
	}
	switch c.Method() {
	case "GET":
		offset, err := strconv.Atoi(c.Params("offset"))
		if err != nil {
			return err
		}
		_ = offset
	}
	_, err = strconv.Atoi(c.Params("ignored value"))
	_, _ = id, page
	return err
}
`,
			msg:   msg,
			notes: note,
		},
		{
			name: "a value that the code can read after the error is made 0",
			in: `package main

import (
	"log"

	"github.com/gofiber/fiber/v2"
)

type request struct{ ID int }

func handler(c *fiber.Ctx) (int, error) {
	id, err := c.ParamsInt("id")
	if err != nil {
		log.Println("bad id", id)
		return 0, err
	}
	page, err := c.ParamsInt("page")
	if err != nil || page < 1 {
		page = 1
	}
	var r request
	r.ID, err = c.ParamsInt("rid")
	size, err := c.ParamsInt("size")
	println(size)
	return id + page + r.ID, err
}
`,
			want: `package main

import (
	"log"
	"strconv"

	"github.com/gofiber/fiber/v2"
)

type request struct{ ID int }

func handler(c *fiber.Ctx) (int, error) {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		id = 0
	}
	if err != nil {
		log.Println("bad id", id)
		return 0, err
	}
	page, err := strconv.Atoi(c.Params("page"))
	if err != nil {
		page = 0
	}
	if err != nil || page < 1 {
		page = 1
	}
	var r request
	r.ID, err = strconv.Atoi(c.Params("rid"))
	if err != nil {
		r.ID = 0
	}
	size, err := strconv.Atoi(c.Params("size"))
	if err != nil {
		size = 0
	}
	println(size)
	return id + page + r.ID, err
}
`,
			msg:   msg,
			notes: note,
		},
		{
			name: "the variable of an earlier statement can be read by a closure",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) (id int, err error) {
	defer func() { println(id) }()
	id, err = c.ParamsInt("id")
	if err != nil {
		return
	}
	return id, nil
}

func shadows(c *fiber.Ctx) error {
	id := 7
	read := func() int { return id }
	id, err := c.ParamsInt("id")
	if err != nil {
		return err
	}
	_ = read
	return nil
}
`,
			want: `package main

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
)

func handler(c *fiber.Ctx) (id int, err error) {
	defer func() { println(id) }()
	id, err = strconv.Atoi(c.Params("id"))
	if err != nil {
		id = 0
	}
	if err != nil {
		return
	}
	return id, nil
}

func shadows(c *fiber.Ctx) error {
	id := 7
	read := func() int { return id }
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		id = 0
	}
	if err != nil {
		return err
	}
	_ = read
	return nil
}
`,
			msg:   msg,
			notes: note,
		},
		{
			name: "the results of the call that a function returns are 0 and the error",
			in: `package main

import "github.com/gofiber/fiber/v2"

func userID(c *fiber.Ctx) (int, error) {
	return c.ParamsInt("id")
}

func named(c *fiber.Ctx) (id int, err error) {
	return c.ParamsInt("id")
}

func in(c *fiber.Ctx) func() (int, error) {
	return func() (int, error) { return c.ParamsInt("other") }
}

func plus(c *fiber.Ctx) (int, error, bool) {
	return 1, nil, true
}
`,
			want: `package main

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
)

func userID(c *fiber.Ctx) (int, error) {
	value, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return 0, err
	}
	return value, nil
}

func named(c *fiber.Ctx) (id int, err error) {
	value, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return 0, err
	}
	return value, nil
}

func in(c *fiber.Ctx) func() (int, error) {
	return func() (int, error) {
		value, err := strconv.Atoi(c.Params("other"))
		if err != nil {
			return 0, err
		}
		return value, nil
	}
}

func plus(c *fiber.Ctx) (int, error, bool) {
	return 1, nil, true
}
`,
			msg:   msg,
			notes: note,
		},
		{
			name: "the call that another call takes both results of is a literal",
			in: `package main

import "github.com/gofiber/fiber/v2"

func pair(a int, err error) error { return err }

func handler(c *fiber.Ctx) error {
	return pair(c.ParamsInt(next(c)))
}

func next(c *fiber.Ctx) string { return c.Params("x") }
`,
			want: `package main

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
)

func pair(a int, err error) error { return err }

func handler(c *fiber.Ctx) error {
	return pair(func(key string) (int, error) {
		value, err := strconv.Atoi(c.Params(key))
		if err != nil {
			return 0, err
		}
		return value, nil
	}(next(c)))
}

func next(c *fiber.Ctx) string { return c.Params("x") }
`,
			msg:   msg,
			notes: note,
		},
		{
			name: "QueryFloat reads decimal numbers only",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	_ = c.QueryFloat("a")
	_ = c.QueryFloat("b", 1.5)
	f := c.QueryFloat
	_ = f
	return nil
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx) error {
	_ = fiber.Query[float64](c, "a")
	_ = fiber.Query[float64](c, "b", 1.5)
	f := func(key string, defaultValue ...float64) float64 {
		return fiber.Query[float64](c, key, defaultValue...)
	}
	_ = f
	return nil
}
`,
			msg:   msg,
			notes: []string{"QueryFloat is fiber.Query[float64] now, which reads decimal numbers only"},
		},
	})
}
