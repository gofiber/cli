# cli

Fiber Command Line Interface

[![Packaging status](https://repology.org/badge/vertical-allrepos/fiber-cli.svg)](https://repology.org/project/fiber-cli/versions)

## Installation

Requires Go 1.25 or later.

```bash
go install github.com/gofiber/cli/fiber@latest
```

## Commands

The Fiber CLI provides several commands to enhance development workflows:

- `fiber dev` – Rerun the project whenever watched files change
- `fiber serve` – Serve static files with optional TLS and caching
- `fiber new` – Generate a new Fiber project from templates
- `fiber migrate` – Migrate an existing project to a newer Fiber version
- `fiber upgrade` – Upgrade the CLI itself to the latest release
- `fiber version` – Print the local and latest available CLI versions

## fiber

### Synopsis

🚀 Fiber is an Express inspired web framework written in Go with 💖

Learn more on [gofiber.io](https://gofiber.io)

CLI version detected using Go build info

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

## fiber serve

### Synopsis

Serve static files

See the [File server guide](docs/guide/fileserver.md) for more details.

```bash
fiber serve [flags]
```

### Options

```text
      --addr string      address to listen on (default ":3000")
      --browse           enable directory browsing
      --cache duration   cache duration (default 10s)
      --cert string      TLS certificate file
      --compress         enable compression
      --cors             enable CORS middleware
      --dir string       directory to serve (default ".")
      --download         force file downloads
      --health           enable health check endpoints (default true)
      --index string     comma-separated list of index files (default "index.html")
      --key string       TLS private key file
      --logger           enable logger middleware (default true)
      --maxage int       Cache-Control max-age header in seconds
      --path string      request path to serve (default "/")
      --prefork          enable prefork mode
      --quiet            disable startup message
      --range            enable byte range requests
  -h, --help             help for serve
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

See the [Migration guide](docs/guide/migrate.md) for more details.

```bash
fiber migrate --to 3.0.0
```

### Options

```text
  -t, --to string        Migrate to a specific version e.g:3.0.0 Format: X.Y.Z
  -f, --force            Force migration even if already on the version
  -s, --skip_go_mod      Skip running go mod tidy, download and vendor
      --hash string      Commit hash for Fiber version
      --third-party strings   Refresh third-party modules (contrib,storage,template). Provide a comma-separated list and optionally append @<commit> to pin a commit
  -v, --verbose          Enable verbose output
  -h, --help             help for migrate
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

Print the local and released version number of Fiber and the CLI

```bash
fiber version [flags]
```

### Options

```text
  -h, --help   help for version
```

<!-- skip-docs -->
## ☕ Supporters

Fiber is an open-source project that runs on donations to pay the bills, e.g., our domain name, hosting, and serverless infrastructure. If you want to support Fiber, please become a [GitHub Sponsor](https://github.com/sponsors/gofiber).

<p align="center">
  <a href="https://www.coderabbit.ai/?utm_source=gofiber&utm_medium=sponsor&utm_content=readme">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="https://www.coderabbit.ai/images/logo-dark.svg">
      <img width="280" height="52" alt="CodeRabbit" src="https://www.coderabbit.ai/images/logo-orange.svg">
    </picture>
  </a>
</p>
<p align="center">
  <a href="https://blacksmith.sh/?utm_source=gofiber&utm_medium=sponsor&utm_content=readme">
    <img width="280" height="96" alt="Blacksmith" src="https://raw.githubusercontent.com/gofiber/.github/main/assets/sponsors/blacksmith.png">
  </a>
</p>

<!-- sponsors -->

### 📅 Monthly Sponsors

<table>
<tr><td valign="top"><strong>🔥 Fiber Guardian</strong></td><td><a href="https://www.coderabbit.ai/?utm_source=cr_org&amp;utm_medium=github" title="@coderabbitai"><img src="https://github.com/coderabbitai.png" width="50" alt="@coderabbitai" /></a></td></tr>
<tr><td valign="top"><strong>☕ Fiber Supporter</strong></td><td><a href="https://ndole.studio" title="@NdoleStudio"><img src="https://github.com/NdoleStudio.png" width="34" alt="@NdoleStudio" /></a>&nbsp;<a href="https://cyberapper.ai" title="@petercool"><img src="https://github.com/petercool.png" width="34" alt="@petercool" /></a></td></tr>
<tr><td valign="top"><strong>🪴 Fiber Friend</strong></td><td><a href="https://github.com/simonheisstpeter" title="@simonheisstpeter"><img src="https://github.com/simonheisstpeter.png" width="32" alt="@simonheisstpeter" /></a></td></tr>
</table>

### 🎁 One-time Sponsors

<table>
<tr><td valign="top"><strong>🚀 Fiber Hero</strong></td><td><a href="https://www.thanks.dev" title="@thnxdev"><img src="https://github.com/thnxdev.png" width="40" alt="@thnxdev" /></a></td></tr>
<tr><td valign="top"><strong>🪴 Fiber Friend</strong></td><td><a href="https://github.com/Gl1tchedPixzl" title="@Gl1tchedPixzl"><img src="https://github.com/Gl1tchedPixzl.png" width="26" alt="@Gl1tchedPixzl" /></a></td></tr>
</table>
<!-- sponsors -->
<!-- skip-docs -->
