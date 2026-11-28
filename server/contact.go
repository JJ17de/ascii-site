package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// The contact form posts to routeContact as a normal HTML form (no script
// fetch), and the server answers with a 303 redirect back to the page with
// ?contact=<result>#contact so the page can show the outcome.

const (
	contactMaxBody    = 16 << 10
	contactMaxName    = 100
	contactMaxEmail   = 254
	contactMaxMessage = 4000

	// Results, used both in the redirect and as the metric label.
	contactSent    = "sent"
	contactInvalid = "invalid"
	contactLimited = "limited"
	contactOffline = "offline"
	contactError   = "error"
	contactSpam    = "spam" // bot check failed: the visitor sees "sent", nothing is delivered
)

// ContactMessage is one validated submission.
type ContactMessage struct {
	Name, Email, Message string
}

// Mailer delivers a contact message. Tests use a fake.
type Mailer interface {
	Send(ctx context.Context, m ContactMessage) error
}

// SMTPConfig configures SMTPMailer.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string // envelope sender and From header
	To       string // where messages are delivered
}

// Configured reports whether enough is set to send mail.
func (c SMTPConfig) Configured() bool {
	return c.Host != "" && c.From != "" && c.To != ""
}

// SMTPMailer sends messages through an SMTP relay. Port 465 uses implicit
// TLS; any other port requires STARTTLS unless the host is loopback.
type SMTPMailer struct {
	cfg     SMTPConfig
	timeout time.Duration
}

// NewSMTPMailer returns a mailer for cfg, or an error if cfg is incomplete
// or its addresses are malformed.
func NewSMTPMailer(cfg SMTPConfig) (*SMTPMailer, error) {
	if !cfg.Configured() {
		return nil, errors.New("SMTP_HOST, CONTACT_FROM and CONTACT_TO are required")
	}
	for _, a := range []string{cfg.From, cfg.To} {
		if _, err := mail.ParseAddress(a); err != nil {
			return nil, fmt.Errorf("bad address %q: %w", a, err)
		}
	}
	if cfg.Port == 0 {
		cfg.Port = 587
	}
	return &SMTPMailer{cfg: cfg, timeout: 20 * time.Second}, nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Send delivers m. The visitor's address goes in Reply-To, never From, so
// the message passes the relay's sender checks.
func (s *SMTPMailer) Send(ctx context.Context, m ContactMessage) error {
	from, _ := mail.ParseAddress(s.cfg.From)
	to, _ := mail.ParseAddress(s.cfg.To)
	body, err := buildMessage(from, to, m, time.Now())
	if err != nil {
		return err
	}

	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))
	deadline := time.Now().Add(s.timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	dialer := &net.Dialer{Deadline: deadline}
	tlsCfg := &tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}

	var conn net.Conn
	if s.cfg.Port == 465 {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}
	_ = conn.SetDeadline(deadline)
	c, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp greeting: %w", err)
	}
	defer c.Close()

	if s.cfg.Port != 465 {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(tlsCfg); err != nil {
				return fmt.Errorf("starttls: %w", err)
			}
		} else if !isLoopback(s.cfg.Host) {
			return errors.New("relay does not offer STARTTLS; refusing to send in clear text")
		}
	}
	if s.cfg.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}
	if err := c.Mail(from.Address); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	if err := c.Rcpt(to.Address); err != nil {
		return fmt.Errorf("rcpt to: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("data close: %w", err)
	}
	return c.Quit()
}

// buildMessage renders an RFC 5322 message. All visitor-supplied text is
// either encoded (Subject, Reply-To) or in the quoted-printable body, so it
// cannot inject headers.
func buildMessage(from, to *mail.Address, m ContactMessage, now time.Time) ([]byte, error) {
	var id [12]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}
	domain := "localhost"
	if i := strings.LastIndexByte(from.Address, '@'); i >= 0 {
		domain = from.Address[i+1:]
	}
	reply := &mail.Address{Name: m.Name, Address: m.Email}

	var b bytes.Buffer
	hdr := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	hdr("From", from.String())
	hdr("To", to.String())
	hdr("Reply-To", reply.String())
	hdr("Subject", mime.QEncoding.Encode("utf-8", "Website contact from "+m.Name))
	hdr("Date", now.UTC().Format(time.RFC1123Z))
	hdr("Message-ID", "<"+hex.EncodeToString(id[:])+"@"+domain+">")
	hdr("MIME-Version", "1.0")
	hdr("Content-Type", "text/plain; charset=utf-8")
	hdr("Content-Transfer-Encoding", "quoted-printable")
	hdr("Auto-Submitted", "auto-generated")
	b.WriteString("\r\n")

	text := "Name:  " + m.Name + "\nEmail: " + m.Email + "\n\n" + m.Message + "\n"
	text = strings.ReplaceAll(text, "\n", "\r\n")
	qp := quotedprintable.NewWriter(&b)
	if _, err := qp.Write([]byte(text)); err != nil {
		return nil, err
	}
	if err := qp.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// validateContact trims and checks a submission.
