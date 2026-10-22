package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateStaticRoutes(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"time"

	"github.com/gofiber/fiber/v2"
)

func main() {
	app := fiber.New()
	app.Static("/", "./public")
	app.Static("/prefix", "./public")
	app.Static("/dir/", "./public")
	app.Static("*", "./public/index.html")
	app.Static("/john*", "./john")
	app.Static("/prefix", "./public", fiber.Static{Index: "index.htm"})
	app.Static("/fiber", "./public", fiber.Static{Compress: true})
	app.Static("/all", "./public", fiber.Static{
		Compress:      true,
		ByteRange:     true,
		Browse:        true,
		Download:      true,
		Index:         "home.html",
		CacheDuration: 10 * time.Second,
		MaxAge:        3600,
	})
}
`)

	out := appRun(t, dir, v3.MigrateStaticRoutes)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, `"github.com/gofiber/fiber/v3/middleware/static"`)
	assert.Contains(t, content, `app.Get("/*", static.New("./public"))`)
	assert.Contains(t, content, `app.Get("/prefix*", static.New("./public"))`)
	assert.Contains(t, content, `app.Get("/dir/*", static.New("./public"))`)
	assert.Contains(t, content, `app.Get("*", static.New("./public/index.html"))`)
	assert.Contains(t, content, `app.Get("/john*", static.New("./john"))`)
	assert.Contains(t, content, `static.New("./public", static.Config{IndexNames: []string{"index.htm"}})`)
	assert.Contains(t, content, `static.New("./public", static.Config{Compress: true})`)
	assert.Contains(t, content, "IndexNames:    []string{\"home.html\"},")
	assert.Contains(t, content, "CacheDuration: 10 * time.Second,")
	assert.NotContains(t, content, "fiber.Static")
	assert.NotContains(t, content, ".Static(")
	assert.Contains(t, out, "Migrating app.Static usage")
	assert.Empty(t, internal.DrainNotes(dir))
}

func Test_MigrateStaticRoutes_ConfigsWithCodeInThem(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"time"

	"github.com/gofiber/fiber/v2"
)

func routes(app *fiber.App, n int) {
	app.Static("/plain", "./plain")
	app.Static("/static", "./public", fiber.Static{
		Compress:  true, // enable gzip (recommended)
		ByteRange: true,
		Index:     "index.html", // default document
	})
	app.Static("/m", "./public", fiber.Static{
		ModifyResponse: func(c *fiber.Ctx) error {
			c.Set("X-Static", "1")
			return nil
		},
		Next: func(c *fiber.Ctx) bool {
			return c.Method() != fiber.MethodGet
		},
		MaxAge:        int(time.Hour.Seconds()),
		CacheDuration: time.Duration(n) * time.Second,
	})
}
`)

	appRun(t, dir, v3.MigrateStaticRoutes)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, `app.Get("/plain*", static.New("./plain"))`)
	assert.Contains(t, content, "app.Get(\"/static*\", static.New(\"./public\", static.Config{\n\t\tCompress:   true, // enable gzip (recommended)")
	assert.Contains(t, content, "IndexNames: []string{\"index.html\"}, // default document")
	assert.Contains(t, content, "ModifyResponse: func(c *fiber.Ctx) error {")
	assert.Contains(t, content, "MaxAge:        int(time.Hour.Seconds()),")
	assert.Contains(t, content, "CacheDuration: time.Duration(n) * time.Second,\n\t}))")
	assert.NotContains(t, content, "fiber.Static")
	assert.Empty(t, internal.DrainNotes(dir))
}

func Test_MigrateStaticRoutes_ComputedArguments(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"os"
	"path/filepath"

	"github.com/gofiber/fiber/v2"
)

const (
	publicDir = "./public"
	apiPrefix = "/api"
)

func register(app *fiber.App, base string) {
	prefix := "/static"
	app.Static("/", publicDir)
	app.Static("/assets", filepath.Join(base, "assets"))
	app.Static(prefix, "./public")
	app.Static("/v"+"1", "./v1")
	app.Static(apiPrefix, base, fiber.Static{Browse: true})
	app.Static("/env", os.Getenv("ASSETS_DIR"))
	app.Static(`+"`/raw`"+`, `+"`./raw`"+`)
	app.Static("/here", "")
}
`)

	appRun(t, dir, v3.MigrateStaticRoutes)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, `app.Get("/*", static.New(publicDir))`)
	assert.Contains(t, content, `app.Get("/assets*", static.New(filepath.Join(base, "assets")))`)
	assert.Contains(t, content, `app.Get(prefix+"*", static.New("./public"))`)
	assert.Contains(t, content, `app.Get("/v1*", static.New("./v1"))`)
	// an empty root served the working directory in v2, static.New("") serves the file system root
	assert.Contains(t, content, `app.Get(apiPrefix+"*", static.New(cmp.Or(base, "."), static.Config{Browse: true}))`)
	assert.Contains(t, content, `app.Get("/env*", static.New(cmp.Or(os.Getenv("ASSETS_DIR"), ".")))`)
	assert.Contains(t, content, `app.Get("/raw*", static.New(`+"`./raw`"+`))`)
	assert.Contains(t, content, `app.Get("/here*", static.New("."))`)
	assert.Contains(t, content, `"cmp"`)
	assert.NotContains(t, content, ".Static(")
	assert.Empty(t, internal.DrainNotes(dir))
}

func Test_MigrateStaticRoutes_GroupsRoutersAndChains(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

type server struct{ app *fiber.App }

func (s *server) routes() {
	s.app.Static("/field", "./field")
}

func register(r fiber.Router) {
	r.Static("/router", "./router")
}

func main() {
	app := fiber.New()
	api := app.Group("/api")
	api.Static("/assets", "./assets")
	app.Static("/a", "./a").Static("/b", "./b")
}
`)

	appRun(t, dir, v3.MigrateStaticRoutes)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, `s.app.Get("/field*", static.New("./field"))`)
	assert.Contains(t, content, `r.Get("/router*", static.New("./router"))`)
	assert.Contains(t, content, `api.Get("/assets*", static.New("./assets"))`)
	assert.Contains(t, content, `app.Get("/a*", static.New("./a")).Get("/b*", static.New("./b"))`)
}

