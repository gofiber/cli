package v3_test

import (
	"testing"

	v3 "github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// The types of the other files of a package tell what the receiver of a call is.
// Only the files that are part of the package count.
func Test_ctxSiblings(t *testing.T) {
	t.Parallel()

	const (
		in = `package main

import "github.com/gofiber/fiber/v2"

func handler(s *session) error {
	return s.c.Redirect("/x", fiber.StatusFound)
}
`
		want = `package main

import "github.com/gofiber/fiber/v2"

func handler(s *session) error {
	return s.c.Redirect().Status(fiber.StatusFound).To("/x")
}
`
		declaration = `
import "github.com/gofiber/fiber/v2"

type session struct{ c *fiber.Ctx }
`
	)

	ctxRunCases(t, v3.MigrateRedirectMethods, []ctxCase{
		{
			name:   "a file of the package",
			others: map[string]string{"session.go": "package main\n" + declaration},
			in:     in,
			want:   want,
			msg:    "Migrating redirect methods",
		},
		{
			name:   "a file with another build constraint",
			others: map[string]string{"session.go": "//go:build linux\n\npackage main\n" + declaration},
			in:     in,
			want:   want,
			msg:    "Migrating redirect methods",
		},
		{
			name:   "a file that is excluded from the build",
			others: map[string]string{"session.go": "//go:build ignore\n\npackage main\n" + declaration},
			in:     in,
		},
		{
			name:   "a file of another package",
			others: map[string]string{"session.go": "package other\n" + declaration},
			in:     in,
		},
		{
			name:   "a file that does not parse",
			others: map[string]string{"session.go": "package main\n" + declaration + "\nfunc (\n"},
			in:     in,
		},
		{
			name:   "a file that is not Go",
			others: map[string]string{"notes.txt": "package main\n" + declaration},
			in:     in,
		},
	})
}
