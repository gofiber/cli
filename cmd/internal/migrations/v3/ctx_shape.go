package v3

import (
	"go/ast"
	"slices"
	"strings"
)

// The ranges of a request are fiber.Range, with the ranges in a []fiber.RangeSet,
// and their Start and End are int in v2 and int64 in v3. The tracker has to find
// every read and write of a Start or an End, and there is no type checker to ask:
// a value can be a Range in a variable of a generic type, in a map, in a
// channel, behind a function value or an interface. A rshape says what the tracker
// knows of the type of a value as far as the ranges go, and is built from the type
// expressions of the code and from what the operations on a value give: the
// element of a slice, the value of a map, what a channel yields, what a function
// returns, what a generic type or function is instantiated with.

// rkind is the kind of a rshape.
type rkind uint8

const (
	// rkNone: a type that holds no range (numbers, strings, errors, interfaces, the
	// types of packages that know nothing of ranges).
	rkNone rkind = iota
	// rkRange: fiber.Range.
	rkRange
	// rkSet: fiber.RangeSet, one of the ranges.
	rkSet
	// rkSlice: a slice or an array; elem is the type of the elements.
	rkSlice
	// rkMap: a map; key and elem are the types of the keys and of the values.
	rkMap
	// rkChan: a channel; elem is what it carries.
	rkChan
	// rkFunc: a function; params and results are the types of its parameters and
	// results.
	rkFunc
	// rkStruct: a struct; fields are its declared fields.
	rkStruct
	// rkIface: an interface with methods; fields are its declared methods.
	rkIface
	// rkParam: a type parameter of a generic function or type that is not bound
	// yet; name is its name.
	rkParam
)

// rshape is what is known of a type. A nil *rshape is a type that is not known;
// rNone is a type that is known to hold no range.
type rshape struct {
	elem *rshape
	key  *rshape
	// subst binds the type parameters of the declaration that fields belongs to.
	subst map[string]*rshape
	// fields are the declared fields of a struct, or the methods of an interface.
	fields *ast.FieldList
	name   string
	// args are the type arguments of a declared generic type.
	args []*rshape
	// params and results are the parameters and results of a function.
	params  []*rshape
	results []*rshape
	// tparams are the names of the type parameters of a generic function.
	tparams []string
	kind    rkind
	// variadic is set for a function whose last parameter is a ...T, which the
	// shape records as a slice.
	variadic bool
}

// rNone is the shape of a type that holds no range.
var rNone = &rshape{kind: rkNone}

// rRange and rSet are the shapes of fiber.Range and fiber.RangeSet.
var (
	rRange = &rshape{kind: rkRange}
	rSet   = &rshape{kind: rkSet}
)

func rSliceOf(elem *rshape) *rshape { return &rshape{kind: rkSlice, elem: elem} }

func rMapOf(key, elem *rshape) *rshape { return &rshape{kind: rkMap, key: key, elem: elem} }

func rChanOf(elem *rshape) *rshape { return &rshape{kind: rkChan, elem: elem} }

func rFuncOf(params, results []*rshape) *rshape {
	return &rshape{kind: rkFunc, params: params, results: results}
}

// rMaxDepth bounds how far the tracker follows a type, so a type that contains
// itself can not make it loop.
const rMaxDepth = 12

// String spells the shape for the tests.
func (s *rshape) String() string {
	return s.spell(0)
}

func (s *rshape) spell(depth int) string {
	if s == nil {
		return "?"
	}
	if depth > 4 {
		return "..."
	}

	switch s.kind {
	case rkNone:
		return "none"
	case rkRange:
		return "range"
	case rkSet:
		return "set"
	case rkSlice:
		return "[]" + s.elem.spell(depth+1)
	case rkMap:
		return "map[" + s.key.spell(depth+1) + "]" + s.elem.spell(depth+1)
	case rkChan:
		return "chan " + s.elem.spell(depth+1)
	case rkFunc:
		parts := func(list []*rshape) string {
			out := make([]string, len(list))
			for i, p := range list {
				out[i] = p.spell(depth + 1)
			}
			return strings.Join(out, ", ")
		}
		return "func(" + parts(s.params) + ") (" + parts(s.results) + ")"
	case rkStruct:
		return "struct"
	case rkIface:
		return "interface"
	case rkParam:
		return "$" + s.name
	}
	return "?"
}

