package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	fileserver "github.com/gofiber/cli/cmd/fileserver/app"
	"github.com/gofiber/fiber/v3"
)

var (
	serveDir       string
	serveAddr      string
	servePath      string
	serveLogger    bool
	serveCors      bool
	serveHealth    bool
	serveCert      string
	serveKey       string
	serveBrowse    bool
	serveDownload  bool
	serveCompress  bool
	serveCache     time.Duration
	serveMaxAge    int
	serveIndex     string
	serveByteRange bool
	servePrefork   bool
	serveQuiet     bool
)

func init() {
	serveCmd.Flags().StringVar(&serveDir, "dir", ".", "directory to serve")
	serveCmd.Flags().StringVar(&serveAddr, "addr", ":3000", "address to listen on")
	serveCmd.Flags().StringVar(&servePath, "path", "/", "request path to serve")
	serveCmd.Flags().BoolVar(&serveLogger, "logger", true, "enable logger middleware")
	serveCmd.Flags().BoolVar(&serveCors, "cors", false, "enable CORS middleware")
	serveCmd.Flags().BoolVar(&serveHealth, "health", true, "enable health check endpoints")
	serveCmd.Flags().StringVar(&serveCert, "cert", "", "TLS certificate file")
	serveCmd.Flags().StringVar(&serveKey, "key", "", "TLS private key file")
	serveCmd.Flags().BoolVar(&serveBrowse, "browse", false, "enable directory browsing")
	serveCmd.Flags().BoolVar(&serveDownload, "download", false, "force file downloads")
	serveCmd.Flags().BoolVar(&serveCompress, "compress", false, "enable compression")
	serveCmd.Flags().DurationVar(&serveCache, "cache", 10*time.Second, "cache duration")
	serveCmd.Flags().IntVar(&serveMaxAge, "maxage", 0, "Cache-Control max-age header in seconds")
	serveCmd.Flags().StringVar(&serveIndex, "index", "index.html", "comma-separated list of index files")
	serveCmd.Flags().BoolVar(&serveByteRange, "range", false, "enable byte range requests")
	serveCmd.Flags().BoolVar(&servePrefork, "prefork", false, "enable prefork mode")
	serveCmd.Flags().BoolVar(&serveQuiet, "quiet", false, "disable startup message")

	rootCmd.AddCommand(serveCmd)
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Serve static files",
	RunE:  serveRunE,
}

var listen = func(app *fiber.App, addr string, cfg fiber.ListenConfig) error {
	return app.Listen(addr, cfg)
}

func serveRunE(_ *cobra.Command, _ []string) error {
	app := fileserver.NewApp(fileserver.Options{
		Dir:       serveDir,
		Path:      servePath,
		Logger:    serveLogger,
		Cors:      serveCors,
		Health:    serveHealth,
		Browse:    serveBrowse,
		Download:  serveDownload,
		Compress:  serveCompress,
		Cache:     serveCache,
		MaxAge:    serveMaxAge,
		Index:     serveIndex,
		ByteRange: serveByteRange,
	})

	cfg := fiber.ListenConfig{EnablePrefork: servePrefork, DisableStartupMessage: serveQuiet}
	if serveCert != "" && serveKey != "" {
		cfg.CertFile = serveCert
		cfg.CertKeyFile = serveKey
	}

	if err := listen(app, serveAddr, cfg); err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}
	return nil
}
