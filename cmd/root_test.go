package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_Root_Execute(t *testing.T) {
	setupOsExit()
	defer teardownOsExit()

	b := &bytes.Buffer{}
	rootCmd.SetErr(b)
	rootCmd.SetOut(b)

	Execute()
}

func Test_Root_FiberCmd(t *testing.T) {
	assert.Equal(t, rootCmd, FiberCmd())
}
