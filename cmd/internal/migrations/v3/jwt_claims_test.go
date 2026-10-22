package v3_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal"
	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// jwtModule writes the files of a project below a new directory, with the go.mod
// of a module that uses the standalone jwt middleware, and runs the migrations
// that move the middleware and the claims library. It returns the directory.
func jwtModule(t *testing.T, files map[string]string) string {
	t.Helper()

	contribVersions(t, map[string]string{"jwt": "v1.2.6"})
	dir := t.TempDir()
	contribWriteFile(t, dir, "go.mod", `module example.com/app

go 1.22

require (
	github.com/form3tech-oss/jwt-go v3.2.2+incompatible
	github.com/gofiber/fiber/v2 v2.52.15
	github.com/gofiber/jwt/v2 v2.2.1
)
`)
	for name, content := range files {
		contribWriteFile(t, dir, name, content)
	}

	contribMigrate(t, dir, v3.MigrateContribPackages, v3.MigrateJWTExtractor, v3.MigrateJWTClaims)
	return dir
}

// jwtAllNotes returns the messages of the notes, one after the other.
func jwtAllNotes(dir string) string {
	var out []string
	for _, n := range internal.DrainNotes(dir) {
		out = append(out, n.Message)
	}
	return strings.Join(out, "\n")
}

// jwtNotesAbout returns the messages of the notes that hold the text.
func jwtNotesAbout(dir, text string) []string {
	var out []string
	for _, n := range internal.DrainNotes(dir) {
		if strings.Contains(n.Message, text) {
			out = append(out, n.Message)
		}
	}
	return out
}

const jwtMainFile = `package main

import (
	"github.com/form3tech-oss/jwt-go"
	"github.com/gofiber/fiber/v2"
	jwtware "github.com/gofiber/jwt/v2"

	"example.com/app/helper"
)

func main() {
	app := fiber.New()
	app.Use(jwtware.New(jwtware.Config{SigningKey: []byte("secret")}))
	app.Get("/me", func(c *fiber.Ctx) error {
		user := c.Locals("user").(*jwt.Token)
		claims := user.Claims.(jwt.MapClaims)
		return c.SendString(claims["name"].(string) + helper.Issuer)
	})
}
`

// The claims library of the project is the one of the middleware that moved: every
// file of the module that uses it moves, also the ones that import neither the
// middleware nor Fiber, and the claims written for the old library follow.
func Test_MigrateJWTClaims_EveryFileOfTheModule(t *testing.T) {
	dir := jwtModule(t, map[string]string{
		"main.go": jwtMainFile,
		"helper/helper.go": `package helper

import (
	"time"

	jwt "github.com/form3tech-oss/jwt-go"
)

const Issuer = "app"

// Claims are the claims of the access tokens.
type Claims struct {
	UserID string ` + "`json:\"userID\"`" + `
	jwt.StandardClaims
}

func Sign(secret []byte, id string) (string, error) {
	claims := Claims{
		UserID: id,
		StandardClaims: jwt.StandardClaims{
			ExpiresAt: time.Now().Add(time.Hour).Unix(),
			IssuedAt:  time.Now().Unix(),
			Audience:  []string{"web"},
			Id:        "id-" + id,
			Issuer:    Issuer,
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
}
`,
		"helper/read.go": `package helper

func IssuerOf(c Claims) string { return c.StandardClaims.Issuer }
`,
	})

	main := readFile(t, filepath.Join(dir, "main.go"))
	contribRequireParses(t, main)
	assert.Contains(t, main, `"github.com/golang-jwt/jwt/v5"`)
	assert.NotContains(t, main, "form3tech")

	helper := readFile(t, filepath.Join(dir, "helper", "helper.go"))
	contribRequireParses(t, helper)
	assert.Contains(t, helper, `jwt "github.com/golang-jwt/jwt/v5"`, "the name of the import stays")
	assert.NotContains(t, helper, "form3tech")
	assert.Contains(t, helper, "\tjwt.RegisteredClaims\n")
	assert.Contains(t, helper, "RegisteredClaims: jwt.RegisteredClaims{")
	assert.Contains(t, helper, "ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),")
	assert.Contains(t, helper, "IssuedAt:  jwt.NewNumericDate(time.Now()),")
	assert.Contains(t, helper, `Audience:  jwt.ClaimStrings{"web"},`)
	assert.Contains(t, helper, `ID:        "id-" + id,`)
	assert.NotContains(t, helper, "StandardClaims")

	read := readFile(t, filepath.Join(dir, "helper", "read.go"))
	assert.Contains(t, read, "c.RegisteredClaims.Issuer", "a file of the module that never imports the library follows the embedded field")

	notes := internal.DrainNotes(dir)
	require.Len(t, notes, 2)
	for _, n := range notes {
		assert.Equal(t, "helper/helper.go", n.File, "the first file that moved, in the order of the walk")
	}
	slices.SortFunc(notes, func(a, b internal.Note) int { return a.Line - b.Line })
	assert.Contains(t, notes[0].Message, "github.com/form3tech-oss/jwt-go")
	assert.Contains(t, notes[0].Message, "in 2 files")
	assert.Contains(t, notes[0].Message, "Token is expired")
	assert.Contains(t, notes[1].Message, "Claims embeds the StandardClaims of the old library")
	assert.Contains(t, notes[1].Message, "its Id is called ID")
}

