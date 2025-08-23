package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateSessionConfig(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "msession")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    "github.com/gofiber/fiber/v2/middleware/session"
    "time"
)
var _ = session.New(session.Config{
    Expiration: 5 * time.Minute,
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateSessionConfig(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "IdleTimeout:")
	assert.NotContains(t, content, "Expiration:")
	assert.Contains(t, buf.String(), "Migrating session middleware configs")
}

func Test_MigrateSessionConfig_KeyLookup_Cookie(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "msessionkl_cookie")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2/middleware/session"
var _ = session.New(session.Config{
    KeyLookup: "cookie:session_id",
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateSessionExtractor(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "KeyLookup")
	assert.Contains(t, content, `Extractor: session.FromCookie("session_id")`)
	assert.Contains(t, buf.String(), "Migrating session KeyLookup config")
}

func Test_MigrateSessionConfig_KeyLookup_Header(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "msessionkl_header")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2/middleware/session"
var _ = session.New(session.Config{
    KeyLookup: "header:X-Session-ID",
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateSessionExtractor(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "KeyLookup")
	assert.Contains(t, content, `Extractor: session.FromHeader("X-Session-ID")`)
	assert.Contains(t, buf.String(), "Migrating session KeyLookup config")
}

func Test_MigrateSessionConfig_KeyLookup_Query(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "msessionkl_query")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2/middleware/session"
var _ = session.New(session.Config{
    KeyLookup: "query:session_id",
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateSessionExtractor(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "KeyLookup")
	assert.Contains(t, content, `Extractor: session.FromQuery("session_id")`)
	assert.Contains(t, buf.String(), "Migrating session KeyLookup config")
}

func Test_MigrateSessionConfig_KeyLookup_Unknown(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "msessionkl_unknown")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import "github.com/gofiber/fiber/v2/middleware/session"
var _ = session.New(session.Config{
    KeyLookup: "unknown:session_id",
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigrateSessionExtractor(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "KeyLookup")
	assert.NotContains(t, content, "Extractor")
	assert.Contains(t, buf.String(), "Migrating session KeyLookup config")
}
