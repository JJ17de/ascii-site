package main

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

// wantHash is computed independently of cspHash.
func wantHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}

func TestCSPHashKnownValue(t *testing.T) {
	// echo -n 'alert(1)' | openssl dgst -sha256 -binary | base64
	const known = "'sha256-bhHHL3z2vDgxUt0W3dWQOrprscmda2Y5pLsLg4GF+pI='"
	if got := cspHash([]byte("alert(1)")); got != known {
		t.Fatalf("cspHash(alert(1)) = %s, want %s", got, known)
	}
}

func TestInlineHashes(t *testing.T) {
	tests := []struct {
		name    string
		doc     string
		scripts []string
		styles  []string
	}{
		{
			name:    "single script and style",
			doc:     `<html><head><style>body{color:red}</style></head><body><script>console.log(1)</script></body></html>`,
			scripts: []string{wantHash("console.log(1)")},
			styles:  []string{wantHash("body{color:red}")},
		},
		{
			name:    "multiple blocks, attributes, mixed case, duplicates",
			doc:     "<SCRIPT type=\"module\">a()</SCRIPT>\n<style media='screen'>p{}</style><script>b()</script ><script>a()</script>",
			scripts: []string{wantHash("a()"), wantHash("b()")},
			styles:  []string{wantHash("p{}")},
		},
		{
			name:    "commented out and raw-text elements ignored",
			doc:     `<!-- <script>evil()</script> --><title><script>x()</script></title><textarea><style>q{}</style></textarea><script>ok()</script>`,
			scripts: []string{wantHash("ok()")},
		},
		{
			name:    "tag-like text inside script content",
			doc:     `<script>var s = "<style>no</style><script src=x>";</script>`,
			scripts: []string{wantHash(`var s = "<style>no</style><script src=x>";`)},
		},
		{
			name:    "quoted > in attribute",
			doc:     `<script data-x="a>b">go()</script>`,
			scripts: []string{wantHash("go()")},
		},
		{
			name:    "CRLF normalised like the HTML parser",
			doc:     "<script>a()\r\nb()\rc()</script>",
			scripts: []string{wantHash("a()\nb()\nc()")},
		},
		{
			name:    "abrupt empty comment",
			doc:     `<!--><script>s()</script>`,
			scripts: []string{wantHash("s()")},
		},
		{
			name:    "custom element with script prefix is not a script",
			doc:     `<script-x>nope</script-x><scripts>nope</scripts>`,
			scripts: nil,
		},
		{
			name: "no blocks",
			doc:  `<p>hello</p>`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scripts, styles, err := inlineHashes([]byte(tt.doc))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(scripts, tt.scripts) {
				t.Errorf("scripts = %v, want %v", scripts, tt.scripts)
			}
			if !reflect.DeepEqual(styles, tt.styles) {
				t.Errorf("styles = %v, want %v", styles, tt.styles)
			}
		})
	}
}

func TestInlineHashesErrors(t *testing.T) {
	for _, doc := range []string{
		`<script>never closed`,
		`<style>never closed`,
		`<script data-x="unterminated>x()</script>`,
	} {
		if _, _, err := inlineHashes([]byte(doc)); err == nil {
			t.Errorf("inlineHashes(%q): expected error", doc)
		}
	}
}

func TestLoadSiteRejectsExternalScript(t *testing.T) {
	for _, tag := range []string{
		`<script src="https://cdn.example/x.js"></script>`,
		`<SCRIPT SRC=x.js></SCRIPT>`,
		`<script type="module" src='a.js' defer></script>`,
		`<script defer src></script>`,
	} {
		fsys := fstest.MapFS{
			"index.html":    {Data: []byte("<html><body><script>ok()</script></body></html>")},
			"lab/evil.html": {Data: []byte("<html><body>" + tag + "</body></html>")},
		}
		site, err := LoadSite(fsys)
		if err == nil {
			t.Errorf("%s: expected load error, got site with %d files", tag, site.Len())
			continue
		}
		if !errors.Is(err, errExternalScript) || !strings.Contains(err.Error(), "lab/evil.html") {
			t.Errorf("%s: error %q should wrap errExternalScript and name the file", tag, err)
		}
	}
}

func TestLoadSiteSkipsHiddenAndBuildsCSP(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":    {Data: []byte(`<style>a{}</style><script>b()</script>`)},
		".gitkeep":      {Data: nil},
		".git/config":   {Data: []byte("secret")},
		"lab/tree.html": {Data: []byte(`<p>no scripts</p>`)},
	}
	site, err := LoadSite(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if site.Len() != 2 {
		t.Fatalf("Len = %d, want 2 (hidden files skipped)", site.Len())
	}
	want := "default-src 'none'; script-src " + wantHash("b()") + "; style-src " + wantHash("a{}") +
		"; img-src data:; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"
	if got := site.Lookup("index.html").csp; got != want {
		t.Errorf("csp = %q\nwant  %q", got, want)
	}
	if got := site.Lookup("lab/tree.html").csp; !strings.Contains(got, "script-src 'none'; style-src 'none'") {
		t.Errorf("page without blocks should use 'none': %q", got)
	}
}

func TestEmbeddedSiteLoads(t *testing.T) {
	fsys, source, err := siteFS("")
	if err != nil || source != "embedded" {
		t.Fatalf("siteFS: %v %q", err, source)
	}
	site, err := LoadSite(fsys)
	if err != nil {
		t.Fatalf("embedded site failed to load: %v", err)
	}
	if site.Lookup(indexFile) == nil {
		t.Fatal("embedded site has no index.html")
	}
	if site.Lookup(".gitkeep") != nil {
		t.Fatal(".gitkeep must not be served")
	}
	if _, err := fs.Stat(fsys, ".gitkeep"); err != nil {
		t.Fatalf("expected .gitkeep in embedded FS (all: prefix): %v", err)
	}
}
