# ascii-site

A personal, professional website drawn entirely in text characters on one `<canvas>`. It follows the same rules as the `ascii-tree` project: one self-contained HTML file, no libraries, images, fonts or network requests, glow only from character bloom and `+ : .` halos, clock-driven animation, and sound synthesized live with the Web Audio API (off until a visitor turns it on).

Three languages each have a real job:

| Part | Language | Job |
|---|---|---|
| `engine/` | C++17 | `asciigen` renders the block-letter name and section banners from a 5x7 font, and generates the 3D network-constellation scene (nodes, edges, wireframe globe). Deterministic, so output is byte-identical across compilers. |
| `builder/` | Python 3.9+ (stdlib only) | `build.py` validates `content/site.json`, runs the engine, and inlines content, assets and `web/app.js` into one `dist/index.html`, plus a hidden semantic copy of the content for screen readers, search engines and no-JS visitors. `lint.py` enforces the project rules. |
| `server/` | Go (stdlib only) | Serves the site from an embedded copy as one static binary, with a hash-based Content-Security-Policy, security headers, gzip/ETag, `/healthz`, `/readyz` and Prometheus `/metrics`. Graceful shutdown on SIGTERM. |
| `web/` | JavaScript | The in-browser renderer: layout, scrolling, navigation, intro animation, globe, sound. |

## Prerequisites

- A C++17 compiler (`c++`; on macOS the Xcode Command Line Tools provide it)
- Python 3.9 or newer
- Go 1.22 or newer
- `make`
- Optional, for screenshots: Playwright (run `./setup.command` on macOS, or `pip install playwright && python -m playwright install chromium`)

## Commands

```bash
make            # engine -> site -> server binary
make run        # serve on http://localhost:8080  (PORT=9000 make run to change)
make site       # just build dist/index.html (open it directly in a browser)
make test       # C++ self-test, Python unit tests, go vet + go test, lint
make clean
```

`dist/index.html` works on its own when opened by double-click. The Go server adds headers and ops endpoints for deployment.

## Editing content

Everything on the page comes from `content/site.json`. The build fails with a clear message if a field is missing or malformed. Notes:

- **Contact details:** phone number and email are left off by default. Set `"email"` to show a mail link.
- **Clearance:** not published. Advertising clearance status publicly is generally discouraged.
- **Projects:** a project's `href` can point at a page in `lab/`, which is copied to `dist/lab/`. The tree and dunk films are included.
- **External links:** they must be `https://`, and `lint.py` rejects any URL on the page that isn't listed in the content file.

## Server endpoints

| Path | Purpose |
|---|---|
| `/` and static files | the site (GET/HEAD only) |
| `/healthz` | liveness: always `ok` |
| `/readyz` | readiness: 200 once the site is loaded, 503 during shutdown |
| `/metrics` | Prometheus text format: `http_requests_total{route,method,code}`, `http_request_duration_seconds` histogram, `http_requests_in_flight`, `http_response_bytes_total`, `site_build_info`, `process_start_time_seconds`, Go runtime gauges |

Route labels are normalized to a fixed set (`/`, `/lab`, `/healthz`, `/readyz`, `/metrics`, `static`, `notfound`), so label cardinality stays bounded for Grafana. Server flags: `-addr` (or `PORT`), `-dir` (or `SITE_DIR`) to serve from disk instead of the embedded copy, and `-hsts`. Version and commit are set at build time through `-ldflags`, and the Makefile does this from git.

## Rules enforced by `lint.py`

- exactly one inline `<script>` and one inline `<style>`
- no external resources, `url()` or `@import`
- no inline event handlers or style attributes (they would break the CSP)
- no `fetch`, XHR, WebSocket, beacons, `eval`, `innerHTML`, `setTimeout` or `setInterval`
- no painted glow (`arc`, radial gradients, `shadowBlur`)
- no `//` comment after code on the same line
- only https links that are listed in `content/site.json`
- a 400 KB page budget

## Layout

```
content/site.json      resume content (the only file most edits touch)
engine/                C++ asset generator  -> bin/asciigen
builder/build.py       Python page builder  -> dist/index.html
builder/lint.py        rule checks
web/app.js             canvas renderer (inlined into the page)
web/template.html      page shell
lab/                   extra self-contained pages (tree, dunk)
server/                Go server; server/web/ is filled by `make` and embedded
tests/                 Python unit tests and Playwright screenshots
```

## Next steps

Left for you to set up: container image (the server builds with `CGO_ENABLED=0`, so it fits a distroless or scratch base), Kubernetes manifests, Terraform, Argo CD, CI, and Grafana dashboards on top of `/metrics`.
