package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeContribModule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		module string
		want   string
	}{
		{
			name:   "otel legacy version suffix",
			module: "otelfiber/v2",
			want:   "otel",
		},
		{
			name:   "otel legacy subpackage",
			module: "otelfiber/v2/middleware",
			want:   "otel/middleware",
		},
		{
			name:   "i18n legacy name",
			module: "fiberi18n",
			want:   "i18n",
		},
		{
			name:   "i18n legacy subpackage",
			module: "fiberi18n/messages",
			want:   "i18n/messages",
		},
		{
			name:   "zerolog legacy name",
			module: "fiberzerolog",
			want:   "zerolog",
		},
		{
			name:   "unchanged module",
			module: "monitor",
			want:   "monitor",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, NormalizeContribModule(tt.module))
		})
	}
}

func TestContribModuleAliases(t *testing.T) {
	t.Parallel()

	assert.ElementsMatch(t, []string{"otel", "otelfiber"}, ContribModuleAliases("otel"))
	assert.ElementsMatch(t, []string{"i18n", "fiberi18n"}, ContribModuleAliases("i18n"))
	assert.ElementsMatch(t, []string{"zerolog", "fiberzerolog"}, ContribModuleAliases("zerolog"))
}
