package v3_test

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
)

// ctxMigrateFn is the signature of the migrations.
type ctxMigrateFn = func(cmd *cobra.Command, cwd string, curr, target *semver.Version) error

// ctxCase is one migration run on a single file.
type ctxCase struct {
	name string
	// in is the v2 source. § stands for a backquote, which a raw string literal
	// can not hold.
	in string
	// want is the complete migrated source, empty when the file stays as it is.
	want string
	// msg is what the migration prints, empty when it prints nothing.
	msg string
	// notes are substrings of the notes the run leaves, one for each of them.
	notes []string
}

// ctxSrc turns the § of a test source into backquotes.
func ctxSrc(s string) string {
	return strings.ReplaceAll(s, "§", "`")
}

// ctxRun runs the migration on a project that has one file, main.go, and
// returns the file afterwards, what the migration printed and its notes.
func ctxRun(t *testing.T, migrate ctxMigrateFn, in string) (out, printed string, notes []internal.Note) {
	t.Helper()

	dir := t.TempDir()
	file := writeTempFile(t, dir, in)

	var buf bytes.Buffer
	require.NoError(t, migrate(newCmd(&buf), dir, nil, nil))

	return readFile(t, file), buf.String(), internal.DrainNotes(dir)
}

// ctxRunCases runs the cases, and runs each migrated file through the migration
// once more, which must not change it.
func ctxRunCases(t *testing.T, migrate ctxMigrateFn, cases []ctxCase) {
	t.Helper()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			in := ctxSrc(tc.in)
			out, printed, notes := ctxRun(t, migrate, in)

			want := in
			if tc.want != "" {
				want = ctxSrc(tc.want)
			}
			assert.Equal(t, want, out)
			if tc.msg == "" {
				assert.Empty(t, printed)
			} else {
				assert.Contains(t, printed, tc.msg)
			}

			if assert.Len(t, notes, len(tc.notes), "notes: %v", notes) {
				for _, want := range tc.notes {
					assert.True(t, slices.ContainsFunc(notes, func(n internal.Note) bool {
						return strings.Contains(n.String(), want)
					}), "no note contains %q: %v", want, notes)
				}
			}

			again, printedAgain, _ := ctxRun(t, migrate, out)
			assert.Equal(t, out, again, "the second run changed the file")
			assert.Empty(t, printedAgain, "the second run printed")
		})
	}
}
