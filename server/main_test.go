package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

const (
	testScript = `document.body.dataset.ok = "1";`
	testStyle  = `body{color:#0f0;background:#000}`
)

var testIndex = "<!doctype html><html><head><link rel=\"icon\" href=\"data:,\"><style>" + testStyle +
	"</style></head><body>" + strings.Repeat("<pre>ascii ascii ascii ascii</pre>\n", 40) +
	"<script>" + testScript + "</script></body></html>"

func testFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":    {Data: []byte(testIndex)},
		"lab/tree.html": {Data: []byte("<!doctype html><style>p{}</style><script>tree()</script><p>tree</p>")},
		"lab/data.json": {Data: []byte(`{"a":1}`)},
		"robots.txt":    {Data: []byte("User-agent: *\n")},
		"logo.png":      {Data: []byte("\x89PNG\r\n\x1a\n")},
		"blob.bin":      {Data: []byte{0, 1, 2}},
		".gitkeep":      {Data: nil},
	}
}

func discardLogger() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

func newTestServer(t *testing.T, opts Options) *Server {
	t.Helper()
	site, err := LoadSite(testFS())
	if err != nil {
		t.Fatalf("LoadSite: %v", err)
	}
	if opts.Logger == nil {
		opts.Logger = discardLogger()
	}
	return NewServer(site, opts)
}

func do(h http.Handler, method, target string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Add(headers[i], headers[i+1])
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

var commonSecurityHeaders = map[string]string{
	"X-Content-Type-Options":       "nosniff",
	"Referrer-Policy":              "no-referrer",
	"Permissions-Policy":           "camera=(), microphone=(), geolocation=(), interest-cohort=()",
	"Cross-Origin-Opener-Policy":   "same-origin",
	"Cross-Origin-Resource-Policy": "same-origin",
	"X-Frame-Options":              "DENY",
}

func checkSecurityHeaders(t *testing.T, rr *httptest.ResponseRecorder, wantCSP string) {
	t.Helper()
	for k, v := range commonSecurityHeaders {
		if got := rr.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if got := rr.Header().Get("Content-Security-Policy"); got != wantCSP {
		t.Errorf("Content-Security-Policy = %q, want %q", got, wantCSP)
	}
}

func TestCSPHeaderMatchesInlineHashes(t *testing.T) {
	s := newTestServer(t, Options{})
	rr := do(s.Handler(), "GET", "/")
	if rr.Code != 200 {
		t.Fatalf("status %d", rr.Code)
	}
	want := "default-src 'none'; script-src " + wantHash(testScript) +
		"; style-src " + wantHash(testStyle) +
		"; img-src data:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"
	checkSecurityHeaders(t, rr, want)
}

func TestSecurityHeadersOnAllResponses(t *testing.T) {
	s := newTestServer(t, Options{HSTS: true})
	h := s.Handler()
	cases := []struct {
		method, path string
		code         int
		csp          string
	}{
		{"GET", "/", 200, s.site.Load().Lookup("index.html").csp},
		{"GET", "/lab/data.json", 200, cspNonHTML},
		{"GET", "/healthz", 200, cspNonHTML},
		{"GET", "/metrics", 200, cspNonHTML},
		{"GET", "/does-not-exist", 404, cspNonHTML},
		{"POST", "/", 405, cspNonHTML},
		{"DELETE", "/nope", 405, cspNonHTML},
	}
	for _, c := range cases {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			rr := do(h, c.method, c.path)
			if rr.Code != c.code {
				t.Fatalf("status %d, want %d", rr.Code, c.code)
			}
			checkSecurityHeaders(t, rr, c.csp)
			if got := rr.Header().Get("Strict-Transport-Security"); got != hstsValue {
				t.Errorf("HSTS = %q", got)
			}
		})
	}
}

func TestNoHSTSByDefault(t *testing.T) {
	s := newTestServer(t, Options{})
	if got := do(s.Handler(), "GET", "/").Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("HSTS should be off by default, got %q", got)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	s := newTestServer(t, Options{})
	for _, m := range []string{"POST", "PUT", "DELETE", "PATCH", "OPTIONS", "BREW"} {
		for _, p := range []string{"/", "/healthz", "/metrics", "/lab/tree.html"} {
			rr := do(s.Handler(), m, p)
			if rr.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s: status %d, want 405", m, p, rr.Code)
			}
			if got := rr.Header().Get("Allow"); got != "GET, HEAD" {
				t.Errorf("%s %s: Allow = %q", m, p, got)
			}
		}
	}
}

