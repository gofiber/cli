---
id: migrate
title: "\U0001F501 Migration"
sidebar_position: 10
---

The `fiber migrate` command upgrades an existing Fiber project to a newer
[github.com/gofiber/fiber](https://github.com/gofiber/fiber) release.
It determines the current version from your `go.mod` file and applies any
necessary migration steps.

Run it in the directory of the module. In the root of a repository that has no
`go.mod` of its own (several modules, with or without a `go.work`), it starts from the
oldest Fiber version the modules below it require and migrates all of them. It treats
the files below the directory as one project, so run it in each module when the
modules of a repository are on different Fiber versions.

## Usage

```bash
fiber migrate --to 3.0.0
```

## Flags

- `-t`, `--to` – Target version to migrate to. Defaults to the latest release
- `--hash` – Commit hash for the Fiber version when migrating to a pseudo version
- `--third-party` – Refresh third-party modules like `contrib`, `storage`, or `template`. Provide a comma-separated list (e.g. `--third-party=contrib,storage`) and append `@<commit>` to pin to a specific commit
- `-f`, `--force` – Force migration even if already on the target version
- `-s`, `--skip_go_mod` – Skip the `go.mod` work: `go mod tidy`, `go mod download` and `go mod vendor` (which only runs for a project that already has a `vendor` directory), and the version lookups and `go.mod` edits for contrib modules. The source files are still migrated
- `-v`, `--verbose` – Enable verbose output during migration
- `--include` – Comma-separated list of files to include in the migration. Supports glob and regex patterns
- `--exclude` – Comma-separated list of files to exclude from the migration. Supports glob and regex patterns

## What runs

The command reads the Fiber version your `go.mod` requires (the oldest one, when it
requires several majors at once) and picks the migrations by that version and by the
one you move to:

| You are on | You move to | What runs |
| --- | --- | --- |
| Fiber v2 | Fiber v3 (pre-releases included) | the v2 to v3 source rewrites, the `go.mod` and dependency work, the review |
| a v3 pre-release (alpha, beta, rc) | v3 | the clean-up of API that only the pre-releases had |
| Fiber v3 | a newer v3 | the `go.mod` and dependency work, plus the few rewrites a v3 minor needs (`RuleList` in 3.6) |

The v2 to v3 rewrites never run on a project that is already on v3. If a run was
interrupted after `go.mod` had been bumped, `--force` runs them again; every rewrite
is idempotent, so a second run changes nothing that the first one already migrated.

## What the rewrites touch

- A rewrite only looks at files that import the package whose API it changes, and it
  uses the name the file gives that import (`f "github.com/gofiber/fiber/v2"` is
  `f.Ctx`). Strings, comments and code of other packages that happen to use the same
  names (`session`, `static`, `utils`, `client`) are left alone.
- A file that does not parse is skipped and listed, and the run goes on with the next
  file. Fix the file and run the command again with `--force`.
- Where v3 has no faithful equivalent, the old code stays and a note says why. When
  dropping a setting would change what the program does without a compile error, the
  old setting stays on purpose, so that the build fails and points at it.
- A setting that v3 has no field for, but that a few lines of code reproduce, gets those
  lines. A token lookup with a prefix in front of the token (`AuthScheme` of the jwt
  middleware, `TokenPrefix` of paseto, on a header other than `Authorization` or in a
  cookie or the query) becomes an `extractors.FromCustom` that takes the prefix off the
  way v2 did, and a session `KeyLookup` that is only known when the program runs becomes
  a function that reads it the way the v2 constructor did.
- A type or a value that changed shape gets the conversion that hides it, wherever the
  value travels in the module (variables, parameters, results, fields, maps, channels,
  generics): the `Start` and `End` of the ranges `c.Range` returns are `int64` in v3, so
  the code that reads them gets `int(...)` and the code that writes them `int64(...)`;
  `session.Source` and its constants become `string` and the strings `"cookie"`,
  `"header"` and `"query"`, or the type `fibermigrate.SessionSource` where the program
  tells a `Source` from a string; `store.KeyLookup` and `store.CookieName` are read from
  `store.Extractor` (a write to them is dropped, v2 never changed where the store looks
  with it); `sess.Keys()` is converted to the `[]string` v2 returned. A removed method of
  the context that code uses as a value (`h := c.RedirectBack`, `f := c.ParamsInt`,
  `g := c.Range`) becomes a function literal with the v2 signature, bound to the receiver
  the way Go binds a method value; `c.ParamsInt` with a spread of defaults and
  `c.AllParams` become function literals as well.
- Stores, apps, clients and request contexts are followed across the packages of the
  module, also into files that do not import Fiber: a session store that an `auth` package
  exports gets its `defer sess.Release()` where handlers use it, `server.New().Listen(":3000")`
  in a `main` package that never imports Fiber gets the listener settings of the app, and a
  handler in a package that imports neither Fiber nor its middleware is migrated like one
  that takes `*fiber.Ctx` when its argument is a type another package declares as a
  wrapper that embeds `fiber.Ctx`, as an alias of it (`type Ctx = fiber.Ctx`), or when it
  comes from a function, a field, a generic type or a channel of another package. The file
  gets the imports the rewrite needs, and a wrapper that declares a method of its own that
  v3 uses (a `Redirect(to string)`) is called through the embedded context. A value whose
  type cannot be followed is reported where it calls a method that v3 removed. The other
  modules of the repository (a `go.work` workspace, a nested module, a `replace` to a
  directory below the one you run the command in) are read as part of the project; a module
  outside of that directory is not.
- Where a parameter, a variable, a constant or a type of a function hides the name of an
  import (`fiber := fiber.New()`, a parameter called `strconv`), the code the migration
  writes there is spelled with a second import of the package under a name nothing uses
  (`fiberv3`, `stdstrconv`), which is added only when it is used.
- The sentry-go API that versions 0.46 to 0.50 removed is rewritten to what the
  changelog recommends: `scope.SetExtra` becomes `SetContext`, `SetExtras` becomes
  `SetContexts`, `RemoveExtra` becomes `RemoveContext`, `event.Extra` becomes
  `Contexts`, `sentry.Logger` becomes `DebugLogger`, and a nil check of a client
  (`hub.Client() == nil`) becomes `!hub.Client().IsEnabled()`. What has no equivalent
  (the old metrics API, reserved context names) is reported.
- `Conn()` of the postgres storage returned a `*sql.DB`; in the v3 line it is a
  `*pgxpool.Pool`. The calls the migration can follow are wrapped in
  `fibermigrate.PostgresDB(...)`, which hands out one `*sql.DB` for each pool.
- A healthcheck config that another file of the package declares is split where it
  is declared: the liveness probe stays in the variable, the readiness probe moves to a
  second one next to it, and the calls become one route for each probe. A config that
  cannot be split (shared by several routes, returned by a function, kept in a struct
  field or a parameter, declared in another package, assigned field by field) keeps the
  v2 middleware, see `health.go` below.
- A type that embeds `fiber.Ctx` and declares a method of the name a rewrite introduces
  (a `Redirect(to string)` or `Bind(v any)` of your own) is migrated through the embedded
  context: `c.Ctx.Redirect().Back("/")`.
- Where an explicit v3 setting reproduces the v2 default, the migration writes it:
  `IdleTimeout: 24 * time.Hour` for sessions, `ByteRange: true` for `SendFile`,
  `FailOnTimeout: true` for `app.Test`, the v2 expiration and `Cache-Control` defaults
  of the cache middleware, `OnTimeout` returning `fiber.ErrRequestTimeout` for the
  timeout middleware (408 instead of 503), `Status(fiber.StatusFound)` for a redirect
  without a status (302 instead of 303). The 408 of a timeout goes to the `ErrorHandler`
  of the application once, as in v2.
- `c.Download(file[, name])` becomes `fibermigrate.Download(c, file[, name])`, the
  `Download` of v3 with the byte ranges and the `Content-Type` that v2 sent. `c.ParamsInt(key)`
  without a default gives the `0` that v2 returned together with the error, wherever the
  value is used.
- The jwt library the project uses for claims (`dgrijalva/jwt-go`, `form3tech-oss/jwt-go`,
  `golang-jwt/jwt` v3 and v4) moves to `golang-jwt/jwt/v5` in every file of a module that
  uses the jwt middleware, not only in the files that import the middleware, because the
  tokens of the middleware are v5 tokens. One note per module lists the libraries and the two
  differences in behavior (the text of the errors, tokens with an `iat` in the future), and
  what v5 removed (`claims.Valid()`, the `ValidationError` types, ...) is noted where it is
  used. `fiberi18n.GetMessage` and `MustGetMessage` of the v1 module become `Localize` and
  `MustLocalize` of the translator.
- A wrapper around a storage (a struct that embeds `fiber.Storage` and overrides `Get`,
  `Set`, `Delete` or `Reset`) gets the `GetWithContext`, `SetWithContext`,
  `DeleteWithContext` and `ResetWithContext` twins of the methods it overrides, which the
  middleware of v3 calls instead of them.
- A project that is pinned to an older release of Fiber v2 gets the rewrites for the API
  of that release: below v2.50 `c.GetReqHeaders()["X"]` (a `map[string]string` then) becomes
  `c.Get("X")` and `c.GetRespHeaders()["X"]` becomes `c.GetRespHeader("X")`, below v2.48
  the `Hooks().OnListen` handlers get the `fiber.ListenData` parameter, and below v2.23 the
  `StackTraceHandler` of the recover middleware gets the context.
- When `otelfiber` moves to its v3 module, the other OpenTelemetry modules of `go.mod`
  (`go.opentelemetry.io/otel*` and `go.opentelemetry.io/contrib*`) are raised with it to the
  release that module needs, so that `go mod tidy` can resolve them.
- The files are written through the formatter, so that what a migration adds is formatted
  like the rest, and then the text you wrote is put back wherever no migration changed it:
  CRLF line endings, the order and the spacing of your imports, comments written without a
  space. A file that cannot be merged safely is left as the migrations wrote it.

### Code that v3 has no counterpart for

Where no rewrite can be faithful, the migration adds the v2 behavior to your project
instead of changing what the program does. It goes to **one package of the module**,
`internal/fibermigrate` (package `fibermigrate`), that the first migration that needs it
creates and that every package of the module shares, so an agent or an app made in one
package can be used in another. Every file starts with a `Code generated by "fiber
migrate"; DO NOT EDIT.` line and says what it is and when to delete it, and the note of
each use names the file. A module path without a dot (`module myapp`) gets the import in
the standard library group, as `goimports` does. The files:

- `agent.go`: a copy of the v2 HTTP client agent (`fibermigrate.Get`, `fibermigrate.Agent`,
  `fibermigrate.Client`, `fibermigrate.FormFile`, ...) for the requests that the client
  package of v3 cannot make the way the agent did: a `Host` that differs from the URL,
  `Debug`, `SetResponse`, `Dest`, `RetryIf`, a header that the body encoder of v3
  replaces, a query that v2 replaced or discarded, a `FormFile` that two requests send,
  `MultipartForm` without a file, `FileData` or `SendFile` without `MultipartForm`, ...
  Every such request gets a note that says why, and every other request moves to the
  client package. A package that other packages import, and whose exported API uses the
  agent, the client or a form file, keeps all of its values on the copy so that the code
  that imports it keeps compiling. Delete the file once these requests use
  `github.com/gofiber/fiber/v3/client`;
- `listen.go`: the listener settings of `fiber.Config` (`Prefork`, `Network`,
  `DisableStartupMessage`, `EnablePrintRoutes`) move to the `Listen` call. When the config
  is built away from that call (a condition, a function that takes arguments, another
  package), `fibermigrate.NewApp` records the settings with the app and
  `fibermigrate.ListenConfig`, `Listen` and `Listener` hand them to the call that starts
  it, wherever it is; `fibermigrate.ConfigOf(app)` stands in for `app.Config()`.
  `fibermigrate.Config` (the `fiber.Config` of v2, with the four settings) and
  `fibermigrate.New` stand in for a config that does not go straight to `fiber.New`: it is
  returned by a function, handed to another one as a parameter, kept in a struct field or a
  variable of the package, written with an alias of `fiber.Config` (`type Config =
  fiber.Config`), or changed by the functions of a functional option (`type Option
  func(*fiber.Config)`). The migration follows the config through all of these, across
  packages and files, and every place that holds it takes the type of the registry, so the
  code that reads and assigns the settings is not touched; where it cannot follow the config
  (a collection, a channel, an interface, another module) the settings stay and the note says
  where it stopped. Delete the file once you pass a `fiber.ListenConfig` to `Listen`
  yourself. The settings of an app that no call of the module starts are dropped, with one
  note, unless the app goes to code the migration cannot see (a function of another module,
  an interface, a struct field), where it may be started;
- `ctx.go`: `LegacyCtx`, a `fiber.Ctx` that also has the methods v2 had and v3 removed
  (`BodyParser`, `QueryParser`, `ParamsInt`, `QueryInt`, `RedirectBack`, `UserContext`, ...),
  for an interface of your project that mirrors the context of v2. The note at the interface
  lists the methods v3 lacks, and every line that hands a context to it passes
  `fibermigrate.LegacyCtx{Ctx: c}` when the interface only asks for methods the adapter has.
  An interface that asks for a method v3 changed (`Bind`, `Context`, `Format`, `Range`,
  `Redirect`, `SendFile`), and a struct that embeds the context, get the note only. Delete the
  file once the interface describes what your handlers use;
- `download.go`: `Download`, the `c.Download` of v3 with the byte ranges and the
  `Content-Type` that `c.Download` of v2 sent. Delete the file if you do not need them;
- `health.go`: the v2 healthcheck middleware (`HealthConfig`, `Health`) for the configs
  that cannot be split into one route for each probe. Delete the file once the probes
  are registered one by one;
- `postgres.go`: `PostgresDB`, the one `*sql.DB` for each pool of the postgres storage;
- `session.go`: the type `SessionSource` and its constants, only for a module whose
  program tells a session `Source` from a string.

Values do not cross the boundary of a Go module: a nested module (a directory with a
`go.mod` of its own) gets its own `internal/fibermigrate`, and a config or an app that goes
from one module of the repository to another keeps its settings, with a note that says so,
because two copies of the package cannot exchange values. The adapter for a filesystem
root that is an `http.FileSystem` (packr, pkger, rice, statik, your own type),
`fiber_migrate_httpfs.go`, is still a file next to the code that uses it, once per
package; delete it once you serve an `embed.FS` or `os.DirFS`.

## What is left for you

At the end the command lists what it could not do for you, sorted by file and line:

```text
4 items to review by hand:
  handlers/files.go:41: SendFile takes the file name literally in Fiber v3 instead of percent-decoding it, ...
  main.go:3: proxy: Fiber v3 refuses upstreams that resolve to loopback or private addresses ...
  main.go:12: github.com/gofiber/fiber/v2/middleware/filesystem does not exist in Fiber v3 (replaced by the static middleware)
  main.go:27: MigrateConfigListenerFields: fiber.Config has no Prefork field in v3: set EnablePrefork in the fiber.ListenConfig of the call that starts this app ...
```

When a migration raised the entry, its name follows the position. Quote it when you report a
problem with a migration. A line number is the line the file has when the command ends, whatever
the rewrites that came after the migration did to the code above it; where a rewrite replaced the
code the entry is about, it is the first line of what replaced it. Several findings on one line are
one entry, and the others follow it with `also:`. A finding that the command tells the project once,
at the first place, names the others: `(2 more in this project: a.go:14, sub/b.go:6)`, and
`and 3 more` after the fifth.

The list has four kinds of entries:

- files that were skipped because they do not parse;
- v2 constructs that are still in the code and will not compile on v3, with the
  migration or the v3 API to use instead;
- dependencies that are built for Fiber v2 and stay in the program: their handlers take the
  `*fiber.Ctx` of v2 and do not work with a v3 app. There is one entry for each module, at
  the first place the program imports it (not a test), with the module that imports Fiber v2.
  When `go.mod` was not tidied (`--skip_go_mod`, or `go mod tidy` failed) the requirements
  whose `go.mod` is in the module cache are looked at instead and the entry says `probably`;
- behavior changes of the middleware your project uses that no rewrite can reproduce
  (proxy SSRF policy, a timed-out handler that keeps running, cache defaults, hashed
  basic auth passwords, `SendFile` file names, the random token that replaces the UUID as
  the default session ID, no proxy being trusted unless `TrustProxy` is set, the charset that
  `c.JSON` adds to the `Content-Type`, the body of a 404, the text of the errors of the
  binders, what a `go` directive that jumps several releases switches on, ...). These are
  reported once per project, at the first import. A test of the project that compares a
  `Content-Type` or the body of a 404 gets an entry at the line of the comparison.
  A few rewrites report their own difference the same way: the error of `ParamsInt`
  loses the `failed to convert: ` v2 put in front of it, the deprecated `timeout.New`
  of v2 only logged the error of its handler, `Redirect().Back` follows a `Referer`
  of the same origin only, each healthcheck probe is a route of its own (HEAD is
  answered, another method gets 405), and the v3 client adds `Accept: application/json`
  to a JSON body, encodes a space of a query as `+` and returns one error where the
  agent returned a slice.

A construct that v3 refuses on purpose is left as it is and the note says why: the csrf
middleware of v2 compared a token that came from the CSRF cookie with that same cookie
(`KeyLookup: "cookie:csrf_"`), which protected nothing, and v3 panics on it; use a header
or a form field instead.

An empty list means the migrations found nothing left to report. It does not replace
building the project and running its tests.

## Examples

Upgrade to the latest Fiber version:

```bash
fiber migrate
```

Migrate to a specific release:

```bash
fiber migrate --to 3.0.0
```

Use a commit hash for unreleased versions:

```bash
fiber migrate --to 3.0.0 --hash abcdef123456
```

Refresh contrib packages to their latest versions:

```bash
fiber migrate --third-party=contrib
```

Refresh template packages to a specific commit:

```bash
fiber migrate --third-party=template@abcdef123456
```

Refresh both contrib and storage packages at once:

```bash
fiber migrate --third-party=contrib,storage
```

Force re-running migrations even if the version matches:

```bash
fiber migrate --to 3.0.0 --force
```

Skip `go mod` commands for offline environments:

```bash
fiber migrate --skip_go_mod
```

Verbose mode prints detailed progress and executed steps:

```bash
fiber migrate --verbose
```

Limit the migration to specific files:

```bash
fiber migrate --include=internal/** --exclude="*_test.go"
```
