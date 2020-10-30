# cli
Fiber Command Line Interface

# Installation
```bash
go get -u github.com/gofiber/cli/fiber
```

# Commands
## fiber
### Synopsis

🚀 Fiber is an Express inspired web framework written in Go with 💖
 
Learn more on https://gofiber.io
 
CLI version v0.0.x

### Options

```
  -h, --help   help for fiber
```

## fiber dev
### Synopsis

Rerun the fiber project if watched files changed

```
fiber dev [flags]
```

### Options

```
  -d, --delay duration          delay to trigger rerun (default 1s)
  -D, --exclude_dirs strings    ignore these directories (default [assets,tmp,vendor,node_modules])
  -F, --exclude_files strings   ignore these files
  -e, --extensions strings      file extensions to watch (default [go,tmpl,tpl,html])
  -h, --help                    help for dev
  -r, --root string             root path for watch, all files must be under root (default ".")
  -t, --target string           target path for go build (default ".")
```

## fiber new
### Synopsis

Generate a new fiber project

```
fiber new PROJECT [module name] [flags]
```

### Examples

```
  fiber new fiber-demo
    Generates a project with go module name fiber-demo

  fiber new fiber-demo your.own/module/name
    Specific the go module name

  fiber new fiber-demo -t=complex
    Generate a complex project
```

### Options

```
  -h, --help              help for new
  -r, --repo string       complex boilerplate repo name in github (default "https://github.com/gofiber/boilerplate")
  -t, --template string   basic|complex (default "basic")
```

## fiber upgrade
### Synopsis

Upgrade Fiber cli if a newer version is available

```
fiber upgrade [flags]
```

### Options

```
  -h, --help   help for upgrade
```

## fiber version
### Synopsis

Print the local and released version number of fiber

```
fiber version [flags]
```

### Options

```
  -h, --help   help for version
```
