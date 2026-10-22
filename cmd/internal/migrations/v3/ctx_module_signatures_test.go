package v3_test

import (
	"testing"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateHandlerSignatures_AliasesOfTheContext(t *testing.T) {
	t.Parallel()

	xpkgRunCases(t, []xpkgCase{
		{
			name:    "the alias of the package, of another package, and an alias of it",
			migrate: v3.MigrateHandlerSignatures,
			files: map[string]string{
				"internal/web/more.go": `package web

// Ctx2 is an alias of the alias.
type Ctx2 = Ctx
`,
				"internal/other/other.go": `package other

import "example.com/app/internal/web"

// Ctx3 is an alias of the alias of another package.
type Ctx3 = web.Ctx2
`,
				xpkgHandlers: `package handlers

import (
	"example.com/app/internal/other"
	"example.com/app/internal/web"
)

func one(c *web.Ctx) error                                 { return nil }
func two(c *web.Ctx2, d []*web.Ctx) error                 { return nil }
func three(c *other.Ctx3, e map[string]*other.Ctx3) error { return nil }
func four(c *web.Context, p *web.Plain) error             { return nil }

type handler func(*web.Ctx) error
`,
			},
			want: map[string]string{xpkgHandlers: `package handlers

import (
	"example.com/app/internal/other"
	"example.com/app/internal/web"
)

func one(c web.Ctx) error                               { return nil }
func two(c web.Ctx2, d []web.Ctx) error                 { return nil }
func three(c other.Ctx3, e map[string]other.Ctx3) error { return nil }
func four(c *web.Context, p *web.Plain) error           { return nil }

type handler func(web.Ctx) error
`},
			msg: "Migrating handler signatures",
		},
		{
			name:    "an alias of the package that sits in another file, and one under a dot import",
			migrate: v3.MigrateHandlerSignatures,
			files: map[string]string{
				"internal/handlers/types.go": `package handlers

import f "github.com/gofiber/fiber/v2"

// Ctx is the context of the package.
type Ctx = f.Ctx
`,
				xpkgHandlers: `package handlers

func one(c *Ctx) error { return nil }
`,
				"internal/handlers/dot.go": `package handlers

import . "example.com/app/internal/web"

func two(c *Ctx) error { return nil }
`,
			},
			want: map[string]string{
				"internal/handlers/types.go": `package handlers

import f "github.com/gofiber/fiber/v2"

// Ctx is the context of the package.
type Ctx = f.Ctx
`,
				xpkgHandlers: `package handlers

func one(c Ctx) error { return nil }
`,
			},
			msg: "Migrating handler signatures",
		},
		{
			name:    "a pointer to a type that is no alias of the context",
			migrate: v3.MigrateHandlerSignatures,
			files: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

type Ctx = web.Plain

func one(c *Ctx, p *web.Plain, h *web.Hider) error { return nil }

func shadowed() {
	type Ctx struct{}
	var c *Ctx
	_ = c
}
`},
			want: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

type Ctx = web.Plain

func one(c *Ctx, p *web.Plain, h *web.Hider) error { return nil }

func shadowed() {
	type Ctx struct{}
	var c *Ctx
	_ = c
}
`},
		},
		{
			name:    "a local type or a type parameter that hides the alias",
			migrate: v3.MigrateHandlerSignatures,
			files: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

type Ctx = web.Ctx

func local() {
	type Ctx struct{}
	var c *Ctx
	_ = c
}

func generic[Ctx any](c *Ctx) {}

type box[Ctx any] struct{ c *Ctx }

func (b *box[Ctx]) get() *Ctx { return b.c }

func real(c *Ctx) error { return nil }
`},
			want: map[string]string{xpkgHandlers: `package handlers

import "example.com/app/internal/web"

type Ctx = web.Ctx

func local() {
	type Ctx struct{}
	var c *Ctx
	_ = c
}

func generic[Ctx any](c *Ctx) {}

type box[Ctx any] struct{ c *Ctx }

func (b *box[Ctx]) get() *Ctx { return b.c }

func real(c Ctx) error { return nil }
`},
			msg: "Migrating handler signatures",
		},
		{
			name:    "a nil pointer conversion keeps the pointer",
			migrate: v3.MigrateHandlerSignatures,
			files: map[string]string{xpkgHandlers: `package handlers

import (
	"reflect"

	"example.com/app/internal/web"
)

var (
	typ  = reflect.TypeOf((*web.Ctx)(nil))
	typ2 = reflect.TypeOf((*web.Ctx)(nil)).Elem()
	none = (*web.Ctx)(nil)
)
`},
			want: map[string]string{xpkgHandlers: `package handlers

import (
	"reflect"

	"example.com/app/internal/web"
)

var (
	typ  = reflect.TypeOf((*web.Ctx)(nil)).Elem()
	typ2 = reflect.TypeOf((*web.Ctx)(nil)).Elem()
	none = (*web.Ctx)(nil)
)
`},
			notes: []string{"(*web.Ctx)(nil) is a nil pointer to the Ctx interface now"},
			msg:   "Migrating handler signatures",
		},
	})
}