// The fields of the standard claims that a program reads have other types in v5: the
// first read of each file is told, and a field of something that is no claims is not.
func Test_MigrateJWTClaims_ReadsOfTheStandardFields(t *testing.T) {
	dir := jwtModule(t, map[string]string{
		"main.go": jwtMainFile,
		"helper/helper.go": `package helper

import (
	"time"

	"github.com/golang-jwt/jwt/v4"
)

const Issuer = "app"

type Claims struct{ jwt.StandardClaims }

type User struct{ Id int }

func expired(claims *Claims, u User) bool {
	return claims.ExpiresAt < time.Now().Unix() || claims.StandardClaims.NotBefore > 0 || u.Id == 0
}
`,
	})

	helper := readFile(t, filepath.Join(dir, "helper", "helper.go"))
	assert.Contains(t, helper, "claims.RegisteredClaims.NotBefore")
	notes := jwtNotesAbout(dir, "the times of the claims")
	require.Len(t, notes, 1)
	assert.Contains(t, notes[0], "claims.ExpiresAt: in v5 the times of the claims")
}

func Test_MigrateJWTClaims_SecondRunChangesNothing(t *testing.T) {
	dir := jwtModule(t, map[string]string{
		"main.go": jwtMainFile,
		"helper/helper.go": `package helper

import jwt "github.com/dgrijalva/jwt-go"

const Issuer = "app"

type Claims struct{ jwt.StandardClaims }
`,
	})
	internal.DrainNotes(dir)
	before := map[string]string{}
	for _, name := range []string{"main.go", "helper/helper.go", "go.mod"} {
		before[name] = readFile(t, filepath.Join(dir, name))
	}

	out := contribMigrate(t, dir, v3.MigrateContribPackages, v3.MigrateJWTExtractor, v3.MigrateJWTClaims)

	assert.Empty(t, out)
	assert.Empty(t, internal.DrainNotes(dir))
	for name, content := range before {
		assert.Equal(t, content, readFile(t, filepath.Join(dir, name)), name)
	}
}

// A module that does not use the jwt middleware keeps its library: nothing hands
// over tokens of another one.
func Test_MigrateJWTClaims_NoMiddlewareNoMove(t *testing.T) {
	contribVersions(t, map[string]string{"jwt": "v1.2.6"})
	dir := t.TempDir()
	contribWriteFile(t, dir, "go.mod", "module example.com/app\n\ngo 1.22\n")
	source := `package main

import "github.com/golang-jwt/jwt/v4"

var _ = jwt.StandardClaims{ExpiresAt: 5}
`
	file := contribWriteFile(t, dir, "main.go", source)

	out := contribMigrate(t, dir, v3.MigrateContribPackages, v3.MigrateJWTExtractor, v3.MigrateJWTClaims)

	assert.Equal(t, source, readFile(t, file))
	assert.Empty(t, out)
	assert.Empty(t, internal.DrainNotes(dir))
}

// The middleware of another module of the repository is no reason to move this
// one.
func Test_MigrateJWTClaims_OtherModule(t *testing.T) {
	contribVersions(t, map[string]string{"jwt": "v1.2.6"})
	dir := t.TempDir()
	contribWriteFile(t, dir, "go.mod", "module example.com/app\n\ngo 1.22\n")
	contribWriteFile(t, dir, "api/go.mod", "module example.com/api\n\ngo 1.22\n")
	contribWriteFile(t, dir, "api/main.go", `package main

import (
	"github.com/gofiber/fiber/v2"
	jwtware "github.com/gofiber/jwt/v3"
)

var _ = jwtware.New(jwtware.Config{SigningKey: []byte("secret")})
var _ fiber.Handler
`)
	source := "package app\n\nimport \"github.com/golang-jwt/jwt/v4\"\n\nvar _ jwt.Claims\n"
	file := contribWriteFile(t, dir, "app.go", source)

	contribMigrate(t, dir, v3.MigrateContribPackages, v3.MigrateJWTExtractor, v3.MigrateJWTClaims)

	assert.Equal(t, source, readFile(t, file), "the middleware is in the module of the api directory")
}

