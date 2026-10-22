package v3_test

import (
	"testing"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateParamsTagKeys_Shapes(t *testing.T) {
	t.Parallel()

	ctxRunCases(t, v3.MigrateParamsTagKeys, []ctxCase{
		{
			name: "params becomes uri",
			in: `package main

import "github.com/gofiber/fiber/v2"

type params struct {
	ID   string §params:"id" json:"id"§
	Slug string §json:"slug" params:"slug" validate:"required"§
}

func handler(c *fiber.Ctx) error {
	var p params
	return c.ParamsParser(&p)
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

type params struct {
	ID   string §uri:"id" json:"id"§
	Slug string §json:"slug" uri:"slug" validate:"required"§
}

func handler(c *fiber.Ctx) error {
	var p params
	return c.ParamsParser(&p)
}
`,
			msg: "Migrating struct tag keys of the parsers",
		},
		{
			name: "reqHeader becomes header",
			in: `package main

import "github.com/gofiber/fiber/v2"

type Headers struct {
	Token string §reqHeader:"X-Token"§
	ID    string §reqHeader:"X-Request-Id" json:"id"§
}

func handler(c *fiber.Ctx) error {
	var h Headers
	return c.ReqHeaderParser(&h)
}
`,
			want: `package main

import "github.com/gofiber/fiber/v2"

type Headers struct {
	Token string §header:"X-Token"§
	ID    string §header:"X-Request-Id" json:"id"§
}

func handler(c *fiber.Ctx) error {
	var h Headers
	return c.ReqHeaderParser(&h)
}
`,
			msg: "Migrating struct tag keys of the parsers",
		},
		{
			name: "interpreted string tags and nested structs",
			in: `package main

type Input struct {
	ID    string "params:\"id\""
	Inner struct {
		Agent string §reqHeader:"User-Agent"§
	}
	List []struct {
		Name string §query:"name" params:"name"§
	}
}
`,
			want: `package main

type Input struct {
	ID    string "uri:\"id\""
	Inner struct {
		Agent string §header:"User-Agent"§
	}
	List []struct {
		Name string §query:"name" uri:"name"§
	}
}
`,
			msg: "Migrating struct tag keys of the parsers",
		},
		{
			name: "only whole keys are renamed",
			in: `package main

type Input struct {
	A string §myparams:"a"§
	B string §xreqHeader:"b"§
	C string §json:"params:\"c\""§
	D string §reqHeaderX:"d" params2:"d"§
	E string §uri:"e" header:"E"§
}
`,
		},
		{
			name: "a tag that has the new key already is reported",
			in: `package main

type Input struct {
	A string §params:"a" uri:"a"§
	B string §reqHeader:"X-B" header:"X-C"§
}
`,
			notes: []string{"main.go:4: the struct tag has a \"params\" and a \"uri\" key", "main.go:5: the struct tag has a \"reqHeader\" and a \"header\" key"},
		},
		{
			name: "structs without any fiber import",
			in: `package models

type Params struct {
	ID string §params:"id"§
}

type Headers struct {
	Token string §reqHeader:"X-Token"§
}
`,
			want: `package models

type Params struct {
	ID string §uri:"id"§
}

type Headers struct {
	Token string §header:"X-Token"§
}
`,
			msg: "Migrating struct tag keys of the parsers",
		},
		{
			name: "nothing to rename",
			in: `package main

type Input struct {
	ID    string §uri:"id" json:"id"§
	Token string §header:"X-Token"§
	Plain string
}
`,
		},
	})
}
