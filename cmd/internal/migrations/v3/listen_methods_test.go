package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

func Test_MigrateListenMethods(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import (
	"crypto/tls"
	"crypto/x509"

	"github.com/gofiber/fiber/v2"
)

func main() {
	app := fiber.New()
	cert, _ := tls.LoadX509KeyPair("cert.pem", "key.pem")
	pool := x509.NewCertPool()
	app.ListenTLS(":443", "cert.pem", "key.pem")
	app.ListenTLSWithCertificate(":443", cert)
	app.ListenMutualTLS(":443", "cert.pem", "key.pem", "ca.pem")
	app.ListenMutualTLSWithCertificate(":443", cert, pool)
}
`)

	out := appRun(t, dir, v3.MigrateListenMethods)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, `app.Listen(":443", fiber.ListenConfig{CertFile: "cert.pem", CertKeyFile: "key.pem"})`)
	assert.Contains(t, content, `app.Listen(":443", fiber.ListenConfig{TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}})`)
	assert.Contains(t, content, `app.Listen(":443", fiber.ListenConfig{CertFile: "cert.pem", CertKeyFile: "key.pem", CertClientFile: "ca.pem"})`)
	assert.Contains(t, content, `app.Listen(":443", fiber.ListenConfig{TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool, Certificates: []tls.Certificate{cert}}})`)
	assert.NotContains(t, content, "ListenTLS")
	assert.NotContains(t, content, "ListenMutualTLS")
	assert.Contains(t, out, "Migrating listen methods")
	assert.Empty(t, internal.DrainNotes(dir))
}

func Test_MigrateListenMethods_SkipNonFiber(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

type server struct{}

func (server) ListenTLS(a, b, c string)                          {}
func (server) ListenTLSWithCertificate(a string, b struct{})     {}
func (server) ListenMutualTLS(a, b, c, d string)                 {}
func (server) ListenMutualTLSWithCertificate(a string, b, c any) {}

func main() {
	var s server
	cert := struct{}{}
	s.ListenTLS(":443", "cert.pem", "key.pem")
	s.ListenTLSWithCertificate(":443", cert)
	s.ListenMutualTLS(":443", "cert.pem", "key.pem", "ca.pem")
	s.ListenMutualTLSWithCertificate(":443", cert, "ca.pem")
}
`)
	before := appRead(t, dir, "main.go")

	out := appRun(t, dir, v3.MigrateListenMethods)

	assert.Equal(t, before, appRead(t, dir, "main.go"))
	assert.NotContains(t, out, "Migrating listen methods")
}

func Test_MigrateListenMethods_ReceiverShapes(t *testing.T) {
	t.Parallel()

	dir := appProject(t, map[string]string{
		"go.mod": "module example.com/demo\n",
		"app.go": `package main

import f "github.com/gofiber/fiber/v2"

type Server struct{ app *f.App }

func NewApp() *f.App { return f.New() }

func (s *Server) Run(certFile, keyFile string) error {
	return s.app.ListenTLS(":8443", certFile, keyFile)
}

func serve(app *f.App) error {
	return app.ListenTLS(":8444", "a.pem", "b.pem")
}
`,
		"main.go": `package main

func main() {
	app := NewApp()
	go app.ListenTLS(":8445", "cert.pem", "key.pem")
	NewApp().ListenTLS(":8446", "cert.pem", "key.pem")
}
`,
	})

	appRun(t, dir, v3.MigrateListenMethods)

	app := appRead(t, dir, "app.go")
	assert.Contains(t, app, `s.app.Listen(":8443", f.ListenConfig{CertFile: certFile, CertKeyFile: keyFile})`)
	assert.Contains(t, app, `app.Listen(":8444", f.ListenConfig{CertFile: "a.pem", CertKeyFile: "b.pem"})`)

	// main.go does not import fiber: the import is added under its path
	main := appRead(t, dir, "main.go")
	assert.Contains(t, main, `"github.com/gofiber/fiber/v2"`)
	assert.Contains(t, main, `go app.Listen(":8445", fiber.ListenConfig{CertFile: "cert.pem", CertKeyFile: "key.pem"})`)
	assert.Contains(t, main, `NewApp().Listen(":8446", fiber.ListenConfig{CertFile: "cert.pem", CertKeyFile: "key.pem"})`)

	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Equal(t, "app.go", notes[0].File)
	assert.Equal(t, 10, notes[0].Line)
	assert.Contains(t, notes[0].Message, "plain HTTP")
}

func Test_MigrateListenMethods_AddsTLSImport(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

func main() {
	app := fiber.New()
	app.ListenTLSWithCertificate(":443", loadCertificate())
}
`)

	appRun(t, dir, v3.MigrateListenMethods)

	content := appRead(t, dir, "main.go")
	assert.Contains(t, content, `"crypto/tls"`)
	assert.Contains(t, content, `fiber.ListenConfig{TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{loadCertificate()}}}`)
}

func Test_MigrateListenMethods_Idempotent(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

func main() {
	app := fiber.New()
	app.ListenTLS(":443", "cert.pem", "key.pem")
}
`)

	appRun(t, dir, v3.MigrateListenMethods)
	first := appRead(t, dir, "main.go")
	out := appRun(t, dir, v3.MigrateListenMethods)

	assert.Equal(t, first, appRead(t, dir, "main.go"))
	assert.Empty(t, out)
}

func Test_MigrateListenMethods_LeavesWhatItCannotConvert(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

func main() {
	app := fiber.New()
	args := []string{":443", "cert.pem", "key.pem"}
	_ = app.ListenTLS(args[0], args[1], args[2])
	_ = app.ListenTLS
}
`)
	before := appRead(t, dir, "main.go")

	appRun(t, dir, v3.MigrateListenMethods)

	// the call is converted, a bare method value is not a call and stays
	content := appRead(t, dir, "main.go")
	assert.NotEqual(t, before, content)
	assert.Contains(t, content, `_ = app.ListenTLS`+"\n")
	assert.Contains(t, content, `app.Listen(args[0], fiber.ListenConfig{CertFile: args[1], CertKeyFile: args[2]})`)
}

func Test_MigrateListenMethods_BrokenFile(t *testing.T) {
	t.Parallel()

	dir := appSingle(t, `package main

import "github.com/gofiber/fiber/v2"

func main() {
	app := fiber.New()
	app.ListenTLS(":443", "cert.pem", "key.pem"
}
`)
	before := appRead(t, dir, "main.go")

	appRun(t, dir, v3.MigrateListenMethods)

	assert.Equal(t, before, appRead(t, dir, "main.go"))
	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0].Message, "does not parse")
}
