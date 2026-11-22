package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Normalised route label values. These are the only values the "route"
// metric label can take.
const (
	routeRoot     = "/"
	routeLab      = "/lab"
	routeHealthz  = "/healthz"
	routeReadyz   = "/readyz"
	routeMetrics  = "/metrics"
	routeStatic   = "static"
	routeNotFound = "notfound"
)

const maxLoggedPathLen = 256

// Options configures a Server.
type Options struct {
	Logger  *slog.Logger
	HSTS    bool
	Version string
	Commit  string
}

// Server serves a Site plus the health, readiness and metrics endpoints.
type Server struct {
	log      *slog.Logger
	hsts     bool
	metrics  *Metrics
	site     atomic.Pointer[Site]
	shutting atomic.Bool
}

// NewServer returns a Server for site. site may be nil, in which case only
// the ops endpoints answer and readiness reports 503.
func NewServer(site *Site, opts Options) *Server {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{
		log:     logger,
		hsts:    opts.HSTS,
		metrics: NewMetrics(opts.Version, opts.Commit),
	}
	s.site.Store(site)
	return s
}

// BeginShutdown makes /readyz report 503 so load balancers stop routing new
// traffic while in-flight requests drain.
func (s *Server) BeginShutdown() { s.shutting.Store(true) }

// Ready reports whether the site is loaded, has an index page and the
// server is not shutting down.
func (s *Server) Ready() bool {
	return !s.shutting.Load() && s.site.Load().Lookup(indexFile) != nil
}

// Handler returns the complete, instrumented HTTP handler.
func (s *Server) Handler() http.Handler { return s.wrap(s.serve) }

// routeHandler is the inner handler; the middleware resolves the route and
// file once and passes them in.
type routeHandler func(w http.ResponseWriter, r *http.Request, route string, f *file)

// resolve maps a URL path to its normalised route label and, for site
// files, the file to serve. Lookups only ever hit the in-memory map built at
// load time, so no request path can reach the underlying filesystem.
func resolve(site *Site, urlPath string) (string, *file) {
	switch urlPath {
	case routeHealthz, routeReadyz, routeMetrics:
		return urlPath, nil
	}
	name, ok := fileName(urlPath)
	if !ok {
		return routeNotFound, nil
	}
	f := site.Lookup(name)
	switch {
	case f == nil:
		return routeNotFound, nil
	case name == indexFile:
		return routeRoot, f
	case strings.HasPrefix(name, "lab/"):
		return routeLab, f
	default:
		return routeStatic, f
	}
}

// fileName converts a decoded URL path to an FS path, rejecting anything
// that is not a clean, relative, slash-separated path: "..", ".", empty
// segments, trailing slashes (directories), backslashes and NUL bytes.
func fileName(urlPath string) (string, bool) {
	if urlPath == "/" {
		return indexFile, true
	}
	if !strings.HasPrefix(urlPath, "/") {
		return "", false
	}
	name := urlPath[1:]
	if strings.ContainsAny(name, "\\\x00") || !fs.ValidPath(name) || name == "." {
		return "", false
	}
	return name, true
}

// wrap is the outer middleware: security headers, in-flight tracking,
// panic recovery, metrics and the per-request access log.
func (s *Server) wrap(next routeHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		s.metrics.inFlight.Add(1)
		defer s.metrics.inFlight.Add(-1)

		route, f := resolve(s.site.Load(), r.URL.Path)
		rec := &recorder{ResponseWriter: w}
		setSecurityHeaders(w.Header(), s.hsts)

		defer func() {
			p := recover()
			err, _ := p.(error)
			abort := errors.Is(err, http.ErrAbortHandler)
			if p != nil && !abort {
				s.log.Error("panic serving request",
					"method", truncate(r.Method, 16),
					"path", truncate(r.URL.Path, maxLoggedPathLen),
					"panic", fmt.Sprint(p),
					"stack", string(debug.Stack()))
				if !rec.wroteHeader {
					s.writeError(rec, r, http.StatusInternalServerError, "internal server error\n")
				}
			}
			s.finish(r, route, rec, start)
			if abort {
				panic(p) // let net/http abort the connection quietly
			}
		}()

		next(rec, r, route, f)
	})
}

