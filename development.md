# Local Development

Install [reflex](github.com/cespare/reflex)
```bash
go install github.com/cespare/reflex@latest
```

Use reflex to watch for changes in the project and recompile the binary
```bash
reflex -r '\.go$' -- go install ./fiber  
```

Test the binary in fiber project
```bash
cd my-fiber-project
fiber version
```