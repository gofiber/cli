package main

import (
	"time"

	fileserver "github.com/gofiber/cli/cmd/fileserver/app"
	"github.com/gofiber/fiber/v3"
	fiberlog "github.com/gofiber/fiber/v3/log"
	"github.com/spf13/pflag"
)

func main() {
	dir := pflag.String("dir", ".", "directory to serve")
	addr := pflag.String("addr", ":3000", "address to listen on")
	path := pflag.String("path", "/", "request path to serve")
	enableLogger := pflag.Bool("logger", true, "enable logger middleware")
	enableCors := pflag.Bool("cors", false, "enable CORS middleware")
	enableHealth := pflag.Bool("health", true, "enable health check endpoints")
	cert := pflag.String("cert", "", "TLS certificate file")
	key := pflag.String("key", "", "TLS private key file")
	browse := pflag.Bool("browse", false, "enable directory browsing")
	download := pflag.Bool("download", false, "force file downloads")
	compress := pflag.Bool("compress", false, "enable compression")
	cache := pflag.Duration("cache", 10*time.Second, "cache duration")
	maxAge := pflag.Int("maxage", 0, "Cache-Control max-age header in seconds")
	index := pflag.String("index", "index.html", "comma-separated list of index files")
	byteRange := pflag.Bool("range", false, "enable byte range requests")
	prefork := pflag.Bool("prefork", false, "enable prefork mode")
	disableStartup := pflag.Bool("quiet", false, "disable startup message")
	pflag.Parse()

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
