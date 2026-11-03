---
id: fileserver
title: "📂 File Server"
sidebar_position: 9
---

Fiber ships with a configurable file server located in the `cmd/fileserver`
directory. It can serve static files from any directory and supports TLS for
secure connections. The server is also available via the `fiber serve` command.

You can still run the program directly with `go run ./cmd/fileserver` or build
it using `go build -o fileserver ./cmd/fileserver`.

## Usage

Run the command specifying the directory and optional TLS certificate
and key files:

```bash
fiber serve -dir ./public -addr :8443 -cert cert.pem -key key.pem
```

### Flags

- `-dir` – directory to serve (default `.`)
- `-addr` – address to listen on (default `:3000`)
- `-path` – URL path to mount the directory (default `/`)
- `-logger` – enable request logging (default `true`)
- `-cors` – enable CORS middleware
- `-health` – expose `/livez`, `/readyz` and `/startupz` endpoints (default `true`)
- `-cert` – path to TLS certificate file
- `-key` – path to TLS private key file
- `-browse` – enable directory listing
- `-download` – force file downloads instead of in-browser viewing
- `-compress` – enable serving of pre-compressed assets
- `-cache` – cache duration (e.g. `30s`)
- `-maxage` – Cache-Control max-age in seconds
- `-index` – comma-separated list of index files
- `-range` – enable byte range requests
- `-prefork` – start server in prefork mode
- `-quiet` – disable the startup banner

When both `-cert` and `-key` are provided the server automatically enables TLS.