// The golang-jwt v4 of the project moves with the rest, a RegisteredClaims it uses
// already needs nothing else, and so does the request package.
func Test_MigrateJWTClaims_GolangJWTV4(t *testing.T) {
	dir := jwtModule(t, map[string]string{
		"main.go": `package main

import (
	"github.com/gofiber/fiber/v2"
	jwtware "github.com/gofiber/jwt/v3"
	"github.com/golang-jwt/jwt/v4"
	"github.com/golang-jwt/jwt/v4/request"
)

var _ = jwtware.New(jwtware.Config{SigningKey: []byte("secret")})

func parse(c *fiber.Ctx) (*jwt.Token, error) {
	claims := &jwt.RegisteredClaims{}
	return request.ParseFromRequestWithClaims(c.Request(), request.OAuth2Extractor, claims, func(*jwt.Token) (any, error) { return nil, nil })
}
`,
	})

	main := readFile(t, filepath.Join(dir, "main.go"))
	contribRequireParses(t, main)
	assert.Contains(t, main, `"github.com/golang-jwt/jwt/v5"`)
	assert.Contains(t, main, `"github.com/golang-jwt/jwt/v5/request"`)
	assert.NotContains(t, main, "jwt/v4")
	assert.Contains(t, main, "&jwt.RegisteredClaims{}")
}

// The audience of golang-jwt and dgrijalva is a string and the one of form3tech a
// list; both are a jwt.ClaimStrings in v5. A time that is not the Unix seconds of
// a time.Time cannot be wrapped without knowing that 0 meant none.
func Test_MigrateJWTClaims_StandardClaimsFields(t *testing.T) {
	dir := jwtModule(t, map[string]string{
		"main.go": jwtMainFile,
		"helper/helper.go": `package helper

import (
	"time"

	"github.com/dgrijalva/jwt-go"
)

const Issuer = "app"

func claims(seconds int64, aud []string) jwt.Claims {
	_ = aud
	return jwt.StandardClaims{
		Audience:  "web",
		ExpiresAt: (time.Now().Add(time.Minute)).Unix(),
		NotBefore: seconds,
		Subject:   "me",
	}
}

func list() jwt.Claims { return jwt.StandardClaims{"web", 1, "id", 2, "iss", 3, "sub"} }
`,
	})

	helper := readFile(t, filepath.Join(dir, "helper", "helper.go"))
	assert.Contains(t, helper, `Audience:  jwt.ClaimStrings{"web"},`)
	assert.Contains(t, helper, "ExpiresAt: jwt.NewNumericDate((time.Now().Add(time.Minute))),")
	assert.Contains(t, helper, "NotBefore: seconds,", "left for the user, who is told")
	assert.Contains(t, helper, "jwt.RegisteredClaims{\"web\", 1,", "an unkeyed literal cannot be mapped")

	var messages []string
	for _, n := range internal.DrainNotes(dir) {
		messages = append(messages, n.Message)
	}
	joined := strings.Join(messages, "\n")
	assert.Contains(t, joined, "NotBefore of jwt.RegisteredClaims is a *jwt.NumericDate in v5")
	assert.Contains(t, joined, "the fields of jwt.StandardClaims are not all named here")
}

func Test_MigrateJWTClaims_FormAudienceList(t *testing.T) {
	dir := jwtModule(t, map[string]string{
		"main.go": jwtMainFile,
		"helper/helper.go": `package helper

import jwt "github.com/form3tech-oss/jwt-go"

const Issuer = "app"

var aud = []string{"a", "b"}

var literal = jwt.StandardClaims{Audience: []string{"web", "mobile"}}
var named = jwt.StandardClaims{Audience: aud}
`,
	})

	helper := readFile(t, filepath.Join(dir, "helper", "helper.go"))
	assert.Contains(t, helper, `Audience: jwt.ClaimStrings{"web", "mobile"}`)
	assert.Contains(t, helper, "Audience: jwt.ClaimStrings(aud)")
}

