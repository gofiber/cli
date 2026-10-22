package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gofiber/cli/cmd/internal/migrations/v3"
)

// customLogger is the logger of the v2 docs: it implements log.AllLogger with the
// methods the interface lists. Lines are appended per test.
const customLoggerMethods = `
func (m *myLogger) Trace(v ...interface{})                      {}
func (m *myLogger) Debug(v ...interface{})                      {}
func (m *myLogger) Info(v ...interface{})                       {}
func (m *myLogger) Warn(v ...interface{})                       {}
func (m *myLogger) Error(v ...interface{})                      {}
func (m *myLogger) Fatal(v ...interface{})                      {}
func (m *myLogger) Panic(v ...interface{})                      {}
func (m *myLogger) Tracef(f string, v ...interface{})           {}
func (m *myLogger) Debugf(f string, v ...interface{})           {}
func (m *myLogger) Infof(f string, v ...interface{})            {}
func (m *myLogger) Warnf(f string, v ...interface{})            {}
func (m *myLogger) Errorf(f string, v ...interface{})           {}
func (m *myLogger) Fatalf(f string, v ...interface{})           {}
func (m *myLogger) Panicf(f string, v ...interface{})           {}
func (m *myLogger) Tracew(msg string, kv ...interface{})        {}
func (m *myLogger) Debugw(msg string, kv ...interface{})        {}
func (m *myLogger) Infow(msg string, kv ...interface{})         {}
func (m *myLogger) Warnw(msg string, kv ...interface{})         {}
func (m *myLogger) Errorw(msg string, kv ...interface{})        {}
func (m *myLogger) Fatalw(msg string, kv ...interface{})        {}
func (m *myLogger) Panicw(msg string, kv ...interface{})        {}
func (m *myLogger) SetLevel(lv fiberlog.Level)                  {}
func (m *myLogger) SetOutput(w io.Writer)                       {}
`

func Test_MigrateLoggerGenerics_CustomLogger(t *testing.T) {
	t.Parallel()

	content, run := mw2Single(t, v3.MigrateLoggerGenerics, `package main

import (
	"context"
	"io"
	stdlog "log"

	fiberlog "github.com/gofiber/fiber/v2/log"
)

type myLogger struct{ l *stdlog.Logger }
`+customLoggerMethods+`
func (m *myLogger) Logger() *stdlog.Logger                                { return m.l }
func (m *myLogger) WithContext(ctx context.Context) fiberlog.CommonLogger { return m }

var _ fiberlog.AllLogger = (*myLogger)(nil)

func Setup() {
	fiberlog.SetLogger(&myLogger{})
	_ = fiberlog.DefaultLogger()
}
`)

	assert.Contains(t, content, "var _ fiberlog.AllLogger[*stdlog.Logger] = (*myLogger)(nil)")
	assert.Contains(t, content, "fiberlog.SetLogger(&myLogger{})", "SetLogger infers its type argument")
	assert.Contains(t, content, "fiberlog.DefaultLogger[*stdlog.Logger]()")
	assert.Contains(t, content, "WithContext(ctx any) fiberlog.CommonLogger")
	assert.NotContains(t, content, "// TODO(fiber migrate)", "the type has a Logger method already")
	assert.Contains(t, run.out, "Migrating logger generics")
	assert.Empty(t, run.notes)
}

