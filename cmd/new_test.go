package cmd

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_New_Run(t *testing.T) {
	at := assert.New(t)

	t.Run("new project", func(t *testing.T) {
		defer func() {
			require.NoError(t, os.Chdir("../"))
			_ = os.RemoveAll("normal")
		}()

		setupCmd()
		defer teardownCmd()

		out, err := runCobraCmd(newCmd, "normal")

		require.NoError(t, err)
		at.Contains(out, "Done")
	})

	t.Run("custom mod name", func(t *testing.T) {
		defer func() {
			require.NoError(t, os.Chdir("../"))
			require.NoError(t, os.RemoveAll("custom_mod_name"))
		}()

		setupCmd()
		defer teardownCmd()

		out, err := runCobraCmd(newCmd, "custom_mod_name", "name")

		require.NoError(t, err)
		at.Contains(out, "name")
	})

	t.Run("create complex project", func(t *testing.T) {
		defer func() {
			require.NoError(t, os.Chdir("../"))
			require.NoError(t, os.RemoveAll("complex"))
		}()

		setupCmd()
		defer teardownCmd()

		out, err := runCobraCmd(newCmd, "complex", "-t=complex")
		require.NoError(t, err)
		at.Contains(out, "Done")
	})

	t.Run("failed to create complex project", func(t *testing.T) {
		defer func() {
			require.NoError(t, os.Chdir("../"))
			require.NoError(t, os.RemoveAll("complex_failed"))
		}()

		setupCmd(errFlag)
		defer teardownCmd()

		out, err := runCobraCmd(newCmd, "complex_failed", "-t=complex")

		require.Error(t, err)
		at.Contains(out, "failed to run")
	})

	t.Run("invalid project name", func(t *testing.T) {
		out, err := runCobraCmd(newCmd, ".")

		require.Error(t, err)
		at.Contains(out, ".")
	})
}

func Test_New_CreateBasic(t *testing.T) {
	require.Error(t, createBasic(" ", "name"))
}

func Test_New_CreateComplex(t *testing.T) {
	t.Run("look path error", func(t *testing.T) {
		setupLookPath(errFlag)
		defer teardownLookPath()

		require.Error(t, createComplex(" ", "name"))
	})

	t.Run("failed to replace pattern", func(t *testing.T) {
		setupLookPath()
		defer teardownLookPath()
		setupCmd(errFlag)
		defer teardownCmd()

		repo = "git@any.provider.com:id/repo.git"

		require.Error(t, createComplex(" ", "name"))
	})
}
