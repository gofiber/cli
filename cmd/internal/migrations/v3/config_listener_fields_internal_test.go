package v3

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_appEditsOverlap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		edits []textEdit
		want  bool
	}{
		{name: "none"},
		{name: "apart", edits: []textEdit{{start: 0, end: 2}, {start: 5, end: 6}}},
		{name: "touching", edits: []textEdit{{start: 0, end: 2}, {start: 2, end: 6}}},
		{name: "insertions at one place", edits: []textEdit{{start: 3, end: 3}, {start: 3, end: 3}}},
		{name: "an insertion where an edit ends", edits: []textEdit{{start: 0, end: 3}, {start: 3, end: 3}}},
		{name: "an edit inside another", edits: []textEdit{{start: 0, end: 9}, {start: 2, end: 4}}, want: true},
		{name: "an insertion inside an edit", edits: []textEdit{{start: 4, end: 4}, {start: 0, end: 9}}, want: true},
		{name: "an insertion where an edit starts", edits: []textEdit{{start: 0, end: 0}, {start: 0, end: 5}}, want: true},
		{name: "partly over", edits: []textEdit{{start: 0, end: 5}, {start: 3, end: 8}}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, appEditsOverlap(tt.edits))
		})
	}
}