func TestHealthAndReadiness(t *testing.T) {
	s := newTestServer(t, Options{})
	h := s.Handler()

	rr := do(h, "GET", "/healthz")
	if rr.Code != 200 || strings.TrimSpace(rr.Body.String()) != "ok" {
		t.Fatalf("healthz: %d %q", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("healthz Cache-Control = %q", rr.Header().Get("Cache-Control"))
	}

	rr = do(h, "GET", "/readyz")
	if rr.Code != 200 || strings.TrimSpace(rr.Body.String()) != "ready" {
		t.Fatalf("readyz: %d %q", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("readyz Cache-Control = %q", rr.Header().Get("Cache-Control"))
	}

	s.BeginShutdown()
	if rr = do(h, "GET", "/readyz"); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz during shutdown: %d, want 503", rr.Code)
	}
	if rr = do(h, "GET", "/healthz"); rr.Code != 200 {
		t.Fatalf("healthz during shutdown: %d, want 200", rr.Code)
	}
}

func TestReadinessWithoutSite(t *testing.T) {
	notLoaded := NewServer(nil, Options{Logger: discardLogger()})
	if rr := do(notLoaded.Handler(), "GET", "/readyz"); rr.Code != 503 {
		t.Errorf("nil site: readyz %d, want 503", rr.Code)
	}
	if rr := do(notLoaded.Handler(), "GET", "/"); rr.Code != 404 {
		t.Errorf("nil site: / %d, want 404", rr.Code)
	}

	site, err := LoadSite(fstest.MapFS{"lab/tree.html": {Data: []byte("<p>x</p>")}})
	if err != nil {
		t.Fatal(err)
	}
	noIndex := NewServer(site, Options{Logger: discardLogger()})
	if rr := do(noIndex.Handler(), "GET", "/readyz"); rr.Code != 503 {
		t.Errorf("no index: readyz %d, want 503", rr.Code)
	}
}

func TestTraversalAndDirectories(t *testing.T) {
	s := newTestServer(t, Options{})
	for _, p := range []string{
		"/../index.html",
		"/%2e%2e/x",
		"/%2E%2E/index.html",
		"/lab/../../etc/passwd",
		"/lab/../index.html",
		"/lab/%2e%2e/index.html",
		"/..%2findex.html",
		"/%5c..%5cindex.html",
		"/./index.html",
		"//index.html",
		"/index.html/",
		"/lab/",
		"/lab",
		"/.gitkeep",
		"/nope",
	} {
		rr := do(s.Handler(), "GET", p)
		if rr.Code != http.StatusNotFound {
			t.Errorf("GET %s: status %d, want 404", p, rr.Code)
		}
		if body := rr.Body.String(); body != "not found\n" {
			t.Errorf("GET %s: body %q", p, body)
		}
		if ct := rr.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
			t.Errorf("GET %s: Content-Type %q", p, ct)
		}
	}
}

// TestTraversalRawRequest sends un-normalised request lines over a real
// connection so no client-side cleaning can hide a bug.
func TestTraversalRawRequest(t *testing.T) {
	s := newTestServer(t, Options{})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	for _, target := range []string{"/../index.html", "/%2e%2e/%2e%2e/etc/passwd", "/lab/../../etc/passwd", "/lab/"} {
		conn, err := net.Dial("tcp", ts.Listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: example\r\nConnection: close\r\n\r\n", target)
		resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			conn.Close()
			t.Fatalf("%s: %v", target, err)
		}
		resp.Body.Close()
		conn.Close()
		if resp.StatusCode != 404 {
			t.Errorf("raw GET %s: status %d, want 404", target, resp.StatusCode)
		}
	}
}

