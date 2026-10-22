package sessionshim

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The type is a string of its own, as session.Source was: the constants have the
// strings of the session middleware of Fiber v2, and a value of the type is not a
// string where an interface is asked for it.
func Test_SessionSource(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "cookie", string(SessionSourceCookie))
	assert.Equal(t, "header", string(SessionSourceHeader))
	assert.Equal(t, "query", string(SessionSourceURLQuery))

	var typed any = SessionSourceCookie
	var plain any = "cookie"
	_, isString := typed.(string)
	_, isSource := typed.(SessionSource)
	assert.False(t, isString)
	assert.True(t, isSource)
	assert.NotEqual(t, typed, plain)
}