func Test_MigrateLoggerGenerics_CustomLoggerWithoutLoggerMethod(t *testing.T) {
	t.Parallel()

	content, run := mw2Single(t, v3.MigrateLoggerGenerics, `package main

import (
	"context"
	"io"

	fiberlog "github.com/gofiber/fiber/v2/log"
)

type myLogger struct{}
`+customLoggerMethods+`
func (m *myLogger) WithContext(ctx context.Context) fiberlog.CommonLogger { return m }

var _ fiberlog.AllLogger = (*myLogger)(nil)
var _ fiberlog.ControlLogger = &myLogger{}

func Setup() {
	fiberlog.SetLogger(&myLogger{})
	fiberlog.DefaultLogger().SetLevel(fiberlog.LevelDebug)
}
`)

	assert.Contains(t, content, "var _ fiberlog.AllLogger[any] = (*myLogger)(nil)")
	assert.Contains(t, content, "var _ fiberlog.ConfigurableLogger[any] = &myLogger{}")
	assert.Contains(t, content, "fiberlog.DefaultLogger[any]().SetLevel(fiberlog.LevelDebug)")
	assert.Contains(t, content, "// TODO(fiber migrate): v3's log.AllLogger[T] requires Logger() T")
	assert.Contains(t, content, "func (m *myLogger) Logger() any { return nil }")
	assert.Equal(t, 1, countOf(content, "Logger() any"))
	assert.Empty(t, run.notes)

	// The method is added once: a second run changes nothing.
	assert.Equal(t, content, mw2Again(t, v3.MigrateLoggerGenerics, run.dir).file(t, "main.go"))
}

func Test_MigrateLoggerGenerics_WithContextUsingTheContext(t *testing.T) {
	t.Parallel()

	content, _ := mw2Single(t, v3.MigrateLoggerGenerics, `package main

import (
	"context"
	"io"

	fiberlog "github.com/gofiber/fiber/v2/log"
)

type myLogger struct{ id string }
`+customLoggerMethods+`
func (m *myLogger) Logger() any { return nil }

func (m *myLogger) WithContext(ctx context.Context) fiberlog.CommonLogger {
	if v, ok := ctx.Value("id").(string); ok {
		return &myLogger{id: v}
	}
	return m
}
`)

	assert.Contains(t, content, "func (m *myLogger) WithContext(ctxAny any) fiberlog.CommonLogger {\n\tctx, _ := ctxAny.(context.Context)\n")
	assert.Contains(t, content, `ctx.Value("id")`)
}

func Test_MigrateLoggerGenerics_LoggerMethodInAnotherFile(t *testing.T) {
	t.Parallel()

	run := mw2Apply(t, v3.MigrateLoggerGenerics, map[string]string{
		"main.go": `package main

import (
	fiberlog "github.com/gofiber/fiber/v2/log"
)

func main() {
	l := newLogger()
	fiberlog.SetLogger(l)
	fiberlog.Info("started")
	var _ fiberlog.AllLogger = l
}
`,
		"mylogger.go": `package main

import (
	"context"
	"io"
	stdlog "log"

	fiberlog "github.com/gofiber/fiber/v2/log"
)

type myLogger struct{ l *stdlog.Logger }

func newLogger() *myLogger { return &myLogger{l: stdlog.Default()} }
` + customLoggerMethods + `
func (m *myLogger) Logger() *stdlog.Logger                                { return m.l }
func (m *myLogger) WithContext(ctx context.Context) fiberlog.CommonLogger { return m }
`,
	})

	main := run.file(t, "main.go")
	assert.Contains(t, main, "fiberlog.SetLogger(l)", "the old output was SetLogger[any](l), which does not compile")
	assert.Contains(t, main, "var _ fiberlog.AllLogger[*stdlog.Logger] = l")
	assert.Contains(t, main, `stdlog "log"`, "the type argument needs the import it was declared with")
	assert.NotContains(t, run.file(t, "mylogger.go"), "Logger() any")
}

func Test_MigrateLoggerGenerics_DefaultLogger(t *testing.T) {
	t.Parallel()

	content, run := mw2Single(t, v3.MigrateLoggerGenerics, `package main

import (
	"os"

	"github.com/gofiber/fiber/v2/log"
)

func Setup() {
	l := log.DefaultLogger()
	l.SetLevel(log.LevelDebug)
	l.SetOutput(os.Stderr)
	log.Info("hello")
	log.WithContext(nil).Info("ctx")
}
`)

	// DefaultLogger[any]() answers nil: the built-in logger's Logger() is a *log.Logger.
	assert.Contains(t, content, "l := log.DefaultLogger[*stdlog.Logger]()")
	assert.Contains(t, content, `stdlog "log"`)
	assert.Contains(t, run.out, "Migrating logger generics")
	assert.Empty(t, run.notes)
}