func TestServesFilesWithContentTypes(t *testing.T) {
	s := newTestServer(t, Options{})
	cases := map[string]string{
		"/":              "text/html; charset=utf-8",
		"/index.html":    "text/html; charset=utf-8",
		"/lab/tree.html": "text/html; charset=utf-8",
		"/lab/data.json": "application/json",
		"/robots.txt":    "text/plain; charset=utf-8",
		"/logo.png":      "image/png",
		"/blob.bin":      "application/octet-stream",
	}
	for p, ct := range cases {
		rr := do(s.Handler(), "GET", p)
		if rr.Code != 200 {
			t.Errorf("%s: status %d", p, rr.Code)
			continue
		}
		if got := rr.Header().Get("Content-Type"); got != ct {
			t.Errorf("%s: Content-Type %q, want %q", p, got, ct)
		}
		if got := rr.Header().Get("Cache-Control"); got != "no-cache" {
			t.Errorf("%s: Cache-Control %q", p, got)
		}
	}
	if body := do(s.Handler(), "GET", "/").Body.String(); body != testIndex {
		t.Errorf("index body mismatch")
	}
}

func TestHEAD(t *testing.T) {
	s := newTestServer(t, Options{})
	rr := do(s.Handler(), "HEAD", "/")
	if rr.Code != 200 || rr.Body.Len() != 0 {
		t.Fatalf("HEAD /: %d, body %d bytes", rr.Code, rr.Body.Len())
	}
	if rr.Header().Get("Content-Length") != strconv.Itoa(len(testIndex)) {
		t.Errorf("HEAD Content-Length = %q", rr.Header().Get("Content-Length"))
	}
}

func TestGzipNegotiation(t *testing.T) {
	s := newTestServer(t, Options{})
	h := s.Handler()

	rr := do(h, "GET", "/", "Accept-Encoding", "br, gzip;q=0.8")
	if rr.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected gzip, headers %v", rr.Header())
	}
	if rr.Header().Get("Vary") != "Accept-Encoding" {
		t.Errorf("Vary = %q", rr.Header().Get("Vary"))
	}
	if cl := rr.Header().Get("Content-Length"); cl != strconv.Itoa(rr.Body.Len()) {
		t.Errorf("Content-Length %s != body %d", cl, rr.Body.Len())
	}
	zr, err := gzip.NewReader(rr.Body)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != testIndex {
		t.Fatal("decompressed body differs from original")
	}

	for _, ae := range []string{"", "identity", "gzip;q=0", "br", "*;q=0", "gzip;q=0, *"} {
		rr := do(h, "GET", "/", "Accept-Encoding", ae)
		if rr.Header().Get("Content-Encoding") != "" {
			t.Errorf("Accept-Encoding %q: should not gzip", ae)
		}
		if rr.Body.String() != testIndex {
			t.Errorf("Accept-Encoding %q: wrong body", ae)
		}
		if rr.Header().Get("Vary") != "Accept-Encoding" {
			t.Errorf("Accept-Encoding %q: Vary missing on identity response", ae)
		}
	}

	// Binary types are never compressed.
	if rr := do(h, "GET", "/logo.png", "Accept-Encoding", "gzip"); rr.Header().Get("Content-Encoding") != "" {
		t.Error("png should not be gzipped")
	}
}

func TestAcceptsGzip(t *testing.T) {
	cases := map[string]bool{
		"":                   false,
		"gzip":               true,
		"GZIP":               true,
		"x-gzip":             true,
		"deflate, gzip;q=.5": true,
		"gzip;q=0":           false,
		"gzip; q=0.000":      false,
		"*":                  true,
		"*;q=0":              false,
		"br, *;q=0.1":        true,
		"gzip;q=0, *":        false,
		"gzip;q=NaN":         false,
		"gzip;q=2":           false,
		"identity":           false,
	}
	for in, want := range cases {
		if got := acceptsGzip([]string{in}); got != want {
			t.Errorf("acceptsGzip(%q) = %v, want %v", in, got, want)
		}
	}
	if !acceptsGzip([]string{"br", "gzip"}) {
		t.Error("multiple header lines should be combined")
	}
}

