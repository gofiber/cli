package cmd

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_Helpers_FormatLatency(t *testing.T) {
	t.Parallel()

	cases := []struct {
		d        time.Duration
		expected time.Duration
	}{
		{time.Millisecond * 123456, time.Millisecond * 123450},
		{time.Millisecond * 12340, time.Millisecond * 12340},
		{time.Microsecond * 123456, time.Microsecond * 123450},
		{time.Microsecond * 123450, time.Microsecond * 123450},
		{time.Nanosecond * 123456, time.Nanosecond * 123450},
		{time.Nanosecond * 123450, time.Nanosecond * 123450},
		{time.Nanosecond * 123, time.Nanosecond * 123},
	}

	for _, tc := range cases {
		t.Run(tc.d.String(), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, formatLatency(tc.d), tc.expected)
		})
	}
}

func Test_Helper_Replace(t *testing.T) {
	t.Parallel()

	dir, err := os.MkdirTemp("", "test_helper_replace")
	require.NoError(t, err)
	defer func() {
		require.NoError(t, os.RemoveAll(dir))
	}()

	f, err := os.CreateTemp(dir, "*.go")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	require.NoError(t, replace(dir, "*.go", "old", "new"))
}

func Test_Helper_LoadConfig(t *testing.T) {
	t.Parallel()

	t.Run("no config file", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, loadConfig())
	})

	t.Run("has config file", func(t *testing.T) {
		t.Parallel()
		origHome := homeDir
		tempHome := setupHomeDir(t, "LoadConfig")
		homeDir = tempHome
		defer func() {
			homeDir = origHome
			teardownHomeDir(tempHome)
		}()

		filename := fmt.Sprintf("%s%c%s", homeDir, os.PathSeparator, configName)

		f, err := os.Create(filename)
		require.NoError(t, err)
		defer func() { require.NoError(t, f.Close()) }()
		_, err = f.WriteString("{}")
		require.NoError(t, err)

		require.NoError(t, loadConfig())
	})
}

func Test_Helper_StoreJSON(t *testing.T) {
	t.Parallel()

	require.Error(t, storeJSON("", complex(1, 1)))
}

func Test_Helper_ConfigFilePath(t *testing.T) {
	t.Parallel()
	dir := homeDir
	homeDir = ""
	assert.Equal(t, configName, configFilePath())
	homeDir = dir
}
