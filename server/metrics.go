package main

import (
	"bytes"
	"io"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// metricsContentType is the Prometheus text exposition format 0.0.4.
const metricsContentType = "text/plain; version=0.0.4; charset=utf-8"

// durationBuckets are the upper bounds (seconds) of the latency histogram;
// the implicit +Inf bucket is added on output.
var durationBuckets = [...]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5}

type requestKey struct {
	route, method string
	code          int
}

type histogram struct {
	counts [len(durationBuckets)]uint64 // non-cumulative per-bucket counts
	sum    float64
	count  uint64
}

func (h *histogram) observe(seconds float64) {
	for i, b := range durationBuckets {
		if seconds <= b {
			h.counts[i]++
			break
		}
	}
	h.sum += seconds
	h.count++
}

// Metrics is a minimal, dependency-free Prometheus registry for this server.
// All label values are bounded: routes come from a fixed set (see resolve)
// and methods are normalised by normalizeMethod, so series cannot grow with
// attacker-controlled input.
type Metrics struct {
	start     time.Time
	version   string
	commit    string
	goVersion string

	inFlight atomic.Int64

	mu        sync.Mutex
	requests  map[requestKey]uint64
	durations map[string]*histogram
	respBytes map[string]uint64
}

// NewMetrics returns an empty registry stamped with build information.
func NewMetrics(version, commit string) *Metrics {
	return &Metrics{
		start:     time.Now(),
		version:   version,
		commit:    commit,
		goVersion: runtime.Version(),
		requests:  make(map[requestKey]uint64),
		durations: make(map[string]*histogram),
		respBytes: make(map[string]uint64),
	}
}

// observe records one completed request.
func (m *Metrics) observe(route, method string, code int, n int64, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests[requestKey{route, method, code}]++
	h := m.durations[route]
	if h == nil {
		h = new(histogram)
		m.durations[route] = h
	}
	h.observe(d.Seconds())
	if n > 0 {
		m.respBytes[route] += uint64(n)
	} else if _, ok := m.respBytes[route]; !ok {
		m.respBytes[route] = 0 // keep the series present once a route is seen
	}
}

// normalizeMethod maps the request method onto a fixed set of label values.
func normalizeMethod(m string) string {
	switch m {
	case "GET", "HEAD", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "CONNECT", "TRACE":
		return m
	}
	return "other"
}

var labelEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

func formatFloat(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

func writeHeader(b *bytes.Buffer, name, typ, help string) {
	b.WriteString("# HELP " + name + " " + help + "\n")
	b.WriteString("# TYPE " + name + " " + typ + "\n")
}

// writeSample writes one sample line; labels are name/value pairs.
func writeSample(b *bytes.Buffer, name string, value string, labels ...string) {
	b.WriteString(name)
	if len(labels) > 0 {
		b.WriteByte('{')
		for i := 0; i+1 < len(labels); i += 2 {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(labels[i])
			b.WriteString(`="`)
			b.WriteString(labelEscaper.Replace(labels[i+1]))
			b.WriteByte('"')
		}
		b.WriteByte('}')
	}
	b.WriteByte(' ')
	b.WriteString(value)
	b.WriteByte('\n')
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Render writes all metrics in the text exposition format. Output order is
// deterministic: families in a fixed order, series sorted by label values.
func (m *Metrics) Render(w io.Writer, siteFiles int) (int64, error) {
	var b bytes.Buffer

	m.mu.Lock()
	writeHeader(&b, "http_requests_total", "counter", "Total HTTP requests by normalized route, method and status code.")
	keys := make([]requestKey, 0, len(m.requests))
	for k := range m.requests {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, c := keys[i], keys[j]
		if a.route != c.route {
			return a.route < c.route
		}
		if a.method != c.method {
			return a.method < c.method
		}
		return a.code < c.code
	})
	for _, k := range keys {
		writeSample(&b, "http_requests_total", strconv.FormatUint(m.requests[k], 10),
			"route", k.route, "method", k.method, "code", strconv.Itoa(k.code))
	}

	writeHeader(&b, "http_request_duration_seconds", "histogram", "HTTP request latency by normalized route.")
	for _, route := range sortedKeys(m.durations) {
		h := m.durations[route]
		var cum uint64
		for i, bound := range durationBuckets {
			cum += h.counts[i]
			writeSample(&b, "http_request_duration_seconds_bucket", strconv.FormatUint(cum, 10),
				"route", route, "le", formatFloat(bound))
		}
		writeSample(&b, "http_request_duration_seconds_bucket", strconv.FormatUint(h.count, 10),
			"route", route, "le", "+Inf")
		writeSample(&b, "http_request_duration_seconds_sum", formatFloat(h.sum), "route", route)
		writeSample(&b, "http_request_duration_seconds_count", strconv.FormatUint(h.count, 10), "route", route)
	}

	writeHeader(&b, "http_response_bytes_total", "counter", "Total response body bytes written by normalized route.")
	for _, route := range sortedKeys(m.respBytes) {
		writeSample(&b, "http_response_bytes_total", strconv.FormatUint(m.respBytes[route], 10), "route", route)
	}
	m.mu.Unlock()

	writeHeader(&b, "http_requests_in_flight", "gauge", "HTTP requests currently being served.")
	writeSample(&b, "http_requests_in_flight", strconv.FormatInt(m.inFlight.Load(), 10))

	writeHeader(&b, "site_build_info", "gauge", "Build information; the value is always 1.")
	writeSample(&b, "site_build_info", "1", "version", m.version, "commit", m.commit, "goversion", m.goVersion)

	writeHeader(&b, "site_files", "gauge", "Number of files being served.")
	writeSample(&b, "site_files", strconv.Itoa(siteFiles))

	writeHeader(&b, "process_start_time_seconds", "gauge", "Start time of the process since unix epoch in seconds.")
	writeSample(&b, "process_start_time_seconds", formatFloat(float64(m.start.UnixNano())/1e9))

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	writeHeader(&b, "go_goroutines", "gauge", "Number of goroutines that currently exist.")
	writeSample(&b, "go_goroutines", strconv.Itoa(runtime.NumGoroutine()))
	writeHeader(&b, "go_memstats_heap_alloc_bytes", "gauge", "Number of heap bytes allocated and still in use.")
	writeSample(&b, "go_memstats_heap_alloc_bytes", strconv.FormatUint(ms.HeapAlloc, 10))

	return b.WriteTo(w)
}