func TestETagAnd304(t *testing.T) {
	s := newTestServer(t, Options{})
	h := s.Handler()

	rr := do(h, "GET", "/lab/tree.html")
	etag := rr.Header().Get("ETag")
	if !regexp.MustCompile(`^"[0-9a-f]{32}"$`).MatchString(etag) {
		t.Fatalf("ETag %q is not a strong hex tag", etag)
	}

	for _, inm := range []string{etag, "W/" + etag, `"other", ` + etag, "*"} {
		rr := do(h, "GET", "/lab/tree.html", "If-None-Match", inm)
		if rr.Code != http.StatusNotModified {
			t.Errorf("If-None-Match %q: status %d, want 304", inm, rr.Code)
		}
		if rr.Body.Len() != 0 {
			t.Errorf("304 had a body")
		}
		if rr.Header().Get("ETag") != etag {
			t.Errorf("304 ETag = %q", rr.Header().Get("ETag"))
		}
		checkSecurityHeaders(t, rr, s.site.Load().Lookup("lab/tree.html").csp)
	}

	if rr := do(h, "GET", "/lab/tree.html", "If-None-Match", `"stale"`); rr.Code != 200 {
		t.Errorf("stale ETag: status %d, want 200", rr.Code)
	}

	// The gzip representation has its own validator.
	gz := do(h, "GET", "/", "Accept-Encoding", "gzip")
	id := do(h, "GET", "/")
	if gz.Header().Get("ETag") == id.Header().Get("ETag") {
		t.Fatal("gzip and identity representations must have different ETags")
	}
	rr = do(h, "GET", "/", "Accept-Encoding", "gzip", "If-None-Match", gz.Header().Get("ETag"))
	if rr.Code != 304 || rr.Header().Get("Content-Encoding") != "" {
		t.Errorf("gzip revalidation: status %d, Content-Encoding %q", rr.Code, rr.Header().Get("Content-Encoding"))
	}
	if rr = do(h, "GET", "/", "If-None-Match", gz.Header().Get("ETag")); rr.Code != 200 {
		t.Errorf("gzip ETag must not validate identity response: status %d", rr.Code)
	}
}

var bucketLine = regexp.MustCompile(`^http_request_duration_seconds_bucket\{route="([^"]*)",le="([^"]*)"\} (\d+)$`)

func TestMetrics(t *testing.T) {
	s := newTestServer(t, Options{Version: "1.2.3", Commit: "abc123"})
	h := s.Handler()

	do(h, "GET", "/")
	do(h, "GET", "/")
	do(h, "HEAD", "/")
	do(h, "GET", "/lab/tree.html")
	do(h, "GET", "/robots.txt")
	do(h, "GET", "/no/such/page-1")
	do(h, "GET", "/no/such/page-2")
	do(h, "POST", "/")
	do(h, "BREW", "/x")
	do(h, "GET", "/healthz")
	do(h, "GET", "/readyz")

	rr := do(h, "GET", "/metrics")
	if rr.Code != 200 {
		t.Fatalf("status %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "text/plain; version=0.0.4; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cc := rr.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q", cc)
	}
	body := rr.Body.String()

	for _, want := range []string{
		`# TYPE http_requests_total counter`,
		`http_requests_total{route="/",method="GET",code="200"} 2`,
		`http_requests_total{route="/",method="HEAD",code="200"} 1`,
		`http_requests_total{route="/",method="POST",code="405"} 1`,
		`http_requests_total{route="/lab",method="GET",code="200"} 1`,
		`http_requests_total{route="static",method="GET",code="200"} 1`,
		`http_requests_total{route="notfound",method="GET",code="404"} 2`,
		`http_requests_total{route="notfound",method="other",code="405"} 1`,
		`http_requests_total{route="/healthz",method="GET",code="200"} 1`,
		`http_requests_total{route="/readyz",method="GET",code="200"} 1`,
		`# TYPE http_request_duration_seconds histogram`,
		`# TYPE http_requests_in_flight gauge`,
		"\nhttp_requests_in_flight 1\n",
		`# TYPE http_response_bytes_total counter`,
		`http_response_bytes_total{route="/"} ` + strconv.Itoa(2*len(testIndex)+len("method not allowed\n")),
		`site_build_info{version="1.2.3",commit="abc123",goversion="go`,
		"\nsite_files 6\n",
		`# TYPE process_start_time_seconds gauge`,
		`# TYPE go_goroutines gauge`,
		`# TYPE go_memstats_heap_alloc_bytes gauge`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics missing %q", want)
		}
	}
	for _, bad := range []string{"page-1", "/no/such", "BREW", `route="/x"`} {
		if strings.Contains(body, bad) {
			t.Errorf("metrics leaked unbounded label value %q", bad)
		}
	}

	// Every sample line must have a HELP and TYPE for its family.
	helps := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		if strings.HasPrefix(line, "# HELP ") {
			helps[strings.Fields(line)[2]] = true
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		name := line[:strings.IndexAny(line, "{ ")]
		for _, suf := range []string{"_bucket", "_sum", "_count"} {
			if strings.HasPrefix(name, "http_request_duration_seconds") {
				name = strings.TrimSuffix(name, suf)
			}
		}
		if !helps[name] {
			t.Errorf("sample %q has no preceding HELP", line)
		}
	}

	checkHistogram(t, body, map[string]int{"/": 4, "/lab": 1, "notfound": 3, "static": 1})
}

