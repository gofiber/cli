package v3_test

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
)

// mw3Migration is the signature of every migration.
type mw3Migration = func(cmd *cobra.Command, cwd string, curr, target *semver.Version) error

// mw3Result is what a migration did to a project.
type mw3Result struct {
	// files holds the content of every file of the project after the run, the
	// ones the migration added included.
	files map[string]string
	// out is what the migration printed.
	out string
	// notes are what it left for the user to review.
	notes []internal.Note
}

// mw3Run writes the files into a new directory, runs the migrations on it in
// order and returns the outcome.
func mw3Run(t *testing.T, files map[string]string, migrations ...mw3Migration) mw3Result {
	t.Helper()

	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	for _, migrate := range migrations {
		require.NoError(t, migrate(cmd, dir, nil, nil))
	}

	res := mw3Result{files: map[string]string{}, out: buf.String(), notes: internal.DrainNotes(dir)}
	require.NoError(t, filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return fmt.Errorf("relative path of %s: %w", path, err)
		}
		res.files[filepath.ToSlash(rel)] = readFile(t, path)
		return nil
	}))
	return res
}

// mw3RunOne is mw3Run for a project that is a single main.go. It returns the
// file as it is after the run.
func mw3RunOne(t *testing.T, src string, migrations ...mw3Migration) (string, mw3Result) {
	t.Helper()

	res := mw3Run(t, map[string]string{"main.go": src}, migrations...)
	return res.files["main.go"], res
}

// mw3Messages returns the text of the notes, to compare as a list.
func mw3Messages(notes []internal.Note) []string {
	msgs := make([]string, 0, len(notes))
	for _, n := range notes {
		msgs = append(msgs, n.Message)
	}
	return msgs
}
