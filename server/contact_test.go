package main

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"net/textproto"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeMailer struct {
	mu   sync.Mutex
	got  []ContactMessage
	fail bool
}

func (f *fakeMailer) Send(_ context.Context, m ContactMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return errors.New("relay down")
	}
	f.got = append(f.got, m)
	return nil
}

func postContact(h http.Handler, form url.Values, mut ...func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/contact", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "203.0.113.7:5555"
	for _, m := range mut {
		m(req)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func goodForm() url.Values {
	return url.Values{"name": {"Ada Lovelace"}, "email": {"ada@example.com"}, "message": {"Hello there.\nSecond line."}, "elapsed": {"8000"}}
}

func TestContactBotPace(t *testing.T) {
	for _, elapsed := range []string{"", "abc", "-5", "900"} {
		fm := &fakeMailer{}
		s := newTestServer(t, Options{Mailer: fm})
		f := goodForm()
		if elapsed == "" {
			f.Del("elapsed")
		} else {
			f.Set("elapsed", elapsed)
		}
		wantResult(t, postContact(s.Handler(), f), contactSent)
		if len(fm.got) != 0 {
			t.Fatalf("elapsed=%q was delivered", elapsed)
		}
	}
}

func TestContactGlobalLimit(t *testing.T) {
	fm := &fakeMailer{}
	s := newTestServer(t, Options{Mailer: fm})
	for i := 0; i < contactAllPerHour; i++ {
		ip := func(r *http.Request) { r.RemoteAddr = fmt.Sprintf("198.51.100.%d:1", i+1) }
		wantResult(t, postContact(s.Handler(), goodForm(), ip), contactSent)
	}
	last := func(r *http.Request) { r.RemoteAddr = "203.0.113.200:1" }
	wantResult(t, postContact(s.Handler(), goodForm(), last), contactLimited)
}

func wantResult(t *testing.T, rr *httptest.ResponseRecorder, result string) {
	t.Helper()
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/?contact="+result+"#contact" {
		t.Fatalf("Location = %q, want result %q", loc, result)
	}
}

func TestContactSends(t *testing.T) {
	fm := &fakeMailer{}
	s := newTestServer(t, Options{Mailer: fm})
	rr := postContact(s.Handler(), goodForm())
	wantResult(t, rr, contactSent)
	checkSecurityHeaders(t, rr, cspNonHTML)
	if len(fm.got) != 1 || fm.got[0].Email != "ada@example.com" || fm.got[0].Message != "Hello there.\nSecond line." {
		t.Fatalf("delivered %+v", fm.got)
	}
	if !strings.Contains(do(s.Handler(), "GET", "/metrics").Body.String(), `contact_submissions_total{result="sent"} 1`) {
		t.Fatal("metric not recorded")
	}
}

func TestContactOfflineWithoutMailer(t *testing.T) {
	s := newTestServer(t, Options{})
	wantResult(t, postContact(s.Handler(), goodForm()), contactOffline)
}

func TestContactDeliveryError(t *testing.T) {
	s := newTestServer(t, Options{Mailer: &fakeMailer{fail: true}})
	wantResult(t, postContact(s.Handler(), goodForm()), contactError)
}

func TestContactInvalid(t *testing.T) {
	cases := map[string]url.Values{
		"empty name":     {"name": {" "}, "email": {"a@b.co"}, "message": {"hi"}},
		"bad email":      {"name": {"A"}, "email": {"not-an-email"}, "message": {"hi"}},
		"display email":  {"name": {"A"}, "email": {"Bob <b@b.co>"}, "message": {"hi"}},
		"header inject":  {"name": {"A\r\nBcc: x@y.co"}, "email": {"a@b.co"}, "message": {"hi"}},
		"no tld":         {"name": {"A"}, "email": {"a@localhost"}, "message": {"hi"}},
		"empty message":  {"name": {"A"}, "email": {"a@b.co"}, "message": {""}},
		"long message":   {"name": {"A"}, "email": {"a@b.co"}, "message": {strings.Repeat("x", contactMaxMessage+1)}},
		"control in msg": {"name": {"A"}, "email": {"a@b.co"}, "message": {"hi\x07"}},
	}
	for name, form := range cases {
		t.Run(name, func(t *testing.T) {
			fm := &fakeMailer{}
			s := newTestServer(t, Options{Mailer: fm})
			form.Set("elapsed", "8000")
			wantResult(t, postContact(s.Handler(), form), contactInvalid)
			if len(fm.got) != 0 {
				t.Fatal("invalid submission was delivered")
			}
		})
	}
}

func TestContactRejectsCrossSiteAndWrongType(t *testing.T) {
	fm := &fakeMailer{}
	s := newTestServer(t, Options{Mailer: fm})
	wantResult(t, postContact(s.Handler(), goodForm(), func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }), contactInvalid)
	wantResult(t, postContact(s.Handler(), goodForm(), func(r *http.Request) { r.Header.Set("Content-Type", "application/json") }), contactInvalid)
	if len(fm.got) != 0 {
		t.Fatal("rejected submission was delivered")
	}
}

func TestContactHoneypot(t *testing.T) {
	fm := &fakeMailer{}
	s := newTestServer(t, Options{Mailer: fm})
	f := goodForm()
	f.Set("website", "http://spam.example")
	wantResult(t, postContact(s.Handler(), f), contactSent)
	if len(fm.got) != 0 {
		t.Fatal("honeypot submission was delivered")
	}
}

