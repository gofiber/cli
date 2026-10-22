package v3

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The environment follows the type of a receiver through the results of calls,
// the values of range statements, interfaces, generic declarations and method
// values, and says "unknown" for what it can not follow.
func Test_ctxEnv_valueFlows(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	"iter"

	"github.com/gofiber/fiber/v2"
)

type plain struct{ n int }

type holder struct{ c *fiber.Ctx }

func (h *holder) get() *fiber.Ctx { return h.c }

type provider interface{ Ctx() *fiber.Ctx }

type provider2 interface{ provider }

type impl struct{ c *fiber.Ctx }

func (i impl) Ctx() *fiber.Ctx { return i.c }

type impl2 struct{ impl }

type ctxList []*fiber.Ctx

type ctxMap map[string]*fiber.Ctx

type box[T any] struct{ v T }

func (b box[T]) get() T { return b.v }

func (b *box[U]) ptr() *U { return &b.v }

type pair[K comparable, V any] struct {
	key K
	val V
}

func first[T any](xs []T) T                           { return xs[0] }
func ident[T any](v T) T                              { return v }
func wrap[T any](v T) *box[T]                         { return &box[T]{v} }
func keys[K comparable, V any](m map[K]V) []K         { return nil }
func pairOf[K comparable, V any](k K, v V) pair[K, V] { return pair[K, V]{k, v} }
func variadic[T any](xs ...T) T                       { return xs[0] }

func two(c *fiber.Ctx) (int, *fiber.Ctx)                { return 1, c }
func twoB(c *fiber.Ctx) (*fiber.Ctx, error)              { return c, nil }
func grouped(c *fiber.Ctx) (a, b int, d *fiber.Ctx)      { return 1, 2, c }
func seq(c *fiber.Ctx) iter.Seq[*fiber.Ctx]              { return nil }
func seq2(c *fiber.Ctx) iter.Seq2[string, *fiber.Ctx]    { return nil }
func each(c *fiber.Ctx) func(yield func(*fiber.Ctx) bool) { return nil }
func ctxs(c *fiber.Ctx) []*fiber.Ctx                     { return nil }

