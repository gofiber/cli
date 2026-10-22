package v3_test

import (
	"bytes"
	"os"
	"path/filepath"
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
	// file is the name of the file, main.go when empty.
	file string
	// others are more files of the project, by name; they are migrated too, but
	// only file is looked at.
	others map[string]string
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

// ctxRun runs the migration on a project that has one file, main.go unless a
// name is given, and returns the file afterwards, what the migration printed and
// its notes.
func ctxRun(t *testing.T, migrate ctxMigrateFn, name, in string, others map[string]string) (out, printed string, notes []internal.Note) {
	t.Helper()

	if name == "" {
		name = "main.go"
	}
	dir := t.TempDir()
	file := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(file, []byte(in), 0o600))
	for other, src := range others {
		require.NoError(t, os.WriteFile(filepath.Join(dir, other), []byte(ctxSrc(src)), 0o600))
	}

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
			out, printed, notes := ctxRun(t, migrate, tc.file, in, tc.others)

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

			again, printedAgain, _ := ctxRun(t, migrate, tc.file, out, tc.others)
			assert.Equal(t, out, again, "the second run changed the file")
			assert.Empty(t, printedAgain, "the second run printed")
		})
	}
}
