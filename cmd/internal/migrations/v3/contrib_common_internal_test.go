package v3

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// replaceField (common.go) is no longer used by a migration since the paseto
// one works on the syntax tree; its behavior stays pinned until it is removed.
func Test_replaceField(t *testing.T) {
	t.Parallel()

	src := "Config{\n\tTokenLookup: [2]string{\"header\", \"Authorization\"}, // where\n\tTokenPrefix: \"Bearer\",\n}"

	got := replaceField(src, "TokenLookup", func(indent, val, comma, comment, newline string) string {
		return FormatFieldWithComment(indent, "Extractor", "x("+val+")", comma, comment, newline)
	})

	assert.Equal(t, "Config{\n\tExtractor: x([2]string{\"header\", \"Authorization\"}), // where\n\tTokenPrefix: \"Bearer\",\n}", got)
}

// A rewrite that moves every use of a contrib package away leaves its import
// behind, and goimports does not always remove it: with an empty module cache it
// keeps the imports of a file that refers to a name it cannot place.
func Test_contribDropOrphans(t *testing.T) {
	t.Parallel()

	const (
		beforeBody = "func home(c *fiber.Ctx) error {\n\tmsg, err := fiberi18n.Localize(c, \"welcome\")\n\treturn c.SendString(msg)\n}\n"
		afterBody  = "func home(c fiber.Ctx) error {\n\tmsg, err := translator.Localize(c, \"welcome\")\n\treturn c.SendString(msg)\n}\n"
	)

	tests := []struct {
		name   string
		before string
		after  string
		want   string
	}{
		{
			name: "one import of a block",
			before: "package main\n\nimport (\n\tfiberi18n \"github.com/gofiber/contrib/v3/i18n\"\n\t\"github.com/gofiber/fiber/v2\"\n)\n\n" +
				beforeBody,
			after: "package main\n\nimport (\n\tfiberi18n \"github.com/gofiber/contrib/v3/i18n\"\n\t\"github.com/gofiber/fiber/v3\"\n)\n\n" +
				afterBody,
			want: "package main\n\nimport (\n\t\"github.com/gofiber/fiber/v3\"\n)\n\n" + afterBody,
		},
		{
			name: "the only import of a block",
			before: "package main\n\nimport (\n\tfiberi18n \"github.com/gofiber/contrib/v3/i18n\"\n)\n\n" +
				"func home() { fiberi18n.Localize(nil, \"welcome\") }\n",
			after: "package main\n\nimport (\n\tfiberi18n \"github.com/gofiber/contrib/v3/i18n\"\n)\n\n" +
				"func home() { translator.Localize(nil, \"welcome\") }\n",
			want: "package main\n\n\nfunc home() { translator.Localize(nil, \"welcome\") }\n",
		},
		{
			name:   "a single import",
			before: "package main\n\nimport fiberi18n \"github.com/gofiber/contrib/v3/i18n\"\n\nfunc home() { fiberi18n.Localize(nil, \"welcome\") }\n",
			after:  "package main\n\nimport fiberi18n \"github.com/gofiber/contrib/v3/i18n\"\n\nfunc home() { translator.Localize(nil, \"welcome\") }\n",
			want:   "package main\n\n\nfunc home() { translator.Localize(nil, \"welcome\") }\n",
		},
		{
			name: "an import with a comment after it",
			before: "package main\n\nimport (\n\tfiberi18n \"github.com/gofiber/contrib/v3/i18n\" // messages\n\t\"github.com/gofiber/fiber/v2\"\n)\n\n" +
				beforeBody,
			after: "package main\n\nimport (\n\tfiberi18n \"github.com/gofiber/contrib/v3/i18n\" // messages\n\t\"github.com/gofiber/fiber/v3\"\n)\n\n" +
				afterBody,
			want: "package main\n\nimport (\n\t\"github.com/gofiber/fiber/v3\"\n)\n\n" + afterBody,
		},
		{
			name: "an import that is still used",
			before: "package main\n\nimport fiberi18n \"github.com/gofiber/contrib/v3/i18n\"\n\n" +
				"var translator *fiberi18n.I18n\n\nfunc home() { fiberi18n.Localize(nil, \"welcome\") }\n",
			after: "package main\n\nimport fiberi18n \"github.com/gofiber/contrib/v3/i18n\"\n\n" +
				"var translator *fiberi18n.I18n\n\nfunc home() { translator.Localize(nil, \"welcome\") }\n",
			want: "package main\n\nimport fiberi18n \"github.com/gofiber/contrib/v3/i18n\"\n\n" +
				"var translator *fiberi18n.I18n\n\nfunc home() { translator.Localize(nil, \"welcome\") }\n",
		},
		{
			name:   "an import that the file did not use to begin with",
			before: "package main\n\nimport fiberi18n \"github.com/gofiber/contrib/v3/i18n\"\n\nvar _ = 1\n",
			after:  "package main\n\nimport fiberi18n \"github.com/gofiber/contrib/v3/i18n\"\n\nvar _ = 2\n",
			want:   "package main\n\nimport fiberi18n \"github.com/gofiber/contrib/v3/i18n\"\n\nvar _ = 2\n",
		},
		{
			name: "the import of another module",
			before: "package main\n\nimport (\n\tjwtware \"github.com/gofiber/contrib/v3/jwt\"\n)\n\n" +
				"func home() { jwtware.New() }\n",
			after: "package main\n\nimport (\n\tjwtware \"github.com/gofiber/contrib/v3/jwt\"\n)\n\n" +
				"func home() { other.New() }\n",
			want: "package main\n\nimport (\n\tjwtware \"github.com/gofiber/contrib/v3/jwt\"\n)\n\n" +
				"func home() { other.New() }\n",
		},
		{
			name: "a name other than the default",
			before: "package main\n\nimport (\n\ti18nmw \"github.com/gofiber/contrib/v3/i18n\"\n\t\"github.com/gofiber/fiber/v2\"\n)\n\n" +
				"func home(c *fiber.Ctx) error { _, err := i18nmw.Localize(c, \"welcome\"); return err }\n",
			after: "package main\n\nimport (\n\ti18nmw \"github.com/gofiber/contrib/v3/i18n\"\n\t\"github.com/gofiber/fiber/v3\"\n)\n\n" +
				"func home(c fiber.Ctx) error { _, err := translator.Localize(c, \"welcome\"); return err }\n",
			want: "package main\n\nimport (\n\t\"github.com/gofiber/fiber/v3\"\n)\n\n" +
				"func home(c fiber.Ctx) error { _, err := translator.Localize(c, \"welcome\"); return err }\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			before, ok := parseSource(tt.before)
			require.True(t, ok)

			got := contribDropOrphans(before, tt.after, "i18n")

			assert.Equal(t, tt.want, got)
			_, ok = parseSource(got)
			assert.True(t, ok, "the result parses")
		})
	}
}

func Test_contribDropOrphans_UnparsableResult(t *testing.T) {
	t.Parallel()

	before, ok := parseSource("package main\n\nimport fiberi18n \"github.com/gofiber/contrib/v3/i18n\"\n\nvar _ = fiberi18n.New\n")
	require.True(t, ok)

	broken := "package main\n\nimport fiberi18n \"github.com/gofiber/contrib/v3/i18n\"\n\nvar _ = \n"
	assert.Equal(t, broken, contribDropOrphans(before, broken, "i18n"))
}
