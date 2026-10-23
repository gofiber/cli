package v3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// healthShimVersions are the versions of Fiber v3 the file is built with: the first
// release and a recent one.
var healthShimVersions = []string{"v3.0.0", "v3.6.1"}

// healthShimTestifyVersion is the version of testify the tests of the file ask for.
const healthShimTestifyVersion = "v1.12.1"

// Test_HealthShimBuildsAgainstFiber builds the file as the project gets it, in the
// package of the module, and runs the tests of its source, so that it keeps
// compiling with the releases of v3 a project can have.
func Test_HealthShimBuildsAgainstFiber(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("builds Fiber, which takes a while")
	}

	tests, err := os.ReadFile(filepath.Join("healthshim", "health_test.go"))
	require.NoError(t, err)
	source := shimUnixLines(string(tests))
	testSource := strings.Replace(source, "package healthshim\n", "package "+moduleShimPkg+"\n", 1)
	require.NotEqual(t, source, testSource)

	for _, version := range healthShimVersions {
		t.Run(version, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			pkg := filepath.Join(dir, "internal", moduleShimPkg)
			require.NoError(t, os.MkdirAll(pkg, 0o750))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/shimcheck\n\ngo 1.25\n\nrequire (\n"+
				"\tgithub.com/gofiber/fiber/v3 "+version+"\n\tgithub.com/stretchr/testify "+healthShimTestifyVersion+"\n)\n"), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(pkg, healthShimFile), []byte(healthShimModuleFile().source), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(pkg, "health_test.go"), []byte(testSource), 0o600))

			shimBuild(t, dir, "fiber "+version)
		})
	}
}
