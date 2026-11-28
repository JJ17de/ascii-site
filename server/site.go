package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
)

const (
	indexFile = "index.html"

	// maxFileSize bounds a single file held in memory. The site is a handful
	// of small self-contained pages; anything this large is a build mistake.
	maxFileSize = 32 << 20
)

// errExternalScript is returned when an HTML page references an external
// script. The site must be fully self-contained so a hash-based CSP covers
// every script that can run.
var errExternalScript = errors.New("external <script src=...> is not allowed: the site must be self-contained")

// file is one fully prepared response body, computed once at load time.
type file struct {
	name        string // path within the FS, e.g. "lab/tree.html"
	contentType string
	csp         string // Content-Security-Policy for this file
	body        []byte
	etag        string // strong ETag of body
	gz          []byte // gzip of body; nil when not compressible or not smaller
	gzETag      string // strong ETag of the gzip representation
}

// Site is an immutable in-memory snapshot of every servable file.
type Site struct {
	files map[string]*file
}

// LoadSite reads every regular file from fsys into memory, precomputing
// content types, ETags, gzip bodies and per-page CSP headers.
//
// Entries whose name starts with "." (e.g. .gitkeep, .git/) are skipped, as
// are non-regular files such as symlinks, so an on-disk directory can never
// expose anything outside itself. Any HTML page with an external script
// causes the whole load to fail.
func LoadSite(fsys fs.FS) (*Site, error) {
	s := &Site{files: make(map[string]*file)}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != "." && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		f, err := loadFile(fsys, p)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		s.files[p] = f
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s, nil
}

// Lookup returns the file at the given FS path, or nil.
func (s *Site) Lookup(name string) *file {
	if s == nil {
		return nil
	}
	return s.files[name]
}

// Len reports the number of servable files.
func (s *Site) Len() int {
	if s == nil {
		return 0
	}
	return len(s.files)
}

func loadFile(fsys fs.FS, name string) (*file, error) {
	data, err := readLimited(fsys, name)
	if err != nil {
		return nil, err
	}
	ext := strings.ToLower(path.Ext(name))
	f := &file{
		name:        name,
		contentType: contentTypeFor(ext),
		csp:         cspNonHTML,
		body:        data,
	}
	if isHTML(ext) {
		scripts, styles, err := inlineHashes(data)
		if err != nil {
			return nil, err
		}
		f.csp = htmlCSP(scripts, styles)
	}

	sum := sha256.Sum256(data)
	tag := hex.EncodeToString(sum[:16]) // 128 bits is plenty for cache validation
	f.etag = `"` + tag + `"`

	if compressible(ext) {
		gz, err := gzipBytes(data)
		if err != nil {
			return nil, fmt.Errorf("gzip: %w", err)
		}
		if len(gz) < len(data) {
			f.gz = gz
			// Each representation needs its own strong validator.
			f.gzETag = `"` + tag + `-gzip"`
		}
	}
	return f, nil
}

