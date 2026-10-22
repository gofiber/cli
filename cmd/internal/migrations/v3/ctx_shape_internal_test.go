package v3

import (
	"go/ast"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
)

// ctxShapesOf returns, for every call of probe in src, the shape the tracker
// infers for its argument, keyed by the text of the argument.
func ctxShapesOf(t *testing.T, src string) map[string]string {
	t.Helper()

	env, g := ctxEnvOf(t, src)
	tracker := newCtxRangeTracker(&ctxPlan{}, newCtxNoter(internal.FileInfo{Root: ".", Path: "main.go", Rel: "main.go"}, g, newCtxRun()), env)
	got := map[string]string{}
	g.walk(func(n ast.Node, stack []ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 || !isIdentNamed(call.Fun, "probe") {
			return true
		}
		got[g.text(call.Args[0])] = tracker.shapeOf(call.Args[0], stack).String()
		return true
	})
	return got
}

func Test_ctxShapes(t *testing.T) {
	t.Parallel()

	const header = `package main

import (
	"slices"
	"maps"

	"github.com/gofiber/fiber/v2"
)

`

	tests := []struct {
		want map[string]string
		name string
		src  string
	}{
		{
			name: "the result of Range and what is made of it",
			src: header + `func handler(c *fiber.Ctx) {
	r, err := c.Range(10)
	probe(r)
	probe(err)
	probe(r.Ranges)
	probe(r.Ranges[0])
	probe(r.Ranges[0].Start)
	probe(r.Ranges[1:])
	for i, rs := range r.Ranges {
		probe(i)
		probe(rs)
	}
	for _, rs := range r.Ranges[2:] {
		probe(rs)
	}
}
`,
			want: map[string]string{
				"r": "range", "err": "none", "r.Ranges": "[]set", "r.Ranges[0]": "set", "r.Ranges[0].Start": "none", "r.Ranges[1:]": "[]set",
				"i": "none", "rs": "set",
			},
		},
		{
			name: "declared types: parameters, results, fields and variables",
			src: header + `type job struct {
	ranges []fiber.RangeSet
	all    fiber.Range
	byName map[string]fiber.RangeSet
	feed   chan fiber.RangeSet
	parse  func(int) (fiber.Range, error)
	extra  any
	n      int
}

func handler(j *job, r fiber.Range, rs ...fiber.RangeSet) (fiber.RangeSet, error) {
	var local []fiber.Range
	probe(j.ranges)
	probe(j.all)
	probe(j.all.Ranges)
	probe(j.byName)
	probe(j.byName["a"])
	probe(j.feed)
	probe(<-j.feed)
	probe(j.parse)
	probe(j.extra)
	probe(j.n)
	probe(r)
	probe(rs)
	probe(rs[0])
	probe(local)
	probe(local[0].Ranges)
	return fiber.RangeSet{}, nil
}
`,
			want: map[string]string{
				"j.ranges": "[]set", "j.all": "range", "j.all.Ranges": "[]set", "j.byName": "map[none]set", `j.byName["a"]`: "set",
				"j.feed": "chan set", "<-j.feed": "set", "j.parse": "func(none) (range, none)", "j.extra": "none", "j.n": "none",
				"r": "range", "rs": "[]set", "rs[0]": "set", "local": "[]range", "local[0].Ranges": "[]set",
			},
		},
		{
			name: "maps, channels, function values and interfaces",
			src: header + `type parser interface {
	Parse(c *fiber.Ctx) (fiber.Range, error)
}

func handler(c *fiber.Ctx, p parser, v any) {
	byName := map[string]fiber.RangeSet{}
	byName["a"] = fiber.RangeSet{}
	for k, rs := range byName {
		probe(k)
		probe(rs)
	}
	ch := make(chan fiber.Range, 2)
	r, ok := <-ch
	probe(r)
	probe(ok)
	for r := range ch {
		probe(r)
	}
	select {
	case rs := <-make(chan fiber.RangeSet):
		probe(rs)
	}

	rangeOf := c.Range
	probe(rangeOf)
	got, _ := rangeOf(5)
	probe(got)
	probe(rangeOf(5))
	parse := func() fiber.RangeSet { return fiber.RangeSet{} }
	probe(parse())
	probe(func() []fiber.Range { return nil }())

	pr, _ := p.Parse(c)
	probe(pr)
	probe(p.Parse)

	probe(v.(fiber.Range))
	if x, ok := v.(fiber.RangeSet); ok {
		probe(x)
	}
	switch s := v.(type) {
	case fiber.Range:
		probe(s)
	case fiber.RangeSet, int:
		probe(s)
	default:
		probe(s)
	}
}
`,
			want: map[string]string{
				"k": "none", "rs": "set", "r": "range", "ok": "none", "rangeOf": "func(none) (range, none)", "got": "range",
				"rangeOf(5)": "range", "parse()": "set", "func() []fiber.Range { return nil }()": "[]range",
				"pr": "range", "p.Parse": "func(none) (range, none)", "v.(fiber.Range)": "range", "x": "set", "s": "none",
			},
		},
		{
			name: "generic types and functions",
			src: header + `type box[T any] struct{ v T }

func (b *box[T]) get() T       { return b.v }
func (b *box[T]) all() []T     { return []T{b.v} }
func (b *box[T]) set(v T)      { b.v = v }

type pair[K comparable, V any] struct {
	key K
	val V
}

func first[T any](xs []T) T { return xs[0] }
func wrap[T any](v T) *box[T] { return &box[T]{v} }
func keys[K comparable, V any](m map[K]V) []K { return nil }

func handler(c *fiber.Ctx) {
	r, _ := c.Range(5)
	b := box[fiber.Range]{}
	probe(b)
	probe(b.v)
	probe(b.get())
	probe(b.all())
	p := &pair[string, fiber.RangeSet]{}
	probe(p.key)
	probe(p.val)
	probe(first(r.Ranges))
	probe(first[fiber.Range]([]fiber.Range{r}))
	probe(wrap(r).get())
	probe(wrap(r.Ranges[0]).v)
	probe(keys(map[fiber.RangeSet]string{}))
	probe(box[int]{}.get())
	probe(slices.Clone(r.Ranges))
	probe(slices.Max([]fiber.Range{r}))
	for i, rs := range slices.All(r.Ranges) {
		probe(i)
		probe(rs)
	}
	for _, v := range maps.All(map[string]fiber.Range{}) {
		probe(v)
	}
}
`,
			want: map[string]string{
				"b": "struct", "b.v": "range", "b.get()": "range", "b.all()": "[]range", "p.key": "none", "p.val": "set",
				"first(r.Ranges)": "set", "first[fiber.Range]([]fiber.Range{r})": "range", "wrap(r).get()": "range", "wrap(r.Ranges[0]).v": "set",
				"keys(map[fiber.RangeSet]string{})": "[]set", "box[int]{}.get()": "none", "slices.Clone(r.Ranges)": "[]set",
				"slices.Max([]fiber.Range{r})": "range", "i": "none", "rs": "set", "v": "range",
			},
		},
		{
			name: "composite literals and conversions",
			src: header + `type spans []fiber.RangeSet

type wrapped struct {
	fiber.Range
	note string
}

func handler() {
	probe([]fiber.RangeSet{{Start: 1}, {2, 3}})
	probe([]fiber.RangeSet{{Start: 1}, {2, 3}}[0])
	probe(map[string]fiber.Range{"a": {Type: "bytes"}}["a"])
	probe(map[string]fiber.Range{"a": {Type: "bytes"}})
	probe(spans{{1, 2}})
	probe(spans{{1, 2}}[0])
	probe(fiber.Range(wrapped{}.Range))
	probe(wrapped{}.Ranges)
	probe(wrapped{}.note)
	probe(&fiber.RangeSet{})
	probe(new(fiber.Range))
	probe(make([]fiber.RangeSet, 3))
	probe(append([]fiber.RangeSet(nil), fiber.RangeSet{}))
	probe(len([]fiber.RangeSet{}))
	probe(int(5))
}
`,
			want: map[string]string{
				"[]fiber.RangeSet{{Start: 1}, {2, 3}}": "[]set", "[]fiber.RangeSet{{Start: 1}, {2, 3}}[0]": "set",
				`map[string]fiber.Range{"a": {Type: "bytes"}}["a"]`: "range", `map[string]fiber.Range{"a": {Type: "bytes"}}`: "map[none]range",
				"spans{{1, 2}}": "[]set", "spans{{1, 2}}[0]": "set", "fiber.Range(wrapped{}.Range)": "range", "wrapped{}.Ranges": "[]set",
				"wrapped{}.note": "none", "&fiber.RangeSet{}": "set", "new(fiber.Range)": "range", "make([]fiber.RangeSet, 3)": "[]set",
				"append([]fiber.RangeSet(nil), fiber.RangeSet{})": "[]set", "len([]fiber.RangeSet{})": "none", "int(5)": "none",
			},
		},
		{
			name: "package-level variables, shadowing and unknown names",
			src: header + `var cache = map[string]fiber.Range{}
var spans []fiber.RangeSet

func lookup() fiber.Range { return fiber.Range{} }

func handler(other unknown) {
	probe(cache["a"])
	probe(spans[0])
	probe(lookup())
	probe(lookup().Ranges[0])
	probe(other)
	probe(other.thing)
	probe(missing())
	cache := 5
	probe(cache)
}
`,
			want: map[string]string{
				`cache["a"]`: "range", "spans[0]": "set", "lookup()": "range", "lookup().Ranges[0]": "set",
				"other": "?", "other.thing": "?", "missing()": "?", "cache": "none",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := ctxShapesOf(t, tc.src)
			for expr, want := range tc.want {
				assert.Equal(t, want, got[expr], expr)
			}
			require.Len(t, got, len(tc.want), "the probes: %v", got)
		})
	}
}
