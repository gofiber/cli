package v3_test

import (
	"testing"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// The Start and End of the ranges are followed through generic types and functions, maps,
// channels, function values, interfaces and the generic functions of the standard library: the tracker infers
// what a value is from the declarations of the code, where there are no types to ask.
func Test_MigrateCtxRenames_RangeShapes(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateCtxRenames, []ctxCase{
		{
			name: "a range in a generic type",
			in: `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

type box[T any] struct{ v T }

func (b *box[T]) set(v T) { b.v = v }

func (b *box[T]) get() T { return b.v }

type pair[K comparable, V any] struct {
	key K
	val V
}

func handler(c *fiber.Ctx) int {
	r, err := c.Range(100)
	if err != nil {
		return 0
	}
	b := &box[fiber.Range]{}
	b.set(r)
	fmt.Println(b.v.Ranges[0].Start, b.get().Ranges[0].End)
	p := pair[string, fiber.RangeSet]{key: "a", val: r.Ranges[0]}
	p.val.End = len(p.key)
	one := box[[]fiber.RangeSet]{v: r.Ranges}
	return one.v[0].Start + p.val.Start
}
`,
			want: `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

type box[T any] struct{ v T }

func (b *box[T]) set(v T) { b.v = v }

func (b *box[T]) get() T { return b.v }

type pair[K comparable, V any] struct {
	key K
	val V
}

func handler(c *fiber.Ctx) int {
	r, err := c.Range(100)
	if err != nil {
		return 0
	}
	b := &box[fiber.Range]{}
	b.set(r)
	fmt.Println(int(b.v.Ranges[0].Start), int(b.get().Ranges[0].End))
	p := pair[string, fiber.RangeSet]{key: "a", val: r.Ranges[0]}
	p.val.End = int64(len(p.key))
	one := box[[]fiber.RangeSet]{v: r.Ranges}
	return int(one.v[0].Start) + int(p.val.Start)
}
`,
			msg: "Migrating context method renames",
		},
		{
			name: "a range through a generic function",
			in: `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func first[T any](xs []T) T { return xs[0] }

func wrap[T any](v T) *struct{ item T } { return &struct{ item T }{v} }

func apply[T, R any](v T, f func(T) R) R { return f(v) }

func handler(c *fiber.Ctx) int {
	r, _ := c.Range(100)
	rs := first(r.Ranges)
	total := rs.Start + first[fiber.RangeSet](r.Ranges).End
	total += apply(r, func(r fiber.Range) int { return len(r.Ranges) })
	total += apply(rs, func(rs fiber.RangeSet) int { return rs.End })
	total += wrap(rs).item.Start
	return total
}
`,
			want: `package main

import (
	"github.com/gofiber/fiber/v2"
)

func first[T any](xs []T) T { return xs[0] }

func wrap[T any](v T) *struct{ item T } { return &struct{ item T }{v} }

func apply[T, R any](v T, f func(T) R) R { return f(v) }

func handler(c *fiber.Ctx) int {
	r, _ := c.Range(100)
	rs := first(r.Ranges)
	total := int(rs.Start) + int(first[fiber.RangeSet](r.Ranges).End)
	total += apply(r, func(r fiber.Range) int { return len(r.Ranges) })
	total += apply(rs, func(rs fiber.RangeSet) int { return int(rs.End) })
	total += int(wrap(rs).item.Start)
	return total
}
`,
			msg: "Migrating context method renames",
		},
		{
			name: "a range in a map",
			in: `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func handler(c *fiber.Ctx) int {
	r, _ := c.Range(100)
	byName := map[string]fiber.RangeSet{"first": r.Ranges[0]}
	all := map[string][]fiber.RangeSet{}
	all["r"] = r.Ranges
	whole := map[string]fiber.Range{"r": r}
	total := byName["first"].Start + all["r"][0].End + whole["r"].Ranges[1].Start
	for name, rs := range byName {
		total += len(name) + rs.End
	}
	for _, rs := range all {
		total += rs[0].Start
	}
	byName["second"] = fiber.RangeSet{Start: total, End: total + 1}
	return total
}
`,
			want: `package main

import (
	"github.com/gofiber/fiber/v2"
)

func handler(c *fiber.Ctx) int {
	r, _ := c.Range(100)
	byName := map[string]fiber.RangeSet{"first": r.Ranges[0]}
	all := map[string][]fiber.RangeSet{}
	all["r"] = r.Ranges
	whole := map[string]fiber.Range{"r": r}
	total := int(byName["first"].Start) + int(all["r"][0].End) + int(whole["r"].Ranges[1].Start)
	for name, rs := range byName {
		total += len(name) + int(rs.End)
	}
	for _, rs := range all {
		total += int(rs[0].Start)
	}
	byName["second"] = fiber.RangeSet{Start: int64(total), End: int64(total + 1)}
	return total
}
`,
			msg: "Migrating context method renames",
		},
		{
			name: "a range in a channel",
			in: `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func produce(r fiber.Range, out chan<- fiber.RangeSet) {
	for _, rs := range r.Ranges {
		out <- rs
	}
	close(out)
}

func handler(c *fiber.Ctx) int {
	r, _ := c.Range(100)
	ch := make(chan fiber.RangeSet, len(r.Ranges))
	go produce(r, ch)
	total := 0
	for rs := range ch {
		total += rs.Start
	}
	feed := make(chan fiber.Range, 1)
	feed <- r
	select {
	case got := <-feed:
		total += got.Ranges[0].End
	}
	if rs, ok := <-ch; ok {
		total += rs.End
	}
	return total
}
`,
			want: `package main

import (
	"github.com/gofiber/fiber/v2"
)

func produce(r fiber.Range, out chan<- fiber.RangeSet) {
	for _, rs := range r.Ranges {
		out <- rs
	}
	close(out)
}

func handler(c *fiber.Ctx) int {
	r, _ := c.Range(100)
	ch := make(chan fiber.RangeSet, len(r.Ranges))
	go produce(r, ch)
	total := 0
	for rs := range ch {
		total += int(rs.Start)
	}
	feed := make(chan fiber.Range, 1)
	feed <- r
	select {
	case got := <-feed:
		total += int(got.Ranges[0].End)
	}
	if rs, ok := <-ch; ok {
		total += int(rs.End)
	}
	return total
}
`,
			msg: "Migrating context method renames",
		},
		{
			name: "a range through a function value",
			in: `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

type server struct {
	parse func(int) (fiber.Range, error)
	each  func(func(fiber.RangeSet))
}

func handler(c *fiber.Ctx, s *server) int {
	rangeOf := c.Range
	r, _ := rangeOf(100)
	total := r.Ranges[0].Start
	r2, _ := s.parse(10)
	total += r2.Ranges[0].End
	s.each(func(rs fiber.RangeSet) { total += rs.Start })
	pick := func() fiber.RangeSet { return r.Ranges[0] }
	total += pick().End
	get := func(i int) fiber.RangeSet { return r.Ranges[i] }
	return total + get(1).Start + func() fiber.Range { return r }().Ranges[0].End
}
`,
			want: `package main

import (
	"github.com/gofiber/fiber/v2"
)

type server struct {
	parse func(int) (fiber.Range, error)
	each  func(func(fiber.RangeSet))
}

func handler(c *fiber.Ctx, s *server) int {
	rangeOf := func(size int) (fiber.Range, error) {
		return c.Range(int64(size))
	}
	r, _ := rangeOf(100)
	total := int(r.Ranges[0].Start)
	r2, _ := s.parse(10)
	total += int(r2.Ranges[0].End)
	s.each(func(rs fiber.RangeSet) { total += int(rs.Start) })
	pick := func() fiber.RangeSet { return r.Ranges[0] }
	total += int(pick().End)
	get := func(i int) fiber.RangeSet { return r.Ranges[i] }
	return total + int(get(1).Start) + int(func() fiber.Range { return r }().Ranges[0].End)
}
`,
			msg: "Migrating context method renames",
		},
		{
			name: "a range in an interface",
			in: `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

type parser interface {
	Parse(c *fiber.Ctx) (fiber.Range, error)
	Sets() []fiber.RangeSet
}

func handler(c *fiber.Ctx, p parser, v any) int {
	r, _ := p.Parse(c)
	total := r.Ranges[0].Start + p.Sets()[0].End
	var stored any = r.Ranges[0]
	if rs, ok := stored.(fiber.RangeSet); ok {
		total += rs.Start
	}
	total += v.(fiber.Range).Ranges[0].End
	switch x := v.(type) {
	case fiber.Range:
		total += x.Ranges[0].Start
	case fiber.RangeSet:
		total += x.End
	case int:
		total += x
	}
	return total
}
`,
			want: `package main

import (
	"github.com/gofiber/fiber/v2"
)

type parser interface {
	Parse(c *fiber.Ctx) (fiber.Range, error)
	Sets() []fiber.RangeSet
}

func handler(c *fiber.Ctx, p parser, v any) int {
	r, _ := p.Parse(c)
	total := int(r.Ranges[0].Start) + int(p.Sets()[0].End)
	var stored any = r.Ranges[0]
	if rs, ok := stored.(fiber.RangeSet); ok {
		total += int(rs.Start)
	}
	total += int(v.(fiber.Range).Ranges[0].End)
	switch x := v.(type) {
	case fiber.Range:
		total += int(x.Ranges[0].Start)
	case fiber.RangeSet:
		total += int(x.End)
	case int:
		total += x
	}
	return total
}
`,
			msg: "Migrating context method renames",
		},
		{
			name: "ranges through the generic functions of the standard library",
			in: `package main

import (
	"maps"
	"slices"

	"github.com/gofiber/fiber/v2"
)

func handler(c *fiber.Ctx) int {
	r, _ := c.Range(100)
	copied := slices.Clone(r.Ranges)
	total := copied[0].Start
	for _, rs := range slices.All(r.Ranges) {
		total += rs.End
	}
	for rs := range slices.Values(r.Ranges) {
		total += rs.Start
	}
	longest := slices.MaxFunc(r.Ranges, func(a, b fiber.RangeSet) int { return int(a.End-a.Start) - int(b.End-b.Start) })
	total += longest.End
	byName := map[string]fiber.RangeSet{"a": r.Ranges[0]}
	for _, rs := range maps.All(byName) {
		total += rs.Start
	}
	for rs := range maps.Values(byName) {
		total += rs.End
	}
	slices.SortFunc(r.Ranges, func(a, b fiber.RangeSet) int { return a.Start - b.Start })
	return total
}
`,
			want: `package main

import (
	"maps"
	"slices"

	"github.com/gofiber/fiber/v2"
)

func handler(c *fiber.Ctx) int {
	r, _ := c.Range(100)
	copied := slices.Clone(r.Ranges)
	total := int(copied[0].Start)
	for _, rs := range slices.All(r.Ranges) {
		total += int(rs.End)
	}
	for rs := range slices.Values(r.Ranges) {
		total += int(rs.Start)
	}
	longest := slices.MaxFunc(r.Ranges, func(a, b fiber.RangeSet) int { return int(int(a.End)-int(a.Start)) - int(int(b.End)-int(b.Start)) })
	total += int(longest.End)
	byName := map[string]fiber.RangeSet{"a": r.Ranges[0]}
	for _, rs := range maps.All(byName) {
		total += int(rs.Start)
	}
	for rs := range maps.Values(byName) {
		total += int(rs.End)
	}
	slices.SortFunc(r.Ranges, func(a, b fiber.RangeSet) int { return int(a.Start) - int(b.Start) })
	return total
}
`,
			msg: "Migrating context method renames",
		},
		{
			name: "package-level variables and types",
			in: `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

var cache = map[string]fiber.Range{}

var current fiber.RangeSet

type spans []fiber.RangeSet

func (s spans) total() (n int) {
	for _, rs := range s {
		n += rs.End - rs.Start
	}
	return n
}

func (s spans) first() fiber.RangeSet { return s[0] }

func handler(c *fiber.Ctx) int {
	r, _ := c.Range(100)
	cache["r"] = r
	current = cache["r"].Ranges[0]
	s := spans(r.Ranges)
	return current.Start + s.first().End + s.total()
}
`,
			want: `package main

import (
	"github.com/gofiber/fiber/v2"
)

var cache = map[string]fiber.Range{}

var current fiber.RangeSet

type spans []fiber.RangeSet

func (s spans) total() (n int) {
	for _, rs := range s {
		n += int(rs.End) - int(rs.Start)
	}
	return n
}

func (s spans) first() fiber.RangeSet { return s[0] }

func handler(c *fiber.Ctx) int {
	r, _ := c.Range(100)
	cache["r"] = r
	current = cache["r"].Ranges[0]
	s := spans(r.Ranges)
	return int(current.Start) + int(s.first().End) + s.total()
}
`,
			msg: "Migrating context method renames",
		},
		{
			name: "a read of a value whose type is not known is reported in a file that handles ranges",
			in: `package main

import (
	"example.com/web"

	"github.com/gofiber/fiber/v2"
)

func handler(c *fiber.Ctx) int {
	r, _ := c.Range(100)
	other := web.Parse(c)
	return r.Ranges[0].Start + other.Start + other.Ranges[0].End
}
`,
			want: `package main

import (
	"example.com/web"

	"github.com/gofiber/fiber/v2"
)

func handler(c *fiber.Ctx) int {
	r, _ := c.Range(100)
	other := web.Parse(c)
	return int(r.Ranges[0].Start) + other.Start + other.Ranges[0].End
}
`,
			msg:   "Migrating context method renames",
			notes: []string{"main.go:12: the Start and End of a fiber.RangeSet are int64 in Fiber v3: the type of this value is not known here"},
		},
		{
			name: "a value of another type with a Start and an End is left alone",
			in: `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

type span struct{ Start, End int }

func (span) Len() int { return 0 }

func handler(c *fiber.Ctx) {
	r, _ := c.Range(10)
	s := span{1, 2}
	spans := []span{s}
	fmt.Println(s.Start, spans[0].End, r.Ranges[0].Start, s.Len())
}
`,
			want: `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

type span struct{ Start, End int }

func (span) Len() int { return 0 }

func handler(c *fiber.Ctx) {
	r, _ := c.Range(10)
	s := span{1, 2}
	spans := []span{s}
	fmt.Println(s.Start, spans[0].End, int(r.Ranges[0].Start), s.Len())
}
`,
			msg: "Migrating context method renames",
		},
		{
			name: "conversions between a range and a struct with the same fields",
			in: `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

type span struct{ Start, End int }

type tagged struct {
	Start int §json:"start"§
	End   int §json:"end"§
}

func pick(r fiber.Range, i int) fiber.RangeSet { return r.Ranges[i] }

func handler(c *fiber.Ctx) {
	r, _ := c.Range(10)
	a := span(r.Ranges[0])
	b := span(pick(r, 1))
	d := tagged(r.Ranges[1])
	back := fiber.RangeSet(a)
	again := fiber.RangeSet(span{1, 2})
	fmt.Println(a, b, d, back, again)
}
`,
			want: `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

type span struct{ Start, End int }

type tagged struct {
	Start int §json:"start"§
	End   int §json:"end"§
}

func pick(r fiber.Range, i int) fiber.RangeSet { return r.Ranges[i] }

func handler(c *fiber.Ctx) {
	r, _ := c.Range(10)
	a := span{Start: int(r.Ranges[0].Start), End: int(r.Ranges[0].End)}
	b := func(v fiber.RangeSet) span {
		return span{Start: int(v.Start), End: int(v.End)}
	}(pick(r, 1))
	d := tagged{Start: int(r.Ranges[1].Start), End: int(r.Ranges[1].End)}
	back := fiber.RangeSet{Start: int64(a.Start), End: int64(a.End)}
	again := fiber.RangeSet{Start: int64(span{1, 2}.Start), End: int64(span{1, 2}.End)}
	fmt.Println(a, b, d, back, again)
}
`,
			msg: "Migrating context method renames",
		},
		{
			name: "ranges handed to a function whose parameter is declared are followed",
			in: `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func total(rs []fiber.RangeSet) (n int) {
	for _, r := range rs {
		n += r.End - r.Start
	}
	return n
}

func describe(r fiber.Range, extra ...fiber.RangeSet) string {
	return fmt.Sprint(r.Type, r.Ranges[0].Start, len(extra))
}

func handler(c *fiber.Ctx) string {
	r, _ := c.Range(10)
	return describe(r, r.Ranges...) + fmt.Sprint(total(r.Ranges), total(nil))
}
`,
			want: `package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func total(rs []fiber.RangeSet) (n int) {
	for _, r := range rs {
		n += int(r.End) - int(r.Start)
	}
	return n
}

func describe(r fiber.Range, extra ...fiber.RangeSet) string {
	return fmt.Sprint(r.Type, int(r.Ranges[0].Start), len(extra))
}

func handler(c *fiber.Ctx) string {
	r, _ := c.Range(10)
	return describe(r, r.Ranges...) + fmt.Sprint(total(r.Ranges), total(nil))
}
`,
			msg: "Migrating context method renames",
		},
	})
}
