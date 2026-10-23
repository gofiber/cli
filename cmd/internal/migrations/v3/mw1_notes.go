package v3

import (
	"cmp"
	"fmt"
	"go/ast"
	"slices"
	"strings"

	"github.com/gofiber/cli/cmd/internal"
)

// mw1Reporter collects the notes of a migration while it walks a file, and
// leaves them once the output is known, so that the line a note carries is the
// line of the construct in the text the migration returns: the edits and the
// imports it adds move the code below them.
type mw1Reporter struct {
	g       *goSource
	fi      internal.FileInfo
	pending []mw1PendingNote
}

type mw1PendingNote struct {
	message string
	offset  int
}

func newMW1Reporter(g *goSource, fi internal.FileInfo) *mw1Reporter {
	return &mw1Reporter{g: g, fi: fi}
}

// notef records a note for the node.
func (r *mw1Reporter) notef(at ast.Node, format string, args ...any) {
	r.pending = append(r.pending, mw1PendingNote{offset: r.g.offset(at.Pos()), message: fmt.Sprintf(format, args...)})
}

// flush leaves the notes. applied is the source with the edits applied, final
// the text the migration returns, which only differs by the imports it added
// at the top of the file.
func (r *mw1Reporter) flush(edits *editSet, applied, final string) {
	added := strings.Count(final, "\n") - strings.Count(applied, "\n")
	for _, p := range r.pending {
		offset := mw1MappedOffset(edits.edits, p.offset)
		internal.NotefReturned(r.fi, internal.LineOf(applied, offset)+added, "%s", p.message)
	}
	r.pending = nil
}

// mw1MappedOffset returns where the byte at offset of the source ends up once
// the edits are applied the way editSet.apply does: sorted, and an edit that
// overlaps an earlier one dropped.
func mw1MappedOffset(edits []textEdit, offset int) int {
	sorted := slices.Clone(edits)
	slices.SortStableFunc(sorted, func(a, b textEdit) int {
		return cmp.Or(cmp.Compare(a.start, b.start), cmp.Compare(b.end, a.end))
	})

	shift, pos := 0, 0
	for _, e := range sorted {
		if e.start < pos || e.end < e.start {
			continue
		}
		pos = e.end
		switch {
		case e.end <= offset:
			shift += len(e.text) - (e.end - e.start)
		case e.start < offset:
			// the byte is inside the replaced text: it lands where that text begins
			return e.start + shift
		}
	}
	return offset + shift
}
