package v3

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_shimUnixLines(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "package x\n\nfunc f() {}\n", shimUnixLines("package x\r\n\r\nfunc f() {}\r\n"))
	assert.Equal(t, "package x\n", shimUnixLines("package x\n"), "the line ends of Unix stay")
	assert.Equal(t, "a\rb\n", shimUnixLines("a\rb\r\n"), "a \\r that is no part of a line end stays")
	assert.Empty(t, shimUnixLines(""))
}

// The package clause of a source is found by its line end, so an embedded file that a checkout
// converted to \r\n (git on Windows) would lose its code. Every embedded source goes through
// shimUnixLines; a new shim is added to this list.
func Test_shimSources_HaveTheLineEndsOfUnix(t *testing.T) {
	t.Parallel()

	for name, source := range map[string]string{
		"agentshim/agent.go":       clientShimSource,
		"ctxshim/ctx.go":           legacyShimSource,
		"downloadshim/download.go": downloadShimSource,
		"gettershim/getters.go":    getterShimSource,
		"healthshim/health.go":     healthShimSource,
		"listenshim/listen.go":     appShimSource,
		"postgresshim/postgres.go": pgShimSource,
		"sessionshim/session.go":   sessionShimSource,
	} {
		assert.NotEmpty(t, source, name)
		assert.NotContains(t, source, "\r\n", name)
	}

	// and so do the files the migrations write
	for _, file := range []moduleShimFile{
		clientShimModuleFile(), legacyShimModuleFile(), downloadShimModuleFile(), getterShimModuleFile(),
		healthShimModuleFile(), appShimModuleFile(), pgShimModuleFile(),
	} {
		assert.NotContains(t, file.source, "\r\n", file.name)
		assert.Contains(t, file.source, "\npackage "+moduleShimPkg+"\n", file.name)
	}
}
