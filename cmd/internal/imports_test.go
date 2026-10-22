package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_tidyImports(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "drops what nothing uses, keeps what is used",
			in: `package p

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/utils/v2"
)

var _ = fmt.Sprint
var _ fiber.Ctx
`,
			want: `package p

import (
	"fmt"

	"github.com/gofiber/fiber/v3"
)

var _ = fmt.Sprint
var _ fiber.Ctx
`,
		},
		{
			name: "an alias that nothing uses is dropped, one that is used stays",
			in: `package p

import (
	f "github.com/gofiber/fiber/v3"
	xx "example.com/x"
	yy "example.com/y"
)

var _ f.Ctx
var _ yy.T
`,
			want: `package p

import (
	yy "example.com/y"
	f "github.com/gofiber/fiber/v3"
)

var _ f.Ctx
var _ yy.T
`,
		},
		{
			name: "a package whose name cannot be told from its path is never dropped",
			in: `package p

import (
	"github.com/gofiber/contrib/v3/jwt"
	"github.com/gofiber/contrib/v3/paseto"
	"github.com/stretchr/testify/require"
)

var _ = jwtware.New
`,
			want: `package p

import (
	"github.com/gofiber/contrib/v3/jwt"
	"github.com/gofiber/contrib/v3/paseto"
	"github.com/stretchr/testify/require"
)

var _ = jwtware.New
`,
		},
		{
			name: "a package of an unknown name goes when every qualifier is explained by another import",
			in: `package p

import (
	"github.com/gofiber/fiber/v3"
	"github.com/golang-jwt/jwt/v5"
)

var _ fiber.Ctx
`,
			want: `package p

import (
	"github.com/gofiber/fiber/v3"
)

var _ fiber.Ctx
`,
		},
		{
			name: "a package of an unknown name stays while a qualifier is unexplained",
			in: `package p

import (
	"github.com/gofiber/fiber/v3"
	"github.com/golang-jwt/jwt/v5"
)

var _ fiber.Ctx
var _ = other.Value
`,
			want: `package p

import (
	"github.com/gofiber/fiber/v3"
	"github.com/golang-jwt/jwt/v5"
)

var _ fiber.Ctx
var _ = other.Value
`,
		},
		{
			name: "a local variable named like the package does not use it",
			in: `package p

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/client"
)

type getter struct{}

func (getter) Get(string) string { return "" }

func f(url string) string {
	fiber := getter{}
	return fiber.Get(url)
}

var _ = client.Get
`,
			want: `package p

import (
	"github.com/gofiber/fiber/v3/client"
)

type getter struct{}

func (getter) Get(string) string { return "" }

func f(url string) string {
	fiber := getter{}
	return fiber.Get(url)
}

var _ = client.Get
`,
		},
		{
			name: "uses behind a generic type, in a type switch and in a struct are seen",
			in: `package p

import (
	"io"
	"net/http"
	stdlog "log"
	"sync"
	"time"
)

type l[T any] struct{}

var _ l[*stdlog.Logger]

func f(v any) {
	switch v.(type) {
	case *http.Request:
	}
}

type s struct {
	sync.Mutex
	d []time.Duration
}
`,
			want: `package p

import (
	stdlog "log"
	"net/http"
	"sync"
	"time"
)

type l[T any] struct{}

var _ l[*stdlog.Logger]

func f(v any) {
	switch v.(type) {
	case *http.Request:
	}
}

type s struct {
	sync.Mutex
	d []time.Duration
}
`,
		},
		{
			name: "versioned standard library packages are named after the element before the version",
			in: `package p

import (
	"math/rand/v2"
	"os"
)

var _ = rand.IntN
`,
			want: `package p

import (
	"math/rand/v2"
)

var _ = rand.IntN
`,
		},
		{
			name: "an unused versioned standard library package goes",
			in: `package p

import (
	"math/rand/v2"
	"os"
)

var _ = os.Args
`,
			want: `package p

import (
	"os"
)

var _ = os.Args
`,
		},
		{
			name: "blank, dot and cgo imports stay",
			in: `package p

import (
	_ "embed"
	. "strings"
)
`,
			want: `package p

import (
	_ "embed"
	. "strings"
)
`,
		},
		{
			name: "an import that is missing is not added",
			in: `package p

var _ = fmt.Sprint
`,
			want: `package p

var _ = fmt.Sprint
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := tidyImports("p.go", []byte(tc.in))
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(got))
		})
	}
}

func Test_tidyImports_UnparsableSource(t *testing.T) {
	t.Parallel()

	_, err := tidyImports("p.go", []byte("package p\n\nvar x = (1\n"))
	require.Error(t, err)
}
