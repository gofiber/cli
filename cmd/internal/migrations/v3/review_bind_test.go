package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bindNotes returns the notes of the review about the texts of the binder errors.
func bindNotes(t *testing.T, files map[string]string) []string {
	t.Helper()
	return reviewNotesWith(reviewNotes(t, files), "return a *fiber.BindError")
}

func Test_Review_BinderErrorTexts(t *testing.T) {
	t.Parallel()

	for _, method := range []string{"Body", "Query", "Header", "Cookie", "URI"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()

			notes := bindNotes(t, map[string]string{"main.go": `package main

import "github.com/gofiber/fiber/v3"

type in struct{ Page int ` + "`query:\"page\"`" + ` }

func handler(c fiber.Ctx) error {
	var v in
	if err := c.Bind().` + method + `(&v); err != nil {
		return err
	}
	return nil
}
`})

			require.Len(t, notes, 1, "%v", notes)
			assert.Contains(t, notes[0], "main.go:9: Bind().Body, Query, Header, Cookie and URI return a *fiber.BindError")
			assert.Contains(t, notes[0], `its text reads 'bind "page" from query: schema: error converting value for "page"'`)
			assert.Contains(t, notes[0], `where v2 returned 'failed to decode: schema: ...'`)
		})
	}
}

func Test_Review_BinderErrorTextsOncePerProjectAndOnlyForContexts(t *testing.T) {
	t.Parallel()

	notes := bindNotes(t, map[string]string{
		"a.go": `package main

import f "github.com/gofiber/fiber/v3"

type wrapper struct{ f.Ctx }

func first(c wrapper) error {
	var v struct{}
	return c.Bind().Query(&v)
}

func second(c f.Ctx) error {
	var v struct{}
	return c.Bind().Body(&v)
}
`,
		"b.go": `package main

import "github.com/gofiber/fiber/v3"

func third(c fiber.Ctx) error {
	var v struct{}
	return c.Bind().Header(&v)
}
`,
	})

	require.Len(t, notes, 1, "%v", notes)
	assert.Contains(t, notes[0], "a.go:9: ", "at the first use, in the file read first")
}

func Test_Review_BinderErrorTextsStaySilentElsewhere(t *testing.T) {
	t.Parallel()

	notes := bindNotes(t, map[string]string{"main.go": `package main

import "github.com/gofiber/fiber/v3"

type binder struct{}

func (binder) Body(any) error  { return nil }
func (binder) Query(any) error { return nil }

type other struct{}

func (other) Bind() binder { return binder{} }

func handler(c fiber.Ctx, o other) error {
	var v struct{}
	if err := o.Bind().Body(&v); err != nil {
		return err
	}
	// JSON and the other body binders have no v2 parser to compare with
	_ = c.Bind().JSON(&v)
	_ = c.Bind().Custom("x", &v)
	return binder{}.Query(&v)
}
`})

	assert.Empty(t, notes)
}
