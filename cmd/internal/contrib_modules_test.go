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
			name:   "opa legacy version suffix",
			module: "opafiber/v2",
			want:   "opa",
		},
		{
			name:   "opa legacy name",
			module: "opafiber",
			want:   "opa",
		},
		{
			name:   "unchanged module",
			module: "monitor",
			want:   "monitor",
		},
		{
			name:   "version suffix of a module that keeps its name",
			module: "casbin/v2",
			want:   "casbin",
		},
		{
			name:   "subpackage of a module that keeps its name",
			module: "websocket/event",
			want:   "websocket/event",
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
	assert.ElementsMatch(t, []string{"opa", "opafiber"}, ContribModuleAliases("opa"))
	assert.Equal(t, []string{"casbin"}, ContribModuleAliases("casbin"))
}

// Every module that was renamed changed its package name with it, which is what
// the migration has to keep alive with an import alias.
func TestContribRenamedModulesChangePackageName(t *testing.T) {
	t.Parallel()

	for legacy, renamed := range contribModuleRenames {
		assert.Equal(t, legacy, ContribPackageName(legacy), legacy)
		assert.Equal(t, legacy, ContribPackageName(legacy+"/v2"), legacy)
		assert.Equal(t, renamed, ContribV3PackageName(NormalizeContribModule(legacy)), legacy)
	}
}

func TestContribPackageName(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "jwtware", ContribPackageName("jwt"))
	assert.Equal(t, "pasetoware", ContribPackageName("paseto"))
	assert.Equal(t, "websocket", ContribPackageName("websocket"))
	assert.Equal(t, "fiberzap", ContribPackageName("fiberzap/v2"))
	assert.Equal(t, "swagger", ContribPackageName("swagger"))

	assert.Equal(t, "jwtware", ContribV3PackageName("jwt"))
	assert.Equal(t, "pasetoware", ContribV3PackageName("paseto"))
	assert.Equal(t, "zap", ContribV3PackageName("zap"))
	assert.Equal(t, "otel", ContribV3PackageName("otel/middleware"))
}
