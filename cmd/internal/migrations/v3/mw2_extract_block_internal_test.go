package v3

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_extractBlock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want string
	}{
		{"flat", `{a: 1} rest`, `{a: 1}`},
		{"nested", `{a: map[string]int{"b": 1}, c: 2} rest`, `{a: map[string]int{"b": 1}, c: 2}`},
		{"braces in strings", `{a: "}", b: '}', c: ` + "`}`" + `} rest`, `{a: "}", b: '}', c: ` + "`}`" + `}`},
		{"braces in comments", "{a: 1, // }\n b: /* } */ 2} rest", "{a: 1, // }\n b: /* } */ 2}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// start is the first character after the opening brace.
			end := extractBlock(tt.src, 1, '{', '}')

			assert.Equal(t, tt.want, tt.src[:end])
		})
	}
}