func validateContact(name, email, message string) (ContactMessage, bool) {
	name = strings.TrimSpace(name)
	email = strings.TrimSpace(email)
	message = strings.TrimSpace(strings.ReplaceAll(message, "\r\n", "\n"))
	if !utf8.ValidString(name + email + message) {
		return ContactMessage{}, false
	}
	n, e, msg := utf8.RuneCountInString(name), len(email), utf8.RuneCountInString(message)
	if n == 0 || n > contactMaxName || e == 0 || e > contactMaxEmail || msg == 0 || msg > contactMaxMessage {
		return ContactMessage{}, false
	}
	for _, r := range name + email {
		if unicode.IsControl(r) {
			return ContactMessage{}, false
		}
	}
	for _, r := range message {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return ContactMessage{}, false
		}
	}
	a, err := mail.ParseAddress(email)
	if err != nil || a.Address != email || a.Name != "" || !strings.Contains(email[strings.LastIndexByte(email, '@')+1:], ".") {
		return ContactMessage{}, false
	}
	return ContactMessage{Name: name, Email: email, Message: message}, true
}

// limiter is a fixed-window counter: at most max events per key per window.
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	seen   map[string]*windowCount
}

type windowCount struct {
	start time.Time
	n     int
}

// Contact form limits. Each visitor gets 3 messages an hour, and the whole
// site accepts at most 10 an hour, so bots spread across many addresses
// still can't flood the inbox.
const (
	contactPerIPPerHour = 3
	contactAllPerHour   = 10
	contactMinFill      = 3 * time.Second
)

// humanPace reports whether the form was on screen long enough to have been
// filled in by a person. The page's script sends the milliseconds between
// page load and submit; bots that post directly send nothing, and bots that
// run the page fill it in far faster than a person types.
func humanPace(elapsed string) bool {
	ms, err := strconv.ParseInt(elapsed, 10, 64)
	return err == nil && time.Duration(ms)*time.Millisecond >= contactMinFill
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{max: max, window: window, seen: make(map[string]*windowCount)}
}

// allow records one event for key and reports whether it is within limits.
func (l *limiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.seen) > 10000 {
		for k, w := range l.seen {
			if now.Sub(w.start) >= l.window {
				delete(l.seen, k)
			}
		}
	}
	w := l.seen[key]
	if w == nil || now.Sub(w.start) >= l.window {
		l.seen[key] = &windowCount{start: now, n: 1}
		return true
	}
	if w.n >= l.max {
		return false
	}
	w.n++
	return true
}

// clientKey identifies the sender for rate limiting. Behind a reverse proxy
// (trustProxy), the rightmost X-Forwarded-For entry is the one the trusted
// proxy added. The key is never logged.
func clientKey(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Values("X-Forwarded-For"); len(xff) > 0 {
			parts := strings.Split(xff[len(xff)-1], ",")
			if ip := strings.TrimSpace(parts[len(parts)-1]); net.ParseIP(ip) != nil {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// serveContact handles POST /api/contact.
func (s *Server) serveContact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		s.writeError(w, r, http.StatusMethodNotAllowed, "method not allowed\n")
		return
	}
	w.Header().Set("Cache-Control", cacheNoStore)
	result := s.handleContact(w, r)
	s.metrics.contact(result)
	if result == contactSpam {
		result = contactSent
	}
	http.Redirect(w, r, "/?contact="+result+"#contact", http.StatusSeeOther)
}

func (s *Server) handleContact(w http.ResponseWriter, r *http.Request) string {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return contactInvalid
	}
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if ct != "application/x-www-form-urlencoded" {
		return contactInvalid
	}
	r.Body = http.MaxBytesReader(w, r.Body, contactMaxBody)
	if err := r.ParseForm(); err != nil {
		return contactInvalid
	}
	if r.PostForm.Get("website") != "" || !humanPace(r.PostForm.Get("elapsed")) {
		return contactSpam
	}
	msg, ok := validateContact(r.PostForm.Get("name"), r.PostForm.Get("email"), r.PostForm.Get("message"))
	if !ok {
		return contactInvalid
	}
	now := time.Now()
	if !s.contactPerIP.allow(clientKey(r, s.trustProxy), now) || !s.contactGlobal.allow("all", now) {
		return contactLimited
	}
	if s.mailer == nil {
		return contactOffline
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	if err := s.mailer.Send(ctx, msg); err != nil {
		s.log.Error("contact delivery failed", "err", err.Error())
		return contactError
	}
	return contactSent
}
