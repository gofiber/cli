package v3

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/gofiber/cli/cmd/internal"
)

// MigrateMiddlewareLocals moves the reads of the values that middleware keep in
// the request locals to the functions v3 offers. v3 stores them under keys of
// private types, so c.Locals("requestid") returns nil there, and the context key
// settings that named the keys no longer exist:
//
//	c.Locals("requestid").(string)  ->  requestid.FromContext(c)
//	c.Locals("token").(string)      ->  keyauth.TokenFromContext(c)
//	c.Locals("username").(string)   ->  basicauth.UsernameFromContext(c)
//	c.Locals("csrf").(string)       ->  csrf.TokenFromContext(c)
//	c.Locals("user").(*jwt.Token)   ->  jwtware.FromContext(c)
//
// A read is only a middleware's when the project uses the middleware and the key
// is the one the middleware stores under, by default or through its config
// (constants are resolved), and when the application does not write that key
// itself: a read of an application's own key such as "token" or "username" is
// left alone. A read that cannot be migrated safely is left as it is and noted.
//
// A context key field is dropped once nothing reads the key any more, which is
// not the case as long as a read had to be left alone: the field stays then, and
// keeps the build from passing until the read is dealt with.
func MigrateMiddlewareLocals(cmd *cobra.Command, cwd string, _, _ *semver.Version) error {
	index := newMW2LocalsIndex(cwd)
	changed, err := internal.ChangeFileContentInfo(cwd, func(content string, fi internal.FileInfo) string {
		return migrateLocalsContent(content, fi, index)
	})
	if err != nil {
		return fmt.Errorf("failed to migrate middleware locals: %w", err)
	}
	if !changed {
		return nil
	}

	cmd.Println("Migrating middleware locals")
	return nil
}

func migrateLocalsContent(content string, fi internal.FileInfo, index *mw2LocalsIndex) string {
	if !strings.Contains(content, "Locals(") && !strings.Contains(content, "Context") {
		return content
	}
	m := index.module(fi.Path).planned()
	plan, ok := m.plans[filepath.Clean(fi.Path)]
	if !ok {
		// The file was not read: it does not parse.
		parseFile(fi, content)
		return content
	}

	for _, n := range plan.notes {
		internal.Notef(fi, n.line, "%s", n.msg)
	}

	edits := editSet{edits: slices.Clone(plan.edits.edits)}
	for i := range plan.removals {
		removal := &plan.removals[i]
		if m.blocked[removal.key] > 0 {
			internal.Notef(fi, removal.line,
				"%s was kept: v3 has no such field, and %d read(s) of that key could not be migrated (see the other notes). Move them to %s, then delete the field.",
				removal.what, m.blocked[removal.key], mw2Readers([]*mw2Role{removal.role}))
			continue
		}
		m.noteRemoval(fi, removal)
		edits.edits = append(edits.edits, removal.edits.edits...)
	}

	if len(edits.edits) == 0 {
		return content
	}
	return mw2AddImports(edits.apply(content), plan.imports...)
}

// noteRemoval tells the user what the removal of a key field could not account for.
func (m *mw2LocalsModule) noteRemoval(fi internal.FileInfo, removal *mw2Removal) {
	reader := mw2Readers([]*mw2Role{removal.role})
	unresolved := m.unresolvedRead()

	switch {
	case removal.key.expr && removal.key.dir == "":
		internal.Notef(fi, removal.line,
			"%s was removed: v3 has no such field. The value of its key could not be resolved, so reads of the key by value were not migrated: use %s for them.",
			removal.what, reader)
	case unresolved != "" && removal.key.val != removal.role.defaultKey:
		internal.Notef(fi, removal.line,
			"%s was removed: v3 has no such field. The key of the Locals read at %s cannot be resolved; if it is this key, use %s there.",
			removal.what, unresolved, reader)
	}
}
