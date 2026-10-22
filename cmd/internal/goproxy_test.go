package internal

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_GoProxyBase(t *testing.T) {
	tests := []struct {
		name     string
		goproxy  string
		want     string
		unset    bool
		wantOkay bool
	}{
		{name: "not set", unset: true, want: "https://proxy.golang.org", wantOkay: true},
		{name: "empty", goproxy: "", want: "https://proxy.golang.org", wantOkay: true},
		{name: "a private proxy first", goproxy: "https://proxy.example.com/,https://proxy.golang.org,direct", want: "https://proxy.example.com", wantOkay: true},
		{name: "pipe separated", goproxy: "off|https://proxy.example.com", want: "https://proxy.example.com", wantOkay: true},
		{name: "direct only", goproxy: "direct", wantOkay: false},
		{name: "off", goproxy: "off", wantOkay: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Not parallel: it sets an environment variable.
			t.Setenv("GOPROXY", tt.goproxy)
			if tt.unset {
				require.NoError(t, os.Unsetenv("GOPROXY"))
			}

			got, ok := GoProxyBase()
			assert.Equal(t, tt.wantOkay, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}
