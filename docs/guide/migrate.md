---
id: migrate
title: "\U0001F501 Migration"
sidebar_position: 10
---

The `fiber migrate` command upgrades an existing Fiber project to a newer
[github.com/gofiber/fiber](https://github.com/gofiber/fiber) release.
It determines the current version from your `go.mod` file and applies any
necessary migration steps.

## Usage

```bash
fiber migrate --to 3.0.0
```

## Flags

- `-t`, `--to` – Target version to migrate to. Defaults to the latest release
- `--hash` – Commit hash for the Fiber version when migrating to a pseudo version
- `--third-party` – Refresh third-party modules like `contrib`, `storage`, or `template`. Provide a comma-separated list (e.g. `--third-party=contrib,storage`) and append `@<commit>` to pin to a specific commit
- `-f`, `--force` – Force migration even if already on the target version
- `-s`, `--skip_go_mod` – Skip running `go mod tidy`, `go mod download`, and `go mod vendor`
- `-v`, `--verbose` – Enable verbose output during migration

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