// finish records metrics and writes the access log line. Client IPs and
// user agents are deliberately not logged.
func (s *Server) finish(r *http.Request, route string, rec *recorder, start time.Time) {
	d := time.Since(start)
	status := rec.statusCode()
	s.metrics.observe(route, normalizeMethod(r.Method), status, rec.bytes, d)
	s.log.LogAttrs(r.Context(), slog.LevelInfo, "request",
		slog.String("method", truncate(r.Method, 16)),
		slog.String("path", truncate(r.URL.Path, maxLoggedPathLen)),
		slog.String("route", route),
		slog.Int("status", status),
		slog.Int64("bytes", rec.bytes),
		slog.Float64("duration_ms", float64(d.Microseconds())/1000),
	)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request, route string, f *file) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		s.writeError(w, r, http.StatusMethodNotAllowed, "method not allowed\n")
		return
	}
	switch route {
	case routeHealthz:
		writePlain(w, r, http.StatusOK, "ok\n")
	case routeReadyz:
		if s.Ready() {
			writePlain(w, r, http.StatusOK, "ready\n")
		} else {
			writePlain(w, r, http.StatusServiceUnavailable, "not ready\n")
		}
	case routeMetrics:
		s.serveMetrics(w, r)
	case routeNotFound:
		s.writeError(w, r, http.StatusNotFound, "not found\n")
	default:
		s.serveFile(w, r, f)
	}
}

func (s *Server) serveMetrics(w http.ResponseWriter, r *http.Request) {
	var buf bytes.Buffer
	if _, err := s.metrics.Render(&buf, s.site.Load().Len()); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal server error\n")
		return
	}
	h := w.Header()
	h.Set("Content-Type", metricsContentType)
	h.Set("Cache-Control", cacheNoStore)
	h.Set("Content-Length", strconv.Itoa(buf.Len()))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(buf.Bytes())
	}
}

func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, f *file) {
	h := w.Header()
	h.Set("Content-Security-Policy", f.csp)
	h.Set("Cache-Control", cacheNoCache)
	h.Set("Accept-Ranges", "none")

	body, etag := f.body, f.etag
	if f.gz != nil {
		h.Set("Vary", "Accept-Encoding")
		if acceptsGzip(r.Header.Values("Accept-Encoding")) {
			body, etag = f.gz, f.gzETag
			h.Set("Content-Encoding", "gzip")
		}
	}
	h.Set("ETag", etag)

	if inm := strings.Join(r.Header.Values("If-None-Match"), ","); inm != "" && etagMatches(inm, etag) {
		// A 304 carries validators and cache headers but no representation
		// metadata such as Content-Encoding.
		h.Del("Content-Encoding")
		w.WriteHeader(http.StatusNotModified)
		return
	}

	h.Set("Content-Type", f.contentType)
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

// writePlain writes a short uncacheable text/plain response.
func writePlain(w http.ResponseWriter, r *http.Request, status int, body string) {
	h := w.Header()
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("Cache-Control", cacheNoStore)
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(body))
	}
}

// writeError writes an error response, first clearing any file-specific
// headers a handler may have set before failing.
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, status int, body string) {
	h := w.Header()
	for _, k := range []string{"Content-Encoding", "ETag", "Vary", "Accept-Ranges"} {
		h.Del(k)
	}
	h.Set("Content-Security-Policy", cspNonHTML)
	writePlain(w, r, status, body)
}

// acceptsGzip reports whether the Accept-Encoding header values permit a
// gzip response (RFC 9110 section 12.5.3), honouring q-values and "*".
func acceptsGzip(values []string) bool {
	gzipQ, starQ := -1.0, -1.0
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			coding, params, _ := strings.Cut(part, ";")
			coding = strings.ToLower(strings.TrimSpace(coding))
			q := 1.0
			for _, p := range strings.Split(params, ";") {
				k, val, ok := strings.Cut(strings.TrimSpace(p), "=")
				if ok && strings.EqualFold(strings.TrimSpace(k), "q") {
					f, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
					if err != nil || !(f >= 0 && f <= 1) { // also rejects NaN
						f = 0
					}
					q = f
				}
			}
			switch coding {
			case "gzip", "x-gzip":
				gzipQ = max(gzipQ, q)
			case "*":
				starQ = max(starQ, q)
			}
		}
	}
	if gzipQ >= 0 {
		return gzipQ > 0
	}
	return starQ > 0
}

// etagMatches implements the weak comparison used by If-None-Match: it
// reports whether any entity tag in header (or "*") matches etag.
func etagMatches(header, etag string) bool {
	for {
		header = strings.TrimLeft(header, " \t,")
		if header == "" {
			return false
		}
		if header[0] == '*' {
			return true
		}
		header = strings.TrimPrefix(header, "W/")
		if header == "" || header[0] != '"' {
			return false // malformed
		}
		end := strings.IndexByte(header[1:], '"')
		if end < 0 {
			return false
		}
		if header[:end+2] == etag {
			return true
		}
		header = header[end+2:]
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// recorder captures the status code and body size for metrics and logs.
type recorder struct {
	http.ResponseWriter
	status      int
	bytes       int64
	wroteHeader bool
}

func (r *recorder) WriteHeader(code int) {
	if r.wroteHeader {
		return
	}
	r.wroteHeader = true
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += int64(n)
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (r *recorder) statusCode() int {
	if r.status == 0 {
		return http.StatusOK
	}
	return r.status
}
