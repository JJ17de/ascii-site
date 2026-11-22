package main

import "net/http"

const (
	// cspNonHTML applies to every response that is not an HTML page: nothing
	// may be loaded and nothing may frame it.
	cspNonHTML = "default-src 'none'; frame-ancestors 'none'"

	hstsValue = "max-age=31536000; includeSubDomains"

	cacheNoCache = "no-cache" // revalidate with ETag every time
	cacheNoStore = "no-store" // never cache (ops endpoints, errors)
)

// setSecurityHeaders applies the headers every response carries. HTML
// handlers later replace Content-Security-Policy with the page's own
// hash-based policy.
func setSecurityHeaders(h http.Header, hsts bool) {
	h.Set("Content-Security-Policy", cspNonHTML)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), interest-cohort=()")
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Set("X-Frame-Options", "DENY")
	if hsts {
		h.Set("Strict-Transport-Security", hstsValue)
	}
}
