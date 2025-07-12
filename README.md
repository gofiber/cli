# cli

Fiber Command Line Interface

[![Packaging status](https://repology.org/badge/vertical-allrepos/fiber-cli.svg)](https://repology.org/project/fiber-cli/versions)

## Installation

Requires Go 1.24 or later.

```bash
go install github.com/gofiber/cli/fiber@latest
```

## Commands

## fiber

### Synopsis

🚀 Fiber is an Express inspired web framework written in Go with 💖

Learn more on [gofiber.io](https://gofiber.io)

CLI version v0.0.x

### Options

```text
  -h, --help   help for fiber
```

## fiber dev

### Synopsis

Rerun the fiber project if watched files changed

```bash
fiber dev [flags]
```

### Examples

```bash
  fiber dev --pre-run="command1 flag,command2 flag"
  Pre run specific commands before running the project
```

### Options

```text
  -a, --args strings            arguments for exec
  -d, --delay duration          delay to trigger rerun (default 1s)
  -D, --exclude_dirs strings    ignore these directories (default [assets,tmp,vendor,node_modules])
  -F, --exclude_files strings   ignore these files
  -e, --extensions strings      file extensions to watch (default [go,tmpl,tpl,html])
  -h, --help                    help for dev
  -p, --pre-run strings         pre run commands, see example for more detail
  -r, --root string             root path for watch, all files must be under root (default ".")
  -t, --target string           target path for go build (default ".")
```

## fiber new

### Synopsis

Generate a new fiber project

```bash
fiber new PROJECT [module name] [flags]
```

### Examples

```bash
  fiber new fiber-demo
  Generates a project with go module name fiber-demo

  fiber new fiber-demo your.own/module/name
  Specific the go module name

  fiber new fiber-demo -t=complex
  Generate a complex project

  fiber new fiber-demo -t complex -r githubId/repo
  Generate project based on Github repo

  fiber new fiber-demo -t complex -r https://anyProvider.com/username/repo.git
  Generate project based on repo outside Github with https

  fiber new fiber-demo -t complex -r git@anyProvider.com:id/repo.git
  Generate project based on repo outside Github with ssh
```

### Options

```text
  -h, --help              help for new
  -r, --repo string       complex boilerplate repo name in github or other repo url (default "gofiber/boilerplate")
  -t, --template string   basic|complex (default "basic")
```

## fiber migrate

### Synopsis

Migrate Fiber project version to a newer version

```bash
fiber migrate --to 3.0.0
```

### Options

```text
  -t, --to string   Migrate to a specific version e.g:3.0.0 Format: X.Y.Z
  -h, --help        help for migrate
```

## fiber upgrade

### Synopsis

Upgrade Fiber cli if a newer version is available

```bash
fiber upgrade [flags]
```

### Options

```text
  -h, --help   help for upgrade
```

## fiber version

### Synopsis

Print the local and released version number of fiber

```bash
fiber version [flags]
```

### Options

```text
  -h, --help   help for version
```
