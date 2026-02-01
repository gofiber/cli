package v3_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigratePasetoExtractor_HeaderPrefix(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mpaseto")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import pasetoware "github.com/gofiber/contrib/paseto"
var _ = pasetoware.New(pasetoware.Config{
    TokenLookup: [2]string{"header", "Authorization"},
    TokenPrefix: "Bearer",
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigratePasetoExtractor(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "TokenLookup")
	assert.NotContains(t, content, "TokenPrefix")
	assert.Contains(t, content, `Extractor: extractors.FromAuthHeader("Bearer")`)
	assert.Contains(t, content, `"github.com/gofiber/fiber/v3/extractors"`)
	assert.Contains(t, buf.String(), "Migrating paseto middleware configs")
}

func Test_MigratePasetoExtractor_QueryNoPrefix(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mpaseto_query")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import pasetoware "github.com/gofiber/contrib/paseto"
var _ = pasetoware.New(pasetoware.Config{
    TokenLookup: [2]string{pasetoware.LookupQuery, "token"},
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigratePasetoExtractor(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "TokenLookup")
	assert.Contains(t, content, `Extractor: extractors.FromQuery("token")`)
	assert.Contains(t, buf.String(), "Migrating paseto middleware configs")
}

func Test_MigratePasetoExtractor_UnsupportedLookup(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mpaseto_todo")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import (
    pasetoware "github.com/gofiber/contrib/paseto"
    "strings"
)
var _ = pasetoware.New(pasetoware.Config{
    TokenLookup: [2]string{strings.ToUpper("header"), "Authorization"},
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigratePasetoExtractor(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Contains(t, content, "// TODO: migrate TokenLookup: [2]string{strings.ToUpper(\"header\"), \"Authorization\"}")
	assert.NotContains(t, content, "TokenPrefix")
	assert.Contains(t, buf.String(), "Migrating paseto middleware configs")
}

func Test_MigratePasetoExtractor_CustomAlias(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mpaseto_alias")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import authpaseto "github.com/gofiber/contrib/paseto/v3"
var _ = authpaseto.New(authpaseto.Config{
    TokenLookup: [2]string{"cookie", "session"},
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigratePasetoExtractor(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "TokenLookup")
	assert.Contains(t, content, `Extractor: extractors.FromCookie("session")`)
	assert.Contains(t, content, `"github.com/gofiber/fiber/v3/extractors"`)
	assert.Contains(t, buf.String(), "Migrating paseto middleware configs")
}

func Test_MigratePasetoExtractor_ContribV3Import(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mpaseto_v3import")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	file := writeTempFile(t, dir, `package main
import pasetoware "github.com/gofiber/contrib/v3/paseto"
var _ = pasetoware.New(pasetoware.Config{
    TokenLookup: [2]string{"cookie", "session"},
})`)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigratePasetoExtractor(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.NotContains(t, content, "TokenLookup")
	assert.Contains(t, content, `Extractor: extractors.FromCookie("session")`)
	assert.Contains(t, content, `"github.com/gofiber/fiber/v3/extractors"`)
	assert.Contains(t, buf.String(), "Migrating paseto middleware configs")
}

func Test_MigratePasetoExtractor_SkipUnrelatedPackage(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "mpaseto_skip")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(dir)) }()

	original := `package main
import pasetoware "example.com/paseto"
var _ = pasetoware.Config{
    TokenLookup: [2]string{"header", "Authorization"},
}`
	file := writeTempFile(t, dir, original)

	var buf bytes.Buffer
	cmd := newCmd(&buf)
	require.NoError(t, v3.MigratePasetoExtractor(cmd, dir, nil, nil))

	content := readFile(t, file)
	assert.Equal(t, original, content)
	assert.Empty(t, buf.String())
}