// checkHistogram validates bucket ordering, cumulativeness, +Inf == _count
// and the presence of _sum for each route, with expected counts.
func checkHistogram(t *testing.T, body string, wantCount map[string]int) {
	t.Helper()
	wantLE := []string{"0.005", "0.01", "0.025", "0.05", "0.1", "0.25", "0.5", "1", "2.5", "+Inf"}
	les := map[string][]string{}
	vals := map[string][]uint64{}
	for _, line := range strings.Split(body, "\n") {
		if m := bucketLine.FindStringSubmatch(line); m != nil {
			v, _ := strconv.ParseUint(m[3], 10, 64)
			les[m[1]] = append(les[m[1]], m[2])
			vals[m[1]] = append(vals[m[1]], v)
		}
	}
	for route, want := range wantCount {
		if strings.Join(les[route], ",") != strings.Join(wantLE, ",") {
			t.Errorf("route %s: buckets %v", route, les[route])
			continue
		}
		v := vals[route]
		for i := 1; i < len(v); i++ {
			if v[i] < v[i-1] {
				t.Errorf("route %s: buckets not cumulative: %v", route, v)
			}
		}
		count := fmt.Sprintf("http_request_duration_seconds_count{route=%q} %d", route, want)
		if !strings.Contains(body, count+"\n") {
			t.Errorf("missing %q", count)
		}
		if v[len(v)-1] != uint64(want) {
			t.Errorf("route %s: +Inf bucket %d, want %d", route, v[len(v)-1], want)
		}
		if !strings.Contains(body, fmt.Sprintf("http_request_duration_seconds_sum{route=%q} ", route)) {
			t.Errorf("route %s: missing _sum", route)
		}
	}
}

func TestMetricsDeterministicOrder(t *testing.T) {
	m := NewMetrics("v", "c")
	obs := []requestKey{
		{"static", "GET", 200}, {"/", "HEAD", 200}, {"notfound", "GET", 404},
		{"/", "GET", 304}, {"/", "GET", 200}, {"/lab", "GET", 200},
	}
	for _, k := range obs {
		m.observe(k.route, k.method, k.code, 10, 3*time.Millisecond)
	}
	render := func() []string {
		var b bytes.Buffer
		if _, err := m.Render(&b, 1); err != nil {
			t.Fatal(err)
		}
		var lines []string
		for _, l := range strings.Split(b.String(), "\n") {
			if strings.HasPrefix(l, "http_requests_total{") || strings.HasPrefix(l, "http_response_bytes_total{") {
				lines = append(lines, l)
			}
		}
		return lines
	}
	want := []string{
		`http_requests_total{route="/",method="GET",code="200"} 1`,
		`http_requests_total{route="/",method="GET",code="304"} 1`,
		`http_requests_total{route="/",method="HEAD",code="200"} 1`,
		`http_requests_total{route="/lab",method="GET",code="200"} 1`,
		`http_requests_total{route="notfound",method="GET",code="404"} 1`,
		`http_requests_total{route="static",method="GET",code="200"} 1`,
		`http_response_bytes_total{route="/"} 30`,
		`http_response_bytes_total{route="/lab"} 10`,
		`http_response_bytes_total{route="notfound"} 10`,
		`http_response_bytes_total{route="static"} 10`,
	}
	for i := 0; i < 5; i++ {
		if got := render(); strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("render %d:\n%s\nwant:\n%s", i, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}
}

func TestMetricsConcurrent(t *testing.T) {
	s := newTestServer(t, Options{})
	h := s.Handler()
	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 50; j++ {
				do(h, "GET", "/")
				do(h, "GET", "/metrics")
			}
		}()
	}
	for i := 0; i < 8; i++ {
		<-done
	}
	body := do(h, "GET", "/metrics").Body.String()
	if !strings.Contains(body, `http_requests_total{route="/",method="GET",code="200"} 400`) {
		t.Errorf("expected 400 requests to /, got:\n%s", body)
	}
}

