package v3_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

const mw3ReleaseHeader = `package main

import (
	"context"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/session"
)

var store = session.NewStore()

var _ = context.Background
`

func Test_MigrateSessionRelease_WhereTheStoreComesFrom(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, mw3ReleaseHeader+`
type server struct {
	store *session.Store
	other int
}

// a package level store
func packageLevel(c fiber.Ctx) error {
	sess, err := store.Get(c)
	if err != nil {
		return err
	}
	return sess.Save()
}

// a store that was handed in
func parameter(c fiber.Ctx, st *session.Store) error {
	sess, err := st.Get(c)
	if err != nil {
		return err
	}
	return sess.Save()
}

// a field of a struct the package declares
func (s *server) field(c fiber.Ctx) error {
	sess, err := s.store.Get(c)
	if err != nil {
		return err
	}
	return sess.Save()
}

// a local store, from a typed declaration
func typed(c fiber.Ctx) error {
	var local *session.Store = session.NewStore()
	sess, err := local.Get(c)
	if err != nil {
		return err
	}
	return sess.Save()
}

// not a session store
type cache struct{}

func (cache) Get(c fiber.Ctx) (string, error) { return "", nil }

func other(c fiber.Ctx, ca cache, s *server) error {
	v, err := ca.Get(c)
	if err != nil {
		return err
	}
	w, err := s.other.Get(c)
	_, _ = v, w
	return err
}
`, v3.MigrateSessionRelease)

	assert.Equal(t, 4, strings.Count(got, "defer sess.Release() // Important: Manual cleanup required"))
	assert.NotContains(t, got, "defer v.Release")
	assert.NotContains(t, got, "defer w.Release")
	assert.Empty(t, res.notes)
	assert.Contains(t, res.out, "Adding defer sess.Release()")
}

func Test_MigrateSessionRelease_PlacesTheDeferAfterTheErrorCheck(t *testing.T) {
	t.Parallel()

	got, _ := mw3RunOne(t, mw3ReleaseHeader+`
func withElse(c fiber.Ctx) error {
	sess, err := store.Get(c)
	if err != nil {
		return err
	} else {
		sess.Set("a", 1)
	}
	return sess.Save()
}

func withComment(c fiber.Ctx) error {
	sess, err := store.Get(c) // the session of this request
	if err != nil {
		return err
	}
	return sess.Save()
}

func noCheck(c fiber.Ctx) error {
	sess, _ := store.Get(c)
	return sess.Save()
}

func differentCheck(c fiber.Ctx) error {
	sess, err := store.Get(c)
	sess.Set("a", 1)
	if err != nil {
		return err
	}
	return sess.Save()
}

func inACase(c fiber.Ctx, n int) error {
	switch n {
	case 1:
		sess, err := store.Get(c)
		if err != nil {
			return err
		}
		return sess.Save()
	}
	return nil
}
`, v3.MigrateSessionRelease)

	assert.Contains(t, got, `	} else {
		sess.Set("a", 1)
	}
	defer sess.Release() // Important: Manual cleanup required
	return sess.Save()`)
	assert.Contains(t, got, `	sess, err := store.Get(c) // the session of this request
	if err != nil {
		return err
	}
	defer sess.Release() // Important: Manual cleanup required`)
	assert.Contains(t, got, `	sess, _ := store.Get(c)
	defer sess.Release() // Important: Manual cleanup required`)
	assert.Contains(t, got, `	sess, err := store.Get(c)
	defer sess.Release() // Important: Manual cleanup required
	sess.Set("a", 1)`)
	assert.Contains(t, got, `		if err != nil {
			return err
		}
		defer sess.Release() // Important: Manual cleanup required
		return sess.Save()
	}`)
}