func Test_MigrateLoggerGenerics_DefaultLoggerReusesTheStdImport(t *testing.T) {
	t.Parallel()

	content, _ := mw2Single(t, v3.MigrateLoggerGenerics, `package main

import (
	"log"

	fiberlog "github.com/gofiber/fiber/v2/log"
)

func Setup() {
	log.Println("std")
	_ = fiberlog.DefaultLogger()
}
`)

	assert.Contains(t, content, "fiberlog.DefaultLogger[*log.Logger]()")
	assert.NotContains(t, content, "stdlog")
}

func Test_MigrateLoggerGenerics_SeveralCustomLoggers(t *testing.T) {
	t.Parallel()

	run := mw2Apply(t, v3.MigrateLoggerGenerics, map[string]string{
		"a.go": `package main

import (
	"context"
	"io"
	stdlog "log"

	fiberlog "github.com/gofiber/fiber/v2/log"
)

type myLogger struct{ l *stdlog.Logger }
` + customLoggerMethods + `
func (m *myLogger) Logger() *stdlog.Logger                                { return m.l }
func (m *myLogger) WithContext(ctx context.Context) fiberlog.CommonLogger { return m }

var _ fiberlog.AllLogger = (*myLogger)(nil)
`,
		"b.go": `package main

import (
	"context"
	"io"

	fiberlog "github.com/gofiber/fiber/v2/log"
)

type otherLogger struct{}

func (m *otherLogger) Trace(v ...interface{})  {}
func (m *otherLogger) SetOutput(w io.Writer)   {}
func (m *otherLogger) WithContext(ctx context.Context) fiberlog.CommonLogger { return m }
func (m *otherLogger) Logger() int { return 0 }

func use() { _ = fiberlog.DefaultLogger() }
`,
	})

	assert.Contains(t, run.file(t, "a.go"), "var _ fiberlog.AllLogger[*stdlog.Logger] = (*myLogger)(nil)")
	assert.Contains(t, run.file(t, "b.go"), "fiberlog.DefaultLogger()", "no single type argument fits")
	require.Len(t, run.notes, 1)
	assert.Contains(t, run.notes[0].Message, "different types")
}

func Test_MigrateLoggerGenerics_SkipNonFiber(t *testing.T) {
	t.Parallel()

	src := `package main

import (
	otherlog "example.com/other/log"
)

var _ otherlog.AllLogger = (*customLogger)(nil)

func main() {
	logger := otherlog.DefaultLogger()
	otherlog.SetLogger(logger)
}
`
	content, run := mw2Single(t, v3.MigrateLoggerGenerics, src)

	assert.Equal(t, src, content)
	assert.Empty(t, run.out)
}

func Test_MigrateLoggerGenerics_Idempotent(t *testing.T) {
	t.Parallel()

	run := mw2Apply(t, v3.MigrateLoggerGenerics, map[string]string{"main.go": `package main

import (
	"context"
	"io"

	fiberlog "github.com/gofiber/fiber/v2/log"
)

type myLogger struct{}
` + customLoggerMethods + `
func (m *myLogger) WithContext(ctx context.Context) fiberlog.CommonLogger { return m }

var _ fiberlog.AllLogger = (*myLogger)(nil)

func Setup() { _ = fiberlog.DefaultLogger() }
`})
	first := run.file(t, "main.go")
	assert.Contains(t, run.out, "Migrating logger generics")

	second := mw2Again(t, v3.MigrateLoggerGenerics, run.dir)

	assert.Equal(t, first, second.file(t, "main.go"))
	assert.Empty(t, second.out)
}
