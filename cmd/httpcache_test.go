package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_HTTPCache_ReadWrite(t *testing.T) {
	clearHTTPCache()

	url := "https://example.com/foo"
	body := []byte("bar")

	_, ok := readFromFile(url)
	require.False(t, ok)

	writeToFile(url, body)

	b, ok := readFromFile(url)
	require.True(t, ok)
	require.Equal(t, body, b)
}
