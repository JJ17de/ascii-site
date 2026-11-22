// Command server serves the ascii-site static site from memory, with
// hash-based CSP, precompressed responses and Kubernetes/Prometheus
// operational endpoints. The site is embedded at build time from ./web, or
// read from a directory given by -dir / SITE_DIR.
package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

//go:embed all:web
var embedded embed.FS

// Set at build time with -ldflags "-X main.version=... -X main.commit=...".
var (
	version = "dev"
	commit  = "none"
)

const shutdownTimeout = 10 * time.Second

type config struct {
	addr  string
	dir   string
	hsts  bool
	drain time.Duration
}

// parseConfig reads flags, falling back to the PORT and SITE_DIR
// environment variables for their defaults.
func parseConfig(args []string, getenv func(string) string, output io.Writer) (config, error) {
	var c config
	fset := flag.NewFlagSet("server", flag.ContinueOnError)
	fset.SetOutput(output)

	defAddr := ":8080"
	if p := getenv("PORT"); p != "" {
		defAddr = ":" + p
	}
	fset.StringVar(&c.addr, "addr", defAddr, "listen address (default from $PORT, else :8080)")
	fset.StringVar(&c.dir, "dir", getenv("SITE_DIR"), "serve the site from this directory instead of the embedded copy (default $SITE_DIR)")
	fset.BoolVar(&c.hsts, "hsts", false, "send Strict-Transport-Security (only when served over HTTPS)")
	fset.DurationVar(&c.drain, "drain", 0, "on SIGTERM, report not-ready for this long before closing the listener (e.g. 5s behind a Kubernetes Service)")
	if err := fset.Parse(args); err != nil {
		return c, err
	}
	if fset.NArg() > 0 {
		return c, fmt.Errorf("unexpected arguments: %q", fset.Args())
	}
	return c, nil
}

// siteFS returns the filesystem to serve and a description of its source.
func siteFS(dir string) (fs.FS, string, error) {
	if dir == "" {
		sub, err := fs.Sub(embedded, "web")
		return sub, "embedded", err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return nil, "", err
	}
	if !info.IsDir() {
		return nil, "", fmt.Errorf("%s is not a directory", dir)
	}
	return os.DirFS(dir), dir, nil
}

func newHTTPServer(h http.Handler, logger *slog.Logger) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(os.Args[1:], os.Getenv, logger); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		logger.Error("fatal", "err", err.Error())
		os.Exit(1)
	}
}

func run(args []string, getenv func(string) string, logger *slog.Logger) error {
	cfg, err := parseConfig(args, getenv, os.Stderr)
	if err != nil {
		return err
	}

	fsys, source, err := siteFS(cfg.dir)
	if err != nil {
		return fmt.Errorf("site source: %w", err)
	}
	site, err := LoadSite(fsys)
	if err != nil {
		return fmt.Errorf("refusing to load site from %s: %w", source, err)
	}
	if site.Lookup(indexFile) == nil {
		logger.Warn("index.html missing; /readyz will report 503", "source", source)
	}

	srv := NewServer(site, Options{Logger: logger, HSTS: cfg.hsts, Version: version, Commit: commit})

	ln, err := net.Listen("tcp", cfg.addr)
	if err != nil {
		return err
	}
	logger.Info("starting",
		"addr", ln.Addr().String(),
		"version", version,
		"commit", commit,
		"files", site.Len(),
		"source", source,
		"hsts", cfg.hsts)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		// After the first signal, restore default handling so a second
		// one terminates immediately.
		<-ctx.Done()
		stop()
	}()
	return serve(ctx, ln, srv, logger, cfg.drain)
}

// serve runs the HTTP server on ln until ctx is cancelled, then flips
// readiness, keeps serving for the drain period so load balancers and
// readiness probes can see the 503 and stop routing, and finally shuts
// down gracefully within shutdownTimeout.
func serve(ctx context.Context, ln net.Listener, s *Server, logger *slog.Logger, drain time.Duration) error {
	hs := newHTTPServer(s.Handler(), logger)
	errc := make(chan error, 1)
	go func() { errc <- hs.Serve(ln) }()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down", "drain", drain.String(), "timeout", shutdownTimeout.String())
	s.BeginShutdown()
	if drain > 0 {
		select {
		case <-time.After(drain):
		case err := <-errc:
			return err
		}
	}
	shCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := hs.Shutdown(shCtx); err != nil {
		_ = hs.Close()
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	logger.Info("server stopped")
	return nil
}
