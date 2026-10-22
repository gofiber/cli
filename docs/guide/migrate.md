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
- A type that embeds `fiber.Ctx` and declares a method of the name a rewrite introduces
  (a `Redirect(to string)` or `Bind(v any)` of your own) is migrated through the embedded
  context: `c.Ctx.Redirect().Back("/")`.
- Where an explicit v3 setting reproduces the v2 default, the migration writes it:
  `IdleTimeout: 24 * time.Hour` for sessions, `ByteRange: true` for `SendFile`,
  `FailOnTimeout: true` for `app.Test`, the v2 expiration and `Cache-Control` defaults
  of the cache middleware.
- One migration can add a file: a filesystem root that is an `http.FileSystem` (packr,
  pkger, rice, statik, your own type) gets a small adapter, `fiber_migrate_httpfs.go`,
  declared once per package. Delete it once you serve an `embed.FS` or `os.DirFS`.

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
problem with a migration. A line number is the one the file had when that migration looked at it,
so it can be a few lines off for a file that later migrations changed.

The list has three kinds of entries:

- files that were skipped because they do not parse;
- v2 constructs that are still in the code and will not compile on v3, with the
  migration or the v3 API to use instead;
- behavior changes of the middleware your project uses that no rewrite can reproduce
  (proxy SSRF policy, timeout status, cache defaults, hashed basic auth passwords,
  `SendFile` file names, ...). These are reported once per project, at the first import.

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