func Test_MigrateStaticRoutes_ConfigTypeAndIndex(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"time"

	"github.com/gofiber/fiber/v2"
)

func staticConfig() fiber.Static {
	return fiber.Static{MaxAge: 60, Index: "start.html"}
}

func main() {
	app := fiber.New()

	cfg := fiber.Static{
		Compress:      true,
		CacheDuration: time.Minute,
	}
	cfg.Index = "home.html"
	app.Static("/", "./public", cfg)

	var browse = fiber.Static{Browse: true}
	app.Static("/b", "./public", browse)
	app.Static("/p", "./public", staticConfig())
}
`)

	appRun(t, dir, v3.MigrateStaticRoutes)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, "func staticConfig() static.Config {")
	assert.Contains(t, content, `return static.Config{MaxAge: 60, IndexNames: []string{"start.html"}}`)
	assert.Contains(t, content, "cfg := static.Config{")
	assert.Contains(t, content, `cfg.IndexNames = []string{"home.html"}`)
	assert.Contains(t, content, "var browse = static.Config{Browse: true}")
	assert.Contains(t, content, `app.Get("/*", static.New("./public", cfg))`)
	assert.Contains(t, content, `app.Get("/b*", static.New("./public", browse))`)
	assert.Contains(t, content, `app.Get("/p*", static.New("./public", staticConfig()))`)
	assert.NotContains(t, content, "fiber.Static")
}

func Test_MigrateStaticRoutes_OwnStaticPackage(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"go.mod": "module example.com/demo\n",
		"static/static.go": `package static

func Version() string { return "1" }
`,
		"main.go": `package main

import (
	"log"

	"example.com/demo/static"
	"github.com/gofiber/fiber/v2"
)

func main() {
	app := fiber.New()
	app.Static("/", "./public")
	log.Println(static.Version())
}
`,
	})

	appRun(t, dir, v3.MigrateStaticRoutes)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, `staticmw "github.com/gofiber/fiber/v3/middleware/static"`)
	assert.Contains(t, content, `app.Get("/*", staticmw.New("./public"))`)
	assert.Contains(t, content, "static.Version()")
}

func Test_MigrateStaticRoutes_AliasedFiberImport(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import f "github.com/gofiber/fiber/v2"

func main() {
	app := f.New()
	app.Static("/", "./public", f.Static{Index: "a.html"})
}
`)

	appRun(t, dir, v3.MigrateStaticRoutes)

	assert.Contains(t, appRead(t, dir, "main.go"), `app.Get("/*", static.New("./public", static.Config{IndexNames: []string{"a.html"}}))`)
}

func Test_MigrateStaticRoutes_OtherTypesAndText(t *testing.T) {
	t.Parallel()

	src := `package main

import "github.com/gofiber/fiber/v2"

type files struct{}

func (files) Static(prefix, root string) {}

func main() {
	var f files
	f.Static("/", "./public") // app.Static("/", "./public")
	println("app.Static(\"/\", \"./public\")")
	_ = fiber.New()
}
`
	dir := appSingle(t, src)

	out := appRun(t, dir, v3.MigrateStaticRoutes)

	assert.Equal(t, src, appRead(t, dir, "main.go"))
	assert.Empty(t, out)
}

func Test_MigrateStaticRoutes_UnsupportedShapes(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

func main() {
	app := fiber.New()
	cfgs := []fiber.Static{{Browse: true}}
	app.Static("/", "./public", cfgs...)
	app.Static("/", "./public", fiber.Static{}, fiber.Static{})
}
`)

	appRun(t, dir, v3.MigrateStaticRoutes)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, `app.Static("/", "./public", cfgs...)`)
	assert.Contains(t, content, `app.Static("/", "./public", static.Config{}, static.Config{})`)
	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 2)
	assert.Contains(t, notes[0].Message, "Static was removed in v3")
}

func Test_MigrateStaticRoutes_Idempotent(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

func main() {
	app := fiber.New()
	app.Static("/", "./public")
	app.Static("/s", "./s", fiber.Static{Index: "a.html"})
}
`)

	appRun(t, dir, v3.MigrateStaticRoutes)
	first := appRead(t, dir, "main.go")
	out := appRun(t, dir, v3.MigrateStaticRoutes)

	assert.Equal(t, first, appRead(t, dir, "main.go"))
	assert.Empty(t, out)
}

func Test_MigrateStaticRoutes_OneBadFileDoesNotStopTheOthers(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"a_bad.go": `package main

import "github.com/gofiber/fiber/v2"

func badRoutes(app *fiber.App) {
	app.Static("/dl", "./files", fiber.Static{
		Next: func(c *fiber.Ctx) bool { return false },
	}
}
`,
		"z_good.go": `package main

import "github.com/gofiber/fiber/v2"

func goodRoutes(app *fiber.App) {
	app.Static("/", "./public")
}
`,
	})

	appRun(t, dir, v3.MigrateStaticRoutes)

	assert.Contains(t, appRead(t, dir, "z_good.go"), `app.Get("/*", static.New("./public"))`)
	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Equal(t, "a_bad.go", notes[0].File)
}