func handlers(c *fiber.Ctx, h *holder, p provider, p2 provider2, i2 impl2, list ctxList, byName ctxMap, other box[int]) {
	n, cc := two(c)
	cc.probe("second result")
	cc2, err := twoB(c)
	cc2.probe("first result")
	x, y, d := grouped(c)
	d.probe("third result of grouped names")
	var m, vm = two(c)
	vm.probe("var with several results")
	_, _, _, _, _, _ = n, err, x, y, m, vm

	v, ok := byName["a"]
	v.probe("map, comma ok")
	ch := make(chan *fiber.Ctx, 1)
	got, ok := <-ch
	got.probe("receive, comma ok")
	asserted, ok := any(c).(*fiber.Ctx)
	asserted.probe("assertion, comma ok")
	_ = ok

	for cc := range ch {
		cc.probe("range over a channel")
	}
	for _, cc := range list {
		cc.probe("range over a named slice")
	}
	for _, cc := range byName {
		cc.probe("range over a named map")
	}
	for cc := range seq(c) {
		cc.probe("range over iter.Seq")
	}
	for name, cc := range seq2(c) {
		cc.probe("value of iter.Seq2")
		name.probe("key of iter.Seq2")
	}
	for cc := range each(c) {
		cc.probe("range over an iterator function")
	}
	list[0].probe("index of a named slice")
	byName["a"].probe("index of a named map")
	(<-ch).probe("receive")

	p.Ctx().probe("method of an interface")
	p2.Ctx().probe("method of an embedded interface")
	i2.Ctx().probe("method that an embedded struct promotes")
	h.get().probe("method of a struct")
	get := h.get
	get().probe("method value")
	getter := (*holder).get
	getter(h).probe("method expression")

	b := box[*fiber.Ctx]{v: c}
	b.v.probe("field of a generic struct")
	b.get().probe("method of a generic struct")
	b.ptr().probe("method that names the type parameter differently")
	wrap(c).v.probe("field of the result of a generic function")
	wrap(c).get().probe("method of the result of a generic function")
	first([]*fiber.Ctx{c}).probe("type parameter inferred from a slice")
	first(ctxs(c)).probe("type parameter inferred from a call")
	first(list).probe("type parameter inferred from a named slice")
	ident(c).probe("type parameter inferred")
	ident[*fiber.Ctx](c).probe("type argument written out")
	variadic(c, c).probe("variadic type parameter")
	variadic(ctxs(c)...).probe("spread variadic type parameter")
	pairOf("a", c).val.probe("second of two type parameters")
	keys(map[*fiber.Ctx]string{})[0].probe("type parameter inferred from a map")
	b2 := box[box[*fiber.Ctx]]{}
	b2.v.v.probe("nested generic field")

	other.v.probe("generic type instantiated with a plain type")
	first([]plain{}).probe("generic function of another type")
	first(unknownThing).probe("generic function with an argument of no known type")
	unresolved.probe("undeclared name")
}
`
	want := map[string]ctxKind{
		`"second result"`:                                      ctxYes,
		`"first result"`:                                       ctxYes,
		`"third result of grouped names"`:                      ctxYes,
		`"var with several results"`:                           ctxYes,
		`"map, comma ok"`:                                      ctxYes,
		`"receive, comma ok"`:                                  ctxYes,
		`"assertion, comma ok"`:                                ctxYes,
		`"range over a channel"`:                               ctxYes,
		`"range over a named slice"`:                           ctxYes,
		`"range over a named map"`:                             ctxYes,
		`"range over iter.Seq"`:                                ctxYes,
		`"value of iter.Seq2"`:                                 ctxYes,
		`"key of iter.Seq2"`:                                   ctxNo,
		`"range over an iterator function"`:                    ctxYes,
		`"index of a named slice"`:                             ctxYes,
		`"index of a named map"`:                               ctxYes,
		`"receive"`:                                            ctxYes,
		`"method of an interface"`:                             ctxYes,
		`"method of an embedded interface"`:                    ctxYes,
		`"method that an embedded struct promotes"`:            ctxYes,
		`"method of a struct"`:                                 ctxYes,
		`"method value"`:                                       ctxYes,
		`"method expression"`:                                  ctxYes,
		`"field of a generic struct"`:                          ctxYes,
		`"method of a generic struct"`:                         ctxYes,
		`"method that names the type parameter differently"`:   ctxYes,
		`"field of the result of a generic function"`:          ctxYes,
		`"method of the result of a generic function"`:         ctxYes,
		`"type parameter inferred from a slice"`:               ctxYes,
		`"type parameter inferred from a call"`:                ctxYes,
		`"type parameter inferred from a named slice"`:         ctxYes,
		`"type parameter inferred"`:                            ctxYes,
		`"type argument written out"`:                          ctxYes,
		`"variadic type parameter"`:                            ctxYes,
		`"spread variadic type parameter"`:                     ctxYes,
		`"second of two type parameters"`:                      ctxYes,
		`"type parameter inferred from a map"`:                 ctxYes,
		`"nested generic field"`:                               ctxYes,
		`"generic type instantiated with a plain type"`:        ctxNo,
		`"generic function of another type"`:                   ctxNo,
		`"generic function with an argument of no known type"`: ctxUnknown,
		`"undeclared name"`:                                    ctxUnknown,
	}
	assert.Equal(t, want, ctxKindsOf(t, src))
}

// The variables of the package that another file declares are known too.
func Test_ctxEnv_packageVariables(t *testing.T) {
	t.Parallel()

	const other = `package main

import "github.com/gofiber/fiber/v2"

var global *fiber.Ctx

var inferred = newCtx()

var first, second = pairOf()

var plainValue = 5

func newCtx() *fiber.Ctx { return nil }

func pairOf() (int, *fiber.Ctx) { return 1, nil }
`
	const src = `package main

import "github.com/gofiber/fiber/v2"

func handler() {
	global.probe("declared with a type")
	inferred.probe("declared with a call")
	second.probe("second of several results")
	first.probe("first of several results")
	plainValue.probe("a number")
	missing.probe("not declared anywhere")
}
`
	sibling, ok := parseSource(other)
	require.True(t, ok)
	g, ok := parseSource(src)
	require.True(t, ok)

	names := fiberNames(src)
	env := newCtxEnv(g, names, ctxSibling{file: sibling.file, names: fiberNames(other)})
	got := ctxKindsWith(g, env)
	assert.Equal(t, map[string]ctxKind{
		`"declared with a type"`:      ctxYes,
		`"declared with a call"`:      ctxYes,
		`"second of several results"`: ctxYes,
		`"first of several results"`:  ctxNo,
		`"a number"`:                  ctxUnknown,
		`"not declared anywhere"`:     ctxUnknown,
	}, got)
}
