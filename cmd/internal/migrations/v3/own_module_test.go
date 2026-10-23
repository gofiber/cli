package v3_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// A library that is migrated in its own repository, or a fork of a contrib module, imports its own
// internal packages from the path of its module, which is the old path of a module that moved. An
// internal package can only be imported by the module that has it, and the module it moved to has
// none, so the import is the code of the project: it stays, in the migration and in the review,
// while the imports of the modules the project uses move.
func Test_MigrateContribPackages_InternalPackagesOfTheModuleItself(t *testing.T) {
	contribVersions(t, map[string]string{"session": "v3.1.0"})
	dir := t.TempDir()

	contribWriteFile(t, dir, "go.mod", `module github.com/gofiber/contrib/otelfiber/v2

go 1.22

require (
	github.com/gofiber/contrib/session v1.2.3
	github.com/gofiber/fiber/v2 v2.0.0
)
`)
	lib := contribWriteFile(t, dir, "fiber.go", `package otelfiber

import (
	"github.com/gofiber/contrib/otelfiber/v2/internal"
	"github.com/gofiber/contrib/session"
	"github.com/gofiber/fiber/v2"
)

func Middleware() fiber.Handler { return internal.Handler(session.NewStore) }
`)
	contribWriteFile(t, dir, "internal/internal.go", `package internal

import "github.com/gofiber/fiber/v2"

func Handler(any) fiber.Handler { return nil }
`)

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateContribPackages(newCmd(&buf), dir, nil, nil))

	out := readFile(t, lib)
	assert.Contains(t, out, `"github.com/gofiber/contrib/otelfiber/v2/internal"`, "an internal package of the module stays")
	assert.Contains(t, out, `"github.com/gofiber/contrib/v3/session"`, "a module that moved is imported from where it moved to")
	assert.NotContains(t, out, `"github.com/gofiber/contrib/session"`)

	// the review does not take the packages of the project for modules that were not migrated
	v3.Review(dir)
	var notes []string
	for _, n := range internal.DrainNotes(dir) {
		notes = append(notes, n.String())
	}
	assert.NotContains(t, strings.Join(notes, "\n"), "otelfiber/v2", "%v", notes)
}

// The internal packages of another module are none that a project can import, so whatever has the
// name of one is the project's own: only the path is looked at.
func Test_MigrateContribPackages_OtherImportsOfTheModulesStillMove(t *testing.T) {
	contribVersions(t, map[string]string{"otel": "v1.3.1"})
	dir := t.TempDir()

	contribWriteFile(t, dir, "go.mod", "module example.com/app\n\ngo 1.22\n\nrequire github.com/gofiber/contrib/otelfiber/v2 v2.2.3\n")
	app := contribWriteFile(t, dir, "main.go", `package main

import "github.com/gofiber/contrib/otelfiber/v2"

var _ = otelfiber.Middleware
`)

	var buf bytes.Buffer
	require.NoError(t, v3.MigrateContribPackages(newCmd(&buf), dir, nil, nil))

	assert.Contains(t, readFile(t, app), `otelfiber "github.com/gofiber/contrib/v3/otel"`)
}
