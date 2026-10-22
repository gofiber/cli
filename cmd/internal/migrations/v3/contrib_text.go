package v3

import (
	"strconv"
	"strings"
)

// contribLineStart returns the offset of the first byte of the line holding off.
func contribLineStart(src string, off int) int {
	return strings.LastIndexByte(src[:off], '\n') + 1
}

// contribLineEnd returns the offset just past the line break of the line holding
// off, or len(src) when it is the last line.
func contribLineEnd(src string, off int) int {
	if i := strings.IndexByte(src[off:], '\n'); i >= 0 {
		return off + i + 1
	}
	return len(src)
}

// contribUnquote returns the value of a string literal, or its text when it
// does not unquote.
func contribUnquote(lit string) string {
	if s, err := strconv.Unquote(lit); err == nil {
		return s
	}
	return lit
}
