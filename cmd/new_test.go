package cmd

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_New_Run(t *testing.T) {
	at := assert.New(t)

	t.Run("new project", func(t *testing.T) {
		defer func() {
			at.Nil(os.Chdir("../"))
			at.Nil(os.RemoveAll("normal"))
		}()

		setupCmd()
		defer teardownCmd()

		out, err := runCobraCmd(newCmd, "normal")

		at.Nil(err)
		at.Contains(out, "Done")
	})

	t.Run("custom mod name", func(t *testing.T) {
		defer func() {
			at.Nil(os.Chdir("../"))
			at.Nil(os.RemoveAll("custom_mod_name"))
		}()

		setupCmd()
		defer teardownCmd()

		out, err := runCobraCmd(newCmd, "custom_mod_name", "name")

		at.Nil(err)
		at.Contains(out, "name")
	})

	t.Run("create complex project", func(t *testing.T) {
		defer func() {
			at.Nil(os.Chdir("../"))
			at.Nil(os.RemoveAll("complex"))
		}()

		setupCmd()
		defer teardownCmd()

		out, err := runCobraCmd(newCmd, "complex", "-t=complex")
		at.Nil(err)
		at.Contains(out, "Done")
	})

	t.Run("failed to create complex project", func(t *testing.T) {
		defer func() {
			at.Nil(os.Chdir("../"))
			at.Nil(os.RemoveAll("complex_failed"))
		}()

		setupCmd(errFlag)
		defer teardownCmd()

		out, err := runCobraCmd(newCmd, "complex_failed", "-t=complex")

		at.NotNil(err)
		at.Contains(out, "failed to run")
	})

	t.Run("invalid project name", func(t *testing.T) {
		out, err := runCobraCmd(newCmd, ".")

		at.NotNil(err)
		at.Contains(out, ".")
	})
}

func Test_New_CreateBasic(t *testing.T) {
	assert.NotNil(t, createBasic(" ", "name"))
}

func Test_New_CreateComplex(t *testing.T) {
	at := assert.New(t)

	t.Run("look path error", func(t *testing.T) {
		setupLookPath(errFlag)
		defer teardownLookPath()

		at.NotNil(createComplex(" ", "name"))
	})

	t.Run("failed to replace pattern", func(t *testing.T) {
		setupLookPath()
		defer teardownLookPath()
		setupCmd()
		defer teardownCmd()

		at.NotNil(createComplex(" ", "name"))
	})
}