func TestContactRateLimit(t *testing.T) {
	fm := &fakeMailer{}
	s := newTestServer(t, Options{Mailer: fm})
	for i := 0; i < 3; i++ {
		wantResult(t, postContact(s.Handler(), goodForm()), contactSent)
	}
	wantResult(t, postContact(s.Handler(), goodForm()), contactLimited)
	other := func(r *http.Request) { r.RemoteAddr = "198.51.100.9:1" }
	wantResult(t, postContact(s.Handler(), goodForm(), other), contactSent)
}

func TestContactTrustProxy(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/contact", nil)
	r.RemoteAddr = "10.0.0.2:80"
	r.Header.Set("X-Forwarded-For", "6.6.6.6, 203.0.113.9")
	if got := clientKey(r, true); got != "203.0.113.9" {
		t.Fatalf("trusted key = %q", got)
	}
	if got := clientKey(r, false); got != "10.0.0.2" {
		t.Fatalf("untrusted key = %q", got)
	}
}

func TestContactMethods(t *testing.T) {
	s := newTestServer(t, Options{})
	rr := do(s.Handler(), "GET", "/api/contact")
	if rr.Code != http.StatusMethodNotAllowed || rr.Header().Get("Allow") != "POST" {
		t.Fatalf("GET /api/contact = %d Allow=%q", rr.Code, rr.Header().Get("Allow"))
	}
	if rr := do(s.Handler(), "POST", "/"); rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST / = %d, want 405", rr.Code)
	}
}

func TestLimiterWindow(t *testing.T) {
	l := newLimiter(2, time.Minute)
	t0 := time.Unix(0, 0)
	if !l.allow("k", t0) || !l.allow("k", t0) || l.allow("k", t0) {
		t.Fatal("limit not enforced")
	}
	if !l.allow("k", t0.Add(time.Minute)) {
		t.Fatal("window did not reset")
	}
}

func TestBuildMessageHeaders(t *testing.T) {
	from, _ := mail.ParseAddress("site@example.com")
	to, _ := mail.ParseAddress("me@example.com")
	b, err := buildMessage(from, to, ContactMessage{Name: "Zoë", Email: "z@example.org", Message: "line1\nline2"}, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	msg, err := mail.ReadMessage(strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Get("From") != "<site@example.com>" || !strings.Contains(msg.Header.Get("Reply-To"), "z@example.org") {
		t.Fatalf("headers: %v", msg.Header)
	}
	subj, _ := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if subj != "Website contact from Zoë" {
		t.Fatalf("subject = %q", subj)
	}
}

func TestNewSMTPMailerValidation(t *testing.T) {
	if _, err := NewSMTPMailer(SMTPConfig{Host: "smtp.example.com", From: "x", To: "me@example.com"}); err == nil {
		t.Fatal("bad From accepted")
	}
	m, err := NewSMTPMailer(SMTPConfig{Host: "smtp.example.com", From: "site@example.com", To: "me@example.com"})
	if err != nil || m.cfg.Port != 587 {
		t.Fatalf("mailer %+v err %v", m, err)
	}
}

func TestParseConfigSMTP(t *testing.T) {
	env := map[string]string{"SMTP_HOST": "smtp.example.com", "SMTP_PORT": "465", "CONTACT_FROM": "a@b.co", "CONTACT_TO": "c@d.co"}
	c, err := parseConfig(nil, func(k string) string { return env[k] }, discardWriter{})
	if err != nil || !c.smtp.Configured() || c.smtp.Port != 465 {
		t.Fatalf("cfg %+v err %v", c.smtp, err)
	}
	env["SMTP_PORT"] = "nope"
	if _, err := parseConfig(nil, func(k string) string { return env[k] }, discardWriter{}); err == nil {
		t.Fatal("bad port accepted")
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// TestSMTPMailerLoopback drives SMTPMailer against a minimal in-process SMTP
// server on loopback (where clear text is allowed).
func TestSMTPMailerLoopback(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		tp := textproto.NewConn(c)
		_ = tp.PrintfLine("220 test ESMTP")
		var data strings.Builder
		for {
			line, err := tp.ReadLine()
			if err != nil {
				return
			}
			cmd := strings.ToUpper(strings.SplitN(line, " ", 2)[0])
			switch cmd {
			case "EHLO", "HELO":
				_ = tp.PrintfLine("250 test")
			case "MAIL", "RCPT":
				data.WriteString(line + "\n")
				_ = tp.PrintfLine("250 ok")
			case "DATA":
				_ = tp.PrintfLine("354 go")
				b, _ := tp.ReadDotBytes()
				data.Write(b)
				_ = tp.PrintfLine("250 queued")
			case "QUIT":
				_ = tp.PrintfLine("221 bye")
				got <- data.String()
				return
			default:
				_ = tp.PrintfLine("250 ok")
			}
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	m, err := NewSMTPMailer(SMTPConfig{Host: "127.0.0.1", Port: port, From: "site@example.com", To: "me@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Send(context.Background(), ContactMessage{Name: "Ada", Email: "ada@example.com", Message: "hi\n.dot line"}); err != nil {
		t.Fatal(err)
	}
	select {
	case s := <-got:
		for _, want := range []string{"MAIL FROM:<site@example.com>", "RCPT TO:<me@example.com>", "Reply-To:", "ada@example.com", ".dot line"} {
			if !strings.Contains(s, want) {
				t.Fatalf("missing %q in:\n%s", want, s)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no message received")
	}
}
