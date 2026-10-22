package v3

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