// A Valid() method of a claims type becomes the Validate() method v5 calls, and
// the call that asked the standard claims goes: the parser checks them first.
func Test_MigrateJWTClaims_ValidMethods(t *testing.T) {
	dir := jwtModule(t, map[string]string{
		"main.go": jwtMainFile,
		"helper/helper.go": `package helper

import (
	"errors"

	"github.com/golang-jwt/jwt/v4"
)

const Issuer = "app"

type Access struct {
	UserID string
	jwt.RegisteredClaims
}

func (c Access) Valid() error {
	if err := c.RegisteredClaims.Valid(); err != nil {
		return err
	}
	if c.UserID == "" {
		return errors.New("no user")
	}
	return nil
}

type Plain struct{ jwt.RegisteredClaims }

func (c *Plain) Valid() error { return c.RegisteredClaims.Valid() }

type Odd struct{ jwt.RegisteredClaims }

func (c Odd) Valid() error {
	err := c.RegisteredClaims.Valid()
	if err != nil {
		return errors.New("odd: " + err.Error())
	}
	return nil
}

// Form is no claims type, its Valid is its own.
type Form struct{ Name string }

func (f Form) Valid() error { return nil }
`,
	})

	helper := readFile(t, filepath.Join(dir, "helper", "helper.go"))
	contribRequireParses(t, helper)
	assert.Contains(t, helper, "func (c Access) Validate() error {\n\tif c.UserID == \"\" {")
	assert.NotContains(t, helper, "c.RegisteredClaims.Valid(); err != nil")
	assert.Contains(t, helper, "func (c *Plain) Validate() error { return nil }")
	assert.Contains(t, helper, "func (c Odd) Valid() error {", "a body that does more with the check is left to the user")
	assert.Contains(t, helper, "func (f Form) Valid() error { return nil }")

	odd := jwtNotesAbout(dir, "does not rewrite")
	require.Len(t, odd, 1)
	assert.Contains(t, odd[0], "Valid calls Valid() of its standard claims in a way this migration does not rewrite")
}

func Test_MigrateJWTClaims_ParserLiteral(t *testing.T) {
	dir := jwtModule(t, map[string]string{
		"main.go": jwtMainFile,
		"helper/helper.go": `package helper

import "github.com/golang-jwt/jwt/v4"

const Issuer = "app"

var strict = &jwt.Parser{ValidMethods: []string{"HS256"}, UseJSONNumber: true}
var lax = &jwt.Parser{SkipClaimsValidation: false}
var value = jwt.Parser{UseJSONNumber: true}
var computed = &jwt.Parser{UseJSONNumber: enabled()}
var empty = &jwt.Parser{}

func enabled() bool { return true }
`,
	})

	helper := readFile(t, filepath.Join(dir, "helper", "helper.go"))
	contribRequireParses(t, helper)
	assert.Contains(t, helper, `var strict = jwt.NewParser(jwt.WithValidMethods([]string{"HS256"}), jwt.WithJSONNumber())`)
	assert.Contains(t, helper, `var lax = jwt.NewParser()`)
	assert.Contains(t, helper, "var value = jwt.Parser{UseJSONNumber: true}", "a value has no constructor to take its place")
	assert.Contains(t, helper, "var computed = &jwt.Parser{UseJSONNumber: enabled()}")
	assert.Contains(t, helper, "var empty = &jwt.Parser{}", "the zero parser of v5 works")

	notes := jwtNotesAbout(dir, "the fields of jwt.Parser are private in v5")
	assert.Len(t, notes, 2)
}

// What v5 took away has no rewrite that keeps the code working: each kind of use
// is told once per file, with what to write.
func Test_MigrateJWTClaims_RemovedNames(t *testing.T) {
	dir := jwtModule(t, map[string]string{
		"main.go": jwtMainFile,
		"helper/helper.go": `package helper

import (
	"time"

	"github.com/golang-jwt/jwt/v4"
)

const Issuer = "app"

func check(err error, claims jwt.MapClaims, token *jwt.Token) bool {
	jwt.TimeFunc = func() time.Time { return time.Unix(0, 0) }
	if ve, ok := err.(*jwt.ValidationError); ok && ve.Errors&jwt.ValidationErrorExpired != 0 {
		return true
	}
	if err != nil && err.(*jwt.ValidationError).Errors&jwt.ValidationErrorMalformed != 0 {
		return false
	}
	return claims.VerifyExpiresAt(time.Now().Unix(), true) && claims.VerifyAudience("web", true)
}

func valid(token *jwt.Token) error { return token.Claims.Valid() }
`,
	})

	helper := readFile(t, filepath.Join(dir, "helper", "helper.go"))
	assert.Contains(t, helper, "return jwt.NewValidator(jwt.WithIssuedAt()).Validate(token.Claims)")
	assert.Contains(t, helper, "jwt.TimeFunc = ", "left in place so that the build tells")

	messages := jwtAllNotes(dir)
	assert.Contains(t, messages, "jwt.TimeFunc is gone in v5")
	assert.Contains(t, messages, "v5 has no jwt.ValidationError")
	assert.Contains(t, messages, "errors.Is(err, jwt.ErrTokenExpired)")
	assert.Contains(t, messages, "claims.VerifyExpiresAt: v5 took the Verify methods of the claims away")
	assert.Equal(t, 1, countOf(messages, "v5 has no jwt.ValidationError"), "once per file")
	assert.Equal(t, 1, countOf(messages, "took the Verify methods"), "once per file")
}

