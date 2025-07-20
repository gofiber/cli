package main

import (
	"flag"
	"time"

	fileserver "github.com/gofiber/cli/cmd/fileserver/app"
	"github.com/gofiber/fiber/v3"
	fiberlog "github.com/gofiber/fiber/v3/log"
)

func main() {
	dir := flag.String("dir", ".", "directory to serve")
	addr := flag.String("addr", ":3000", "address to listen on")
	path := flag.String("path", "/", "request path to serve")
	enableLogger := flag.Bool("logger", true, "enable logger middleware")
	enableCors := flag.Bool("cors", false, "enable CORS middleware")
	enableHealth := flag.Bool("health", true, "enable health check endpoints")
	cert := flag.String("cert", "", "TLS certificate file")
	key := flag.String("key", "", "TLS private key file")
	browse := flag.Bool("browse", false, "enable directory browsing")
	download := flag.Bool("download", false, "force file downloads")
	compress := flag.Bool("compress", false, "enable compression")
	cache := flag.Duration("cache", 10*time.Second, "cache duration")
	maxAge := flag.Int("maxage", 0, "Cache-Control max-age header in seconds")
	index := flag.String("index", "index.html", "comma-separated list of index files")
	byteRange := flag.Bool("range", false, "enable byte range requests")
	prefork := flag.Bool("prefork", false, "enable prefork mode")
	disableStartup := flag.Bool("quiet", false, "disable startup message")
	flag.Parse()

	app := fileserver.NewApp(fileserver.Options{
		Dir:       *dir,
		Path:      *path,
		Logger:    *enableLogger,
		Cors:      *enableCors,
		Health:    *enableHealth,
		Browse:    *browse,
		Download:  *download,
		Compress:  *compress,
		Cache:     *cache,
		MaxAge:    *maxAge,
		Index:     *index,
		ByteRange: *byteRange,
	})

	cfg := fiber.ListenConfig{EnablePrefork: *prefork, DisableStartupMessage: *disableStartup}
	if *cert != "" && *key != "" {
		cfg.CertFile = *cert
		cfg.CertKeyFile = *key
	}

	if err := app.Listen(*addr, cfg); err != nil {
		fiberlog.Fatalf("failed to start server: %v", err)
	}
}