func TestPanicRecovery(t *testing.T) {
	var logs bytes.Buffer
	s := newTestServer(t, Options{Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
	h := s.wrap(func(w http.ResponseWriter, r *http.Request, route string, f *file) {
		w.Header().Set("Content-Security-Policy", "something-html-specific")
		w.Header().Set("ETag", `"x"`)
		panic("boom")
	})
	rr := do(h, "GET", "/")
	if rr.Code != 500 || rr.Body.String() != "internal server error\n" {
		t.Fatalf("got %d %q", rr.Code, rr.Body.String())
	}
	checkSecurityHeaders(t, rr, cspNonHTML)
	if rr.Header().Get("ETag") != "" {
		t.Error("stale ETag leaked into 500 response")
	}
	if !strings.Contains(logs.String(), `"panic":"boom"`) {
		t.Errorf("panic not logged: %s", logs.String())
	}
	if !strings.Contains(do(s.Handler(), "GET", "/metrics").Body.String(),
		`http_requests_total{route="/",method="GET",code="500"} 1`) {
		t.Error("panic not counted in metrics")
	}
}

func TestAccessLogFields(t *testing.T) {
	var logs bytes.Buffer
	s := newTestServer(t, Options{Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
	req := httptest.NewRequest("GET", "/lab/tree.html?q=private", nil)
	req.Header.Set("User-Agent", "secret-agent/1.0")
	req.RemoteAddr = "203.0.113.7:4444"
	s.Handler().ServeHTTP(httptest.NewRecorder(), req)

	line := logs.String()
	for _, want := range []string{`"msg":"request"`, `"method":"GET"`, `"path":"/lab/tree.html"`,
		`"route":"/lab"`, `"status":200`, `"bytes":`, `"duration_ms":`} {
		if !strings.Contains(line, want) {
			t.Errorf("log line missing %s: %s", want, line)
		}
	}
	for _, bad := range []string{"203.0.113.7", "secret-agent", "private"} {
		if strings.Contains(line, bad) {
			t.Errorf("log line leaked %q: %s", bad, line)
		}
	}
}

func TestParseConfig(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

	c, err := parseConfig(nil, env(nil), io.Discard)
	if err != nil || c.addr != ":8080" || c.dir != "" || c.hsts {
		t.Errorf("defaults: %+v %v", c, err)
	}
	c, err = parseConfig(nil, env(map[string]string{"PORT": "9000", "SITE_DIR": "/srv/site"}), io.Discard)
	if err != nil || c.addr != ":9000" || c.dir != "/srv/site" {
		t.Errorf("env: %+v %v", c, err)
	}
	c, err = parseConfig([]string{"-addr", "127.0.0.1:1234", "-dir", "/x", "-hsts"},
		env(map[string]string{"PORT": "9000", "SITE_DIR": "/srv/site"}), io.Discard)
	if err != nil || c.addr != "127.0.0.1:1234" || c.dir != "/x" || !c.hsts {
		t.Errorf("flags override env: %+v %v", c, err)
	}
	if _, err := parseConfig([]string{"stray"}, env(nil), io.Discard); err == nil {
		t.Error("stray argument should be rejected")
	}
}

func TestSiteFromDirectory(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := siteFS(dir + "/missing"); err == nil {
		t.Error("missing dir should error")
	}
	fsys, source, err := siteFS(dir)
	if err != nil || source != dir {
		t.Fatalf("siteFS: %v %q", err, source)
	}
	site, err := LoadSite(fsys)
	if err != nil || site.Len() != 0 {
		t.Fatalf("empty dir: %v %d", err, site.Len())
	}
}

func TestGracefulShutdown(t *testing.T) {
	s := newTestServer(t, Options{})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- serve(ctx, ln, s, discardLogger(), 0) }()

	resp, err := http.Get("http://" + ln.Addr().String() + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("readyz before shutdown: %d", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("serve returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down")
	}
	if s.Ready() {
		t.Error("server should report not ready after shutdown began")
	}
}