// holdsRange reports whether the shape is, or contains as its elements, a range:
// the values the tracker has to follow.
func (s *rshape) holdsRange() bool {
	return s.holdsRangeAt(0)
}

func (s *rshape) holdsRangeAt(depth int) bool {
	if s == nil || depth > 4 {
		return false
	}

	switch s.kind {
	case rkRange, rkSet:
		return true
	case rkSlice, rkChan:
		return s.elem.holdsRangeAt(depth + 1)
	case rkMap:
		return s.key.holdsRangeAt(depth+1) || s.elem.holdsRangeAt(depth+1)
	case rkFunc:
		return slices.ContainsFunc(s.results, func(r *rshape) bool { return r.holdsRangeAt(depth + 1) })
	case rkNone, rkStruct, rkIface, rkParam:
	}
	return false
}

// with returns a copy of the shape with the type parameters in bind replaced.
func (s *rshape) with(bind map[string]*rshape) *rshape {
	return s.withAt(bind, 0)
}

func (s *rshape) withAt(bind map[string]*rshape, depth int) *rshape {
	if s == nil || len(bind) == 0 || depth > rMaxDepth {
		return s
	}

	switch s.kind {
	case rkParam:
		if bound, ok := bind[s.name]; ok {
			return bound
		}
		return s
	case rkNone, rkRange, rkSet:
		return s
	case rkSlice, rkChan, rkMap, rkFunc, rkStruct, rkIface:
	}

	c := *s
	c.elem, c.key = s.elem.withAt(bind, depth+1), s.key.withAt(bind, depth+1)
	c.params, c.results = withAll(s.params, bind, depth+1), withAll(s.results, bind, depth+1)
	c.args = withAll(s.args, bind, depth+1)
	if len(s.subst) > 0 {
		c.subst = make(map[string]*rshape, len(s.subst))
		for name, bound := range s.subst {
			c.subst[name] = bound.withAt(bind, depth+1)
		}
	}
	return &c
}

func withAll(list []*rshape, bind map[string]*rshape, depth int) []*rshape {
	out := make([]*rshape, len(list))
	for i, s := range list {
		out[i] = s.withAt(bind, depth)
	}
	return out
}

// unify binds the type parameters that param mentions, from the shape arg of the
// value passed for it, the way the inference of a generic call does.
func unify(param, arg *rshape, bind map[string]*rshape, depth int) {
	if param == nil || depth > rMaxDepth {
		return
	}

	if param.kind == rkParam {
		if _, bound := bind[param.name]; !bound {
			bind[param.name] = arg
		}
		return
	}
	if arg == nil || param.kind != arg.kind {
		return
	}

	switch param.kind {
	case rkSlice, rkChan:
		unify(param.elem, arg.elem, bind, depth+1)
	case rkMap:
		unify(param.key, arg.key, bind, depth+1)
		unify(param.elem, arg.elem, bind, depth+1)
	case rkFunc:
		for i := range min(len(param.params), len(arg.params)) {
			unify(param.params[i], arg.params[i], bind, depth+1)
		}
		for i := range min(len(param.results), len(arg.results)) {
			unify(param.results[i], arg.results[i], bind, depth+1)
		}
	case rkStruct, rkIface, rkNone, rkRange, rkSet, rkParam:
	}

	if param.name != "" && param.name == arg.name {
		for i := range min(len(param.args), len(arg.args)) {
			unify(param.args[i], arg.args[i], bind, depth+1)
		}
	}
}
