package v3_test

import (
	"testing"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// The Start and End of the ranges that c.Range returns are int in v2 and int64 in
// v3: the places that read them get the int back with a conversion.
func Test_MigrateCtxRenames_RangeReads(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateCtxRenames, []ctxCase{
		{
			name: "the ranges of the result of c.Range",
			in: `package main

import (
	"log"
	"strconv"

	"github.com/gofiber/fiber/v2"
)

var data = make([]byte, 1000)

func main() {
	app := fiber.New()

	app.Get("/var", func(c *fiber.Ctx) error {
		size := len(data)
		r, err := c.Range(size)
		if err != nil {
			return err
		}
		for _, rs := range r.Ranges {
			c.Set("X-Range-Start", strconv.Itoa(rs.Start))
			c.Set("X-Range-End", strconv.Itoa(rs.End))
		}
		return c.Send(data[r.Ranges[0].Start : r.Ranges[0].End+1])
	})

	app.Get("/len", func(c *fiber.Ctx) error {
		r, err := c.Range(len(data))
		if err != nil {
			return err
		}
		var total int
		for _, rs := range r.Ranges {
			total += rs.End - rs.Start + 1
		}
		return c.SendString(strconv.Itoa(total) + r.Type)
	})

	log.Fatal(app.Listen(":3000"))
}
`,
			want: `package main

import (
	"log"
	"strconv"

	"github.com/gofiber/fiber/v2"
)

var data = make([]byte, 1000)

func main() {
	app := fiber.New()

	app.Get("/var", func(c *fiber.Ctx) error {
		size := len(data)
		r, err := c.Range(int64(size))
		if err != nil {
			return err
		}
		for _, rs := range r.Ranges {
			c.Set("X-Range-Start", strconv.Itoa(int(rs.Start)))
			c.Set("X-Range-End", strconv.Itoa(int(rs.End)))
		}
		return c.Send(data[int(r.Ranges[0].Start) : int(r.Ranges[0].End)+1])
	})

	app.Get("/len", func(c *fiber.Ctx) error {
		r, err := c.Range(int64(len(data)))
		if err != nil {
			return err
		}
		var total int
		for _, rs := range r.Ranges {
			total += int(rs.End) - int(rs.Start) + 1
		}
		return c.SendString(strconv.Itoa(total) + r.Type)
	})

	log.Fatal(app.Listen(":3000"))
}
`,
			msg: "Migrating context method renames",
		},
		{
			name: "aliased import, the other ways to name a range, and a result in an if",
			in: `package main

import (
	"fmt"

	f "github.com/gofiber/fiber/v2"
)

func handler(c *f.Ctx) error {
	if r, err := c.Range(100); err == nil {
		ranges := r.Ranges
		first := ranges[0]
		last := r.Ranges[len(r.Ranges)-1]
		for i := range r.Ranges {
			fmt.Println(r.Ranges[i].End - first.Start)
		}
		for i, rs := range ranges[1:] {
			fmt.Println(i, rs.Start, last.End)
		}
		var pending f.RangeSet
		pending = first
		fmt.Println(pending.Start)
		func() {
			fmt.Println(first.End)
		}()
	}
	return nil
}
`,
			want: `package main

import (
	"fmt"

	f "github.com/gofiber/fiber/v2"
)

func handler(c *f.Ctx) error {
	if r, err := c.Range(100); err == nil {
		ranges := r.Ranges
		first := ranges[0]
		last := r.Ranges[len(r.Ranges)-1]
		for i := range r.Ranges {
			fmt.Println(int(r.Ranges[i].End) - int(first.Start))
		}
		for i, rs := range ranges[1:] {
			fmt.Println(i, int(rs.Start), int(last.End))
		}
		var pending f.RangeSet
		pending = first
		fmt.Println(int(pending.Start))
		func() {
			fmt.Println(int(first.End))
		}()
	}
	return nil
}
`,
			msg: "Migrating context method renames",
		},
		{
			name: "values that are declared as a range: parameters, results and fields",
			in: `package main

import (
	"slices"
	"strconv"

	"github.com/gofiber/fiber/v2"
)

type job struct {
	ranges []fiber.RangeSet
	all    fiber.Range
}

func (j *job) total() (n int) {
	for _, rs := range j.ranges {
		n += rs.End - rs.Start
	}
	return n + j.all.Ranges[0].End
}

func describe(r fiber.Range, extra ...fiber.RangeSet) string {
	slices.SortFunc(r.Ranges, func(a, b fiber.RangeSet) int { return a.Start - b.Start })
	return r.Type + strconv.Itoa(r.Ranges[0].Start) + strconv.Itoa(extra[0].End)
}

func parse(c *fiber.Ctx) (fiber.Range, error) {
	return c.Range(10)
}

func pick(c *fiber.Ctx) (fiber.RangeSet, error) {
	r, err := parse(c)
	if err != nil {
		return fiber.RangeSet{}, err
	}
	return r.Ranges[0], nil
}

func use(c *fiber.Ctx) int {
	rs, _ := pick(c)
	return rs.Start
}
`,
			want: `package main

import (
	"slices"
	"strconv"

	"github.com/gofiber/fiber/v2"
)

type job struct {
	ranges []fiber.RangeSet
	all    fiber.Range
}

func (j *job) total() (n int) {
	for _, rs := range j.ranges {
		n += int(rs.End) - int(rs.Start)
	}
	return n + int(j.all.Ranges[0].End)
}

func describe(r fiber.Range, extra ...fiber.RangeSet) string {
	slices.SortFunc(r.Ranges, func(a, b fiber.RangeSet) int { return int(a.Start) - int(b.Start) })
	return r.Type + strconv.Itoa(int(r.Ranges[0].Start)) + strconv.Itoa(int(extra[0].End))
}

func parse(c *fiber.Ctx) (fiber.Range, error) {
	return c.Range(10)
}

func pick(c *fiber.Ctx) (fiber.RangeSet, error) {
	r, err := parse(c)
	if err != nil {
		return fiber.RangeSet{}, err
	}
	return r.Ranges[0], nil
}

func use(c *fiber.Ctx) int {
	rs, _ := pick(c)
	return int(rs.Start)
}
`,
			msg: "Migrating context method renames",
		},
		{
			name: "conversions the code has already stay",
			in: `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func handler(c *fiber.Ctx) error {
	r, err := c.Range(int64(10))
	if err != nil {
		return err
	}
	for _, rs := range r.Ranges {
		fmt.Println(int(rs.Start), int64(rs.End), float64(rs.End), uint(rs.Start))
	}
	return nil
}
`,
		},
		{
			name: "other values with a Start and an End stay",
			in: `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

type span struct{ Start, End int }

func handler(c *fiber.Ctx, r span, ranges []span) error {
	rs, err := c.Range(10)
	if err != nil {
		return err
	}
	fmt.Println(r.Start, r.End, ranges[0].Start, len(rs.Ranges))
	for _, rs := range ranges {
		fmt.Println(rs.Start, rs.End)
	}
	{
		rs := span{Start: 1}
		fmt.Println(rs.Start)
	}
	func(rs span) {
		fmt.Println(rs.End)
	}(r)
	return nil
}
`,
		},
		{
			name: "what is assigned to the Start or the End of a range is an int64",
			in: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx, size, n int) error {
	r, err := c.Range(size)
	if err != nil {
		return err
	}
	rs := r.Ranges[0]
	rs.End = size - 1
	rs.Start = rs.End
	rs.Start += n
	rs.End++
	r.Ranges[0].Start = 0
	r.Ranges[0].End = int64(n)
	r.Ranges[0].End -= rs.Start + 1
	r.Ranges[1].Start, r.Ranges[1].End = 1, size
	_ = fiber.RangeSet{Start: 1, End: rs.End}
	_ = []fiber.RangeSet{{Start: n, End: rs.Start}, {n, -1}}
	_ = fiber.Range{Type: "bytes", Ranges: []fiber.RangeSet{{Start: 0, End: size - 1}}}
	return nil
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

func handler(c *fiber.Ctx, size, n int) error {
	r, err := c.Range(int64(size))
	if err != nil {
		return err
	}
	rs := r.Ranges[0]
	rs.End = int64(size - 1)
	rs.Start = rs.End
	rs.Start += int64(n)
	rs.End++
	r.Ranges[0].Start = 0
	r.Ranges[0].End = int64(n)
	r.Ranges[0].End -= int64(int(rs.Start) + 1)
	r.Ranges[1].Start, r.Ranges[1].End = 1, int64(size)
	_ = fiber.RangeSet{Start: 1, End: rs.End}
	_ = []fiber.RangeSet{{Start: int64(n), End: rs.Start}, {int64(n), -1}}
	_ = fiber.Range{Type: "bytes", Ranges: []fiber.RangeSet{{Start: 0, End: int64(size - 1)}}}
	return nil
}
`,
			msg: "Migrating context method renames",
		},
		{
			name: "dot import",
			in: `package main

import . "github.com/gofiber/fiber/v2"

func handler(c *Ctx, rs RangeSet) (int, error) {
	r, err := c.Range(10)
	if err != nil {
		return 0, err
	}
	return r.Ranges[0].End + rs.Start, nil
}
`,
			want: `package main

import . "github.com/gofiber/fiber/v2"

func handler(c *Ctx, rs RangeSet) (int, error) {
	r, err := c.Range(10)
	if err != nil {
		return 0, err
	}
	return int(r.Ranges[0].End) + int(rs.Start), nil
}
`,
			msg: "Migrating context method renames",
		},
	})
}

// A range that goes where the tracker does not follow it is reported: what
// happens to its Start and End there is for the user to check.
func Test_MigrateCtxRenames_RangeEscapes(t *testing.T) {
	t.Parallel()

	const note = "c.Range takes an int64 size in Fiber v3, and the Start and End of the returned ranges are int64: convert them where they are used as int"

	ctxRunCases(t, v3.MigrateCtxRenames, []ctxCase{
		{
			name: "a function of another package",
			in: `package main

import (
	"example.com/web"

	"github.com/gofiber/fiber/v2"
)

func handler(c *fiber.Ctx) error {
	r, err := c.Range(10)
	if err != nil {
		return err
	}
	web.Serve(r)
	web.Serve(r.Ranges[0])
	return nil
}
`,
			notes: []string{"main.go:14: " + note + " (1 more in this project)"},
		},
		{
			name: "a function of the file that takes the types of the fiber package, or something it does not look into",
			in: `package main

import (
	"fmt"
	"log"
	"sort"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
)

func handler(c *fiber.Ctx) error {
	r, err := c.Range(10)
	if err != nil {
		return err
	}
	serve(r)
	each(r.Ranges...)
	fmt.Println(r, r.Ranges, len(r.Ranges), cap(r.Ranges))
	log.Printf("%v", r.Ranges[0])
	sort.Slice(r.Ranges, func(i, j int) bool { return r.Ranges[i].Start < r.Ranges[j].Start })
	assert.Equal(nil, fiber.RangeSet{}, r.Ranges[0])
	_ = append(r.Ranges, r.Ranges[0])
	return c.JSON(r)
}

func serve(r fiber.Range) {}

func each(sets ...fiber.RangeSet) {}
`,
			want: `package main

import (
	"fmt"
	"log"
	"sort"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
)

func handler(c *fiber.Ctx) error {
	r, err := c.Range(10)
	if err != nil {
		return err
	}
	serve(r)
	each(r.Ranges...)
	fmt.Println(r, r.Ranges, len(r.Ranges), cap(r.Ranges))
	log.Printf("%v", r.Ranges[0])
	sort.Slice(r.Ranges, func(i, j int) bool { return int(r.Ranges[i].Start) < int(r.Ranges[j].Start) })
	assert.Equal(nil, fiber.RangeSet{}, r.Ranges[0])
	_ = append(r.Ranges, r.Ranges[0])
	return c.JSON(r)
}

func serve(r fiber.Range) {}

func each(sets ...fiber.RangeSet) {}
`,
			msg: "Migrating context method renames",
		},
		{
			name: "a field, a channel, an interface and a return that are not declared as a range",
			in: `package main

import "github.com/gofiber/fiber/v2"

type holder struct {
	data any
	r    fiber.Range
	n    int
}

func handler(c *fiber.Ctx, h *holder, ch chan fiber.RangeSet) (any, error) {
	r, err := c.Range(10)
	if err != nil {
		return nil, err
	}
	h.r = r
	h.data = r.Ranges[0]
	_ = holder{data: r, r: r}
	ch <- r.Ranges[0]
	var x any = r
	_ = x
	return r.Ranges[0], nil
}
`,
			notes: []string{"main.go:17: " + note + " (4 more in this project)"},
		},
		{
			name: "the results of c.Range that are not assigned",
			in: `package main

import "github.com/gofiber/fiber/v2"

func first(c *fiber.Ctx) (fiber.Range, error) {
	return c.Range(10)
}

func second(c *fiber.Ctx) (any, error) {
	return c.Range(10)
}

func third(c *fiber.Ctx) {
	consume(c.Range(10))
}

func consume(r fiber.Range, err error) {}
`,
			notes: []string{"main.go:10: " + note + " (1 more in this project)"},
		},
		{
			name: "the address of a Start",
			in: `package main

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
)

func handler(c *fiber.Ctx) error {
	r, err := c.Range(10)
	if err != nil {
		return err
	}
	p := &r.Ranges[0].Start
	_ = strconv.Itoa(int(*p))
	return nil
}
`,
			notes: []string{"main.go:14: " + note},
		},
	})
}