// A name the file declares is no package: a variable called jwt does not make the
// library of the file hide.
func Test_MigrateJWTClaims_ALocalNamedLikeThePackage(t *testing.T) {
	dir := jwtModule(t, map[string]string{
		"main.go": jwtMainFile,
		"helper/helper.go": `package helper

import "github.com/dgrijalva/jwt-go"

const Issuer = "app"

type holder struct{ StandardClaims string }

func use(jwt holder) string { return jwt.StandardClaims }

var _ = jwt.StandardClaims{}
`,
	})

	helper := readFile(t, filepath.Join(dir, "helper", "helper.go"))
	contribRequireParses(t, helper)
	assert.Contains(t, helper, "var _ = jwt.RegisteredClaims{}")
	assert.Contains(t, helper, "func use(jwt holder) string { return jwt.RegisteredClaims }",
		"the field called after the type follows, wherever it is selected")
}

// A file that stands alone, with no go.mod to tell the module, moves when it
// imports the middleware itself.
func Test_MigrateJWTClaims_FileWithoutModule(t *testing.T) {
	contribVersions(t, map[string]string{"jwt": "v1.2.6"})
	dir := t.TempDir()
	file := writeTempFile(t, dir, `package main

import (
	"github.com/form3tech-oss/jwt-go"
	jwtware "github.com/gofiber/jwt/v2"
)

var _ = jwtware.New(jwtware.Config{SigningKey: []byte("secret")})
var _ jwt.MapClaims
`)

	contribMigrate(t, dir, v3.MigrateContribPackages, v3.MigrateJWTExtractor, v3.MigrateJWTClaims)

	content := readFile(t, file)
	assert.Contains(t, content, `"github.com/golang-jwt/jwt/v5"`)
	assert.NotContains(t, content, "form3tech")
}

// A package of the library that v5 does not have stays where it is, and is told.
func Test_MigrateJWTClaims_PackageWithoutCounterpart(t *testing.T) {
	dir := jwtModule(t, map[string]string{
		"main.go": jwtMainFile,
		"helper/helper.go": `package helper

import (
	_ "github.com/dgrijalva/jwt-go"
	_ "github.com/dgrijalva/jwt-go/cmd/jwt"
)

const Issuer = "app"
`,
	})

	helper := readFile(t, filepath.Join(dir, "helper", "helper.go"))
	assert.Contains(t, helper, `"github.com/dgrijalva/jwt-go/cmd/jwt"`)
	messages := jwtAllNotes(dir)
	assert.Contains(t, messages, "github.com/dgrijalva/jwt-go/cmd/jwt has no counterpart")
}

// The test files and the files below directories of the module are part of it.
func Test_MigrateJWTClaims_TestFiles(t *testing.T) {
	dir := jwtModule(t, map[string]string{
		"main.go":                      jwtMainFile,
		"helper/helper.go":             "package helper\n\nconst Issuer = \"app\"\n",
		"helper/helper_test.go":        "package helper_test\n\nimport (\n\t\"testing\"\n\n\tjwt \"github.com/golang-jwt/jwt/v4\"\n)\n\nfunc TestClaims(t *testing.T) {\n\t_ = jwt.StandardClaims{Issuer: \"x\"}\n}\n",
		"vendor/example.com/x/x.go":    "package x\n\nimport \"github.com/golang-jwt/jwt/v4\"\n\nvar _ jwt.Claims\n",
		"internal/deep/path/deeper.go": "package deeper\n\nimport \"github.com/golang-jwt/jwt/v4\"\n\nvar _ jwt.Claims\n",
	})

	test := readFile(t, filepath.Join(dir, "helper", "helper_test.go"))
	assert.Contains(t, test, `jwt "github.com/golang-jwt/jwt/v5"`)
	assert.Contains(t, test, "jwt.RegisteredClaims{Issuer: \"x\"}")
	assert.Contains(t, readFile(t, filepath.Join(dir, "internal", "deep", "path", "deeper.go")), "jwt/v5")

	_, err := os.Stat(filepath.Join(dir, "vendor"))
	require.NoError(t, err)
	assert.Contains(t, readFile(t, filepath.Join(dir, "vendor", "example.com", "x", "x.go")), "jwt/v4", "vendor is not the project")
}