func readLimited(fsys fs.FS, name string) ([]byte, error) {
	fh, err := fsys.Open(name)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	if info, err := fh.Stat(); err == nil && info.Size() > maxFileSize {
		return nil, fmt.Errorf("file is %d bytes, limit is %d", info.Size(), maxFileSize)
	}
	data, err := io.ReadAll(io.LimitReader(fh, maxFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFileSize {
		return nil, fmt.Errorf("file exceeds %d bytes", maxFileSize)
	}
	return data, nil
}

func gzipBytes(b []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := zw.Write(b); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func isHTML(ext string) bool { return ext == ".html" || ext == ".htm" }

func contentTypeFor(ext string) string {
	switch ext {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".json":
		return "application/json"
	case ".png":
		return "image/png"
	case ".svg":
		return "image/svg+xml"
	case ".txt":
		return "text/plain; charset=utf-8"
	case ".ico":
		return "image/x-icon"
	default:
		return "application/octet-stream"
	}
}

func compressible(ext string) bool {
	switch ext {
	case ".html", ".htm", ".css", ".js", ".mjs", ".json", ".svg", ".txt":
		return true
	}
	return false
}

// htmlCSP builds the policy for one HTML page from the hashes of its inline
// script and style blocks.
func htmlCSP(scripts, styles []string) string {
	return "default-src 'none'; script-src " + sourceList(scripts) +
		"; style-src " + sourceList(styles) +
		"; img-src data:; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"
}

func sourceList(hashes []string) string {
	if len(hashes) == 0 {
		return "'none'"
	}
	return strings.Join(hashes, " ")
}

// cspHash returns the CSP source expression for an inline block. Browsers
// hash the element's text after the HTML parser has normalised CRLF and lone
// CR to LF, so we do the same; for LF-only files this is the exact bytes.
func cspHash(content []byte) string {
	if bytes.IndexByte(content, '\r') >= 0 {
		content = bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
		content = bytes.ReplaceAll(content, []byte("\r"), []byte("\n"))
	}
	sum := sha256.Sum256(content)
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

// inlineHashes scans an HTML document for <script> and <style> elements and
// returns the CSP hash of each one's content, in document order without
// duplicates. It is a deliberately small scanner rather than a full HTML5
// parser (the standard library has none): it understands comments, tag
// attributes (quoted or not), raw-text elements whose content must not be
// scanned, and case-insensitive tag names. Scripts inside <script>/<style>
// content (e.g. in a JS string) are therefore not mistaken for elements.
func inlineHashes(doc []byte) (scripts, styles []string, err error) {
	seen := make(map[string]bool)
	add := func(list *[]string, h string) {
		if !seen[h] {
			seen[h] = true
			*list = append(*list, h)
		}
	}

	i := 0
	for {
		lt := bytes.IndexByte(doc[i:], '<')
		if lt < 0 {
			return scripts, styles, nil
		}
		i += lt
		rest := doc[i:]

		if bytes.HasPrefix(rest, []byte("<!--")) {
			n := commentLen(rest)
			if n < 0 {
				return scripts, styles, nil // unterminated comment swallows the rest
			}
			i += n
			continue
		}

		name := tagName(rest)
		switch name {
		case "script", "style", "title", "textarea", "xmp", "iframe", "noembed", "noframes":
		case "plaintext":
			return scripts, styles, nil // everything after <plaintext> is text
		default:
			i++
			continue
		}

		hasSrc, tagLen, err := parseTag(rest, 1+len(name))
		if err != nil {
			return nil, nil, fmt.Errorf("<%s> tag: %w", name, err)
		}
		start := i + tagLen
		end := findEndTag(doc[start:], name)
		if end < 0 {
			if name == "script" || name == "style" {
				return nil, nil, fmt.Errorf("unterminated <%s> element", name)
			}
			return scripts, styles, nil
		}
		content := doc[start : start+end]
		switch name {
		case "script":
			if hasSrc {
				return nil, nil, errExternalScript
			}
			add(&scripts, cspHash(content))
		case "style":
			add(&styles, cspHash(content))
		}
		i = start + end + 2 + len(name) // just past "</name"
	}
}

// commentLen returns the length of the comment at the start of b (which
// begins with "<!--"), or -1 if it is unterminated. It handles the abrupt
// forms "<!-->" and "<!--->" and the "--!>" terminator like browsers do.
func commentLen(b []byte) int {
	body := b[4:]
	if bytes.HasPrefix(body, []byte(">")) {
		return 5
	}
	if bytes.HasPrefix(body, []byte("->")) {
		return 6
	}
	best := -1
	for _, term := range []string{"-->", "--!>"} {
		if j := bytes.Index(body, []byte(term)); j >= 0 && (best < 0 || 4+j+len(term) < best) {
			best = 4 + j + len(term)
		}
	}
	return best
}

func isHTMLSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r'
}

// tagName returns the lower-cased name of the start tag at the beginning of
// b, or "" if b does not start with a start tag.
func tagName(b []byte) string {
	if len(b) < 2 || !isASCIILetter(b[1]) {
		return ""
	}
	j := 1
	for j < len(b) && !isHTMLSpace(b[j]) && b[j] != '/' && b[j] != '>' {
		j++
	}
	return strings.ToLower(string(b[1:j]))
}

func isASCIILetter(c byte) bool { return (c|0x20) >= 'a' && (c|0x20) <= 'z' }

// parseTag walks the attributes of the start tag in b beginning at pos and
// returns whether a src attribute is present and the length of the whole
// tag including the closing '>'.
func parseTag(b []byte, pos int) (hasSrc bool, n int, err error) {
	for {
		for pos < len(b) && (isHTMLSpace(b[pos]) || b[pos] == '/') {
			pos++
		}
		if pos >= len(b) {
			return false, 0, errors.New("unterminated tag")
		}
		if b[pos] == '>' {
			return hasSrc, pos + 1, nil
		}
		// Attribute name. A leading '=' is part of the name per the spec.
		nameStart := pos
		pos++
		for pos < len(b) && !isHTMLSpace(b[pos]) && b[pos] != '/' && b[pos] != '>' && b[pos] != '=' {
			pos++
		}
		if strings.EqualFold(string(b[nameStart:pos]), "src") {
			hasSrc = true
		}
		for pos < len(b) && isHTMLSpace(b[pos]) {
			pos++
		}
		if pos >= len(b) || b[pos] != '=' {
			continue
		}
		pos++
		for pos < len(b) && isHTMLSpace(b[pos]) {
			pos++
		}
		if pos < len(b) && (b[pos] == '"' || b[pos] == '\'') {
			q := b[pos]
			j := bytes.IndexByte(b[pos+1:], q)
			if j < 0 {
				return false, 0, errors.New("unterminated attribute value")
			}
			pos += j + 2
			continue
		}
		for pos < len(b) && !isHTMLSpace(b[pos]) && b[pos] != '>' {
			pos++
		}
	}
}

// findEndTag returns the offset in b of the end tag "</name" (any case)
// followed by whitespace, '/', '>' or end of input, or -1.
func findEndTag(b []byte, name string) int {
	off := 0
	for {
		j := bytes.Index(b[off:], []byte("</"))
		if j < 0 {
			return -1
		}
		k := off + j
		after := k + 2 + len(name)
		if after <= len(b) && strings.EqualFold(string(b[k+2:after]), name) &&
			(after == len(b) || isHTMLSpace(b[after]) || b[after] == '/' || b[after] == '>') {
			return k
		}
		off = k + 2
	}
}