// A session that leaves the function must not be released in it: whoever gets
// it next would find a reset session, and Save on it panics.
func Test_MigrateSessionRelease_LeavesASessionThatEscapes(t *testing.T) {
	t.Parallel()

	tests := []struct{ name, body string }{
		{
			name: "returned from a helper",
			body: `func get(c fiber.Ctx) (*session.Session, error) {
	sess, err := store.Get(c)
	if err != nil {
		return nil, err
	}
	return sess, nil
}`,
		},
		{
			name: "returned from a closure",
			body: `var get = func(c fiber.Ctx) (*session.Session, error) {
	sess, err := store.Get(c)
	if err != nil {
		return nil, err
	}
	return sess, nil
}`,
		},
		{
			name: "stored in a struct",
			body: `type holder struct{ sess *session.Session }

func get(c fiber.Ctx, h *holder) error {
	sess, err := store.Get(c)
	if err != nil {
		return err
	}
	h.sess = sess
	return nil
}`,
		},
		{
			name: "wrapped in a returned struct",
			body: `type wrapper struct{ sess *session.Session }

func get(c fiber.Ctx) (*wrapper, error) {
	sess, err := store.Get(c)
	if err != nil {
		return nil, err
	}
	return &wrapper{sess: sess}, nil
}`,
		},
		{
			name: "sent on a channel",
			body: `func get(c fiber.Ctx, out chan<- *session.Session) error {
	sess, err := store.Get(c)
	if err != nil {
		return err
	}
	out <- sess
	return nil
}`,
		},
		{
			name: "handed to a goroutine",
			body: `func get(c fiber.Ctx) error {
	sess, err := store.Get(c)
	if err != nil {
		return err
	}
	go func() { _ = sess.Save() }()
	return nil
}`,
		},
		{
			name: "started with go",
			body: `func save(sess *session.Session) { _ = sess.Save() }

func get(c fiber.Ctx) error {
	sess, err := store.Get(c)
	if err != nil {
		return err
	}
	go save(sess)
	return nil
}`,
		},
		{
			name: "appended to a list",
			body: `var all []*session.Session

func get(c fiber.Ctx) error {
	sess, err := store.Get(c)
	if err != nil {
		return err
	}
	all = append(all, sess)
	return nil
}`,
		},
		{
			name: "copied to another variable",
			body: `var last *session.Session

func get(c fiber.Ctx) error {
	sess, err := store.Get(c)
	if err != nil {
		return err
	}
	last = sess
	return nil
}`,
		},
		{
			name: "returned by a closure that outlives the function",
			body: `func get(c fiber.Ctx) (func() error, error) {
	sess, err := store.Get(c)
	if err != nil {
		return nil, err
	}
	return func() error { return sess.Save() }, nil
}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			src := mw3ReleaseHeader + "\n" + tc.body + "\n"
			got, res := mw3RunOne(t, src, v3.MigrateSessionRelease)
			assert.Equal(t, src, got)
			assert.Empty(t, res.out)
			require.Len(t, res.notes, 1)
			assert.Contains(t, res.notes[0].Message, "the session sess leaves this function")
			assert.Positive(t, res.notes[0].Line)
		})
	}
}

// What a function does with the session before it returns is no escape.
func Test_MigrateSessionRelease_ReleasesASessionThatStaysInTheFunction(t *testing.T) {
	t.Parallel()

	got, res := mw3RunOne(t, mw3ReleaseHeader+`
func render(c fiber.Ctx, sess *session.Session) error { return c.SendString(sess.ID()) }

func use(c fiber.Ctx) error {
	sess, err := store.Get(c)
	if err != nil {
		return err
	}
	if sess == nil || sess.Fresh() {
		return render(c, sess)
	}
	defer func() { _ = sess.Save() }()
	return render(c, sess)
}

// A middleware keeps the session for the handlers that run inside Next.
func middleware(c fiber.Ctx) error {
	sess, err := store.Get(c)
	if err != nil {
		return err
	}
	c.Locals("session", sess)
	return c.Next()
}
`, v3.MigrateSessionRelease)

	assert.Equal(t, 2, strings.Count(got, "defer sess.Release()"))
	assert.Empty(t, res.notes)
}

func Test_MigrateSessionRelease_LeavesWhatIsAlreadyHandled(t *testing.T) {
	t.Parallel()

	tests := []struct{ name, body string }{
		{
			name: "a deferred release",
			body: `func get(c fiber.Ctx) error {
	sess, err := store.Get(c)
	if err != nil {
		return err
	}
	defer sess.Release()
	return sess.Save()
}`,
		},
		{
			name: "a release at the end",
			body: `func get(c fiber.Ctx) error {
	sess, err := store.Get(c)
	if err != nil {
		return err
	}
	err = sess.Save()
	sess.Release()
	return err
}`,
		},
		{
			name: "a loop",
			body: `func get(c fiber.Ctx, ids []string) error {
	for _, id := range ids {
		sess, err := store.GetByID(c, id)
		if err != nil {
			return err
		}
		_ = sess.Save()
	}
	return nil
}`,
		},
		{
			name: "an if statement that holds the session",
			body: `func get(c fiber.Ctx) error {
	if sess, err := store.Get(c); err == nil {
		return sess.Save()
	}
	return nil
}`,
		},
		{
			name: "the middleware pattern",
			body: `func get(c fiber.Ctx) error {
	sess := session.FromContext(c)
	sess.Set("a", 1)
	return nil
}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			src := mw3ReleaseHeader + "\n" + tc.body + "\n"
			got, res := mw3RunOne(t, src, v3.MigrateSessionRelease)
			assert.Equal(t, src, got)
			assert.Empty(t, res.out)
			assert.Empty(t, res.notes)
		})
	}
}

func Test_MigrateSessionRelease_Idempotent(t *testing.T) {
	t.Parallel()

	got, first := mw3RunOne(t, mw3ReleaseHeader+`
func get(c fiber.Ctx) error {
	sess, err := store.Get(c)
	if err != nil {
		return err
	}
	return sess.Save()
}
`, v3.MigrateSessionRelease)
	require.NotEmpty(t, first.out)

	again, second := mw3RunOne(t, got, v3.MigrateSessionRelease)
	assert.Equal(t, got, again)
	assert.Empty(t, second.out)
}

func Test_MigrateSessionRelease_StoreOfAnotherFile(t *testing.T) {
	t.Parallel()

	res := mw3Run(t, map[string]string{
		"store.go": `package main

import "github.com/gofiber/fiber/v3/middleware/session"

var Store = session.NewStore()

type App struct{ Sessions *session.Store }
`,
		"handlers.go": `package main

import "github.com/gofiber/fiber/v3"

func get(c fiber.Ctx, app *App) error {
	sess, err := Store.Get(c)
	if err != nil {
		return err
	}
	other, err := app.Sessions.Get(c)
	if err != nil {
		return err
	}
	_ = other
	return sess.Save()
}
`,
	}, v3.MigrateSessionRelease)

	assert.Equal(t, 2, strings.Count(res.files["handlers.go"], "Release() // Important: Manual cleanup required"))
}
