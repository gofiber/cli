# Local Development

Install [air](https://github.com/cosmtrek/air)

```bash
go install github.com/cosmtrek/air@latest
```

Use air to watch for changes in the project and recompile the binary

```bash
air --build.cmd="go install ./fiber"
```

Test the binary in fiber project

```bash
cd my-fiber-project
fiber version
```
