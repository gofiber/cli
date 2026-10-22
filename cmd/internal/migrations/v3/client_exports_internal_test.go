package v3

import (
	"go/parser"
	"go/token"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// clientExportsOf returns "name:kinds" for each export of a file that imports fiber.
func clientExportsOf(t *testing.T, src string) []string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "x.go", "package x\n\nimport fiber \"github.com/gofiber/fiber/v3\"\n\n"+src, parser.SkipObjectResolution)
	require.NoError(t, err)

	var got []string
	for _, ex := range clientFileExports(file, clientAgentFuncs(file)) {
		got = append(got, ex.name+":"+clientKindWords(ex.kinds, ""))
	}
	sort.Strings(got)
	return got
}

func Test_clientFileExports(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"result", "func New() *fiber.Agent { return nil }", []string{"New:agent"}},
		{"parameter", "func Do(a *fiber.Agent) {}", []string{"Do:agent"}},
		{"unexported function", "func do(a *fiber.Agent) *fiber.Client { return nil }", nil},
		{"unrelated", "func New(app *fiber.App, c fiber.Ctx) error { return nil }", nil},
		{"method of an unexported type", "type t struct{}\nfunc (t) Get() *fiber.Client { return nil }", []string{"t.Get:client"}},
		{"method of a generic type", "type l[T any] struct{}\nfunc (l[T]) Agents() []*fiber.Agent { return nil }", []string{"l.Agents:agent"}},
		{"both kinds", "func Pair() (*fiber.Agent, *fiber.Client) { return nil, nil }", []string{"Pair:agent and client"}},
		{"form file", "func File() *fiber.FormFile { return nil }", []string{"File:form file"}},
		{"variable with a type", "var A *fiber.Agent\nvar b *fiber.Agent", []string{"A:agent"}},
		{"variable that is made", "var A = fiber.Get(\"x\").Set(\"k\", \"v\")\nvar C = &fiber.Client{}\nvar D = fiber.AcquireClient()", []string{"A:agent", "C:client", "D:client"}},
		{"variable that is sent", "var Body = func() []byte { _, b, _ := fiber.Get(\"x\").Bytes(); return b }()\nvar Code, _, _ = fiber.Get(\"x\").Bytes()", nil},
		{"variable that is a function literal", "var Make = func() *fiber.Agent { return nil }\nvar Run = func() { a := fiber.Get(\"x\"); _ = a }", []string{"Make:agent"}},
		{"variable that names a function", "var New = newRequest\nfunc newRequest() *fiber.Agent { return nil }", []string{"New:agent"}},
		{"alias", "type Request = *fiber.Agent\ntype list []*fiber.Agent\ntype Clients []*fiber.Client", []string{"Clients:client", "Request:agent"}},
		{"struct fields", "type API struct {\n\tAgent *fiber.Agent\n\tclient *fiber.Client\n\tName string\n}", []string{"API.Agent:agent"}},
		{"embedded field", "type API struct {\n\t*fiber.Agent\n}\ntype hidden struct {\n\tfiber.Client\n}", []string{"API.Agent:agent", "hidden.Client:client"}},
		{"nested field", "type API struct {\n\tPool map[string][]*fiber.Agent\n}", []string{"API.Pool:agent"}},
		{"interface", "type Doer interface {\n\tDo(*fiber.Agent) error\n\tdone(*fiber.Client)\n}", []string{"Doer.Do:agent"}},
		{"function type", "type Hook func(*fiber.Client)\ntype hook func(*fiber.Client)", []string{"Hook:client"}},
		{"constants and others", "const Name = \"x\"\ntype Config struct{ App *fiber.App }", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, clientExportsOf(t, tc.src))
		})
	}
}

func Test_clientFileExports_NeedsTheFiberImport(t *testing.T) {
	t.Parallel()

	file, err := parser.ParseFile(token.NewFileSet(), "x.go", "package x\n\nfunc New() *fiber.Agent { return nil }\n", parser.SkipObjectResolution)
	require.NoError(t, err)
	assert.Empty(t, clientFileExports(file, nil), "a file that does not import fiber does not use its types")

	aliased, err := parser.ParseFile(token.NewFileSet(), "x.go", "package x\n\nimport f \"github.com/gofiber/fiber/v3\"\n\nfunc New() *f.Agent { return nil }\nfunc Other() *fiber.Agent { return nil }\n", parser.SkipObjectResolution)
	require.NoError(t, err)
	exports := clientFileExports(aliased, nil)
	require.Len(t, exports, 1)
	assert.Equal(t, "New", exports[0].name)
}

func Test_clientDomainSet(t *testing.T) {
	t.Parallel()

	var set clientDomainSet
	assert.False(t, set.has(clientDomainAgent))
	set = set.with(clientDomainClient).with(clientDomainFile)
	assert.False(t, set.has(clientDomainAgent))
	assert.True(t, set.has(clientDomainClient))
	assert.True(t, set.has(clientDomainFile))
	assert.Equal(t, "client and form file", clientKindWords(set, ""))
	assert.Equal(t, "clients and form files", clientKindWords(set, "s"))
	assert.True(t, strings.HasPrefix(clientKindWords(clientDomainSet(0).with(clientDomainAgent), "s"), "agents"))
}
