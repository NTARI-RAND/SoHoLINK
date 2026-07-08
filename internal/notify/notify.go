// Package notify is SoHoLINK's zero-cost notification seam. The onboarding
// backend (operator email-2FA) and the future messaging surface send mail
// through a small Notifier interface so the transport can be swapped without
// touching call sites.
//
// The default production transport is Go stdlib net/smtp (no third-party
// dependency, no per-message cost — the settled 2FA decision). A log/stub
// implementation is provided for dev and tests: it records what WOULD be sent
// (including the last code, so a test can assert on it) and never dials a mail
// server.
//
// Phone/paid-SMS is deferred; there is deliberately no SMS transport here.
package notify

import (
	"fmt"
	"net/mail"
	"net/smtp"
	"strings"
	"sync"
)

// Message is a single outbound email. Body is plain text (the onboarding flow
// sends short 2FA codes; the future messaging surface can add richer bodies).
type Message struct {
	To      string
	Subject string
	Body    string
}

// Notifier sends a Message. Implementations MUST be safe for concurrent use.
// An error means the message was not accepted for delivery; the caller decides
// whether that is fatal (e.g. a 2FA issue path returns the error so the applicant
// can retry).
type Notifier interface {
	Send(msg Message) error
}

// SMTPConfig configures the stdlib net/smtp Notifier. All fields are read from
// environment at wiring time (never hardcoded — house rule). Host+Port form the
// SMTP server address; From is the envelope + header sender. Username/Password
// enable PLAIN auth when Username is non-empty; otherwise the message is sent
// without auth (acceptable for a loopback relay).
type SMTPConfig struct {
	Host     string
	Port     string
	From     string
	Username string
	Password string
}

// SMTPNotifier sends mail via Go stdlib net/smtp. It is the default production
// transport. It holds no connection; each Send dials, sends, and closes, which
// is appropriate for the low volume of 2FA codes.
type SMTPNotifier struct {
	cfg SMTPConfig
}

// NewSMTPNotifier constructs an SMTPNotifier from config. It does not dial;
// connection failures surface on Send.
func NewSMTPNotifier(cfg SMTPConfig) *SMTPNotifier {
	return &SMTPNotifier{cfg: cfg}
}

// Send delivers msg via SMTP. When Username is set, PLAIN auth is used;
// otherwise the message is sent unauthenticated. The RFC 5322 headers are built
// minimally (From/To/Subject) with the plain-text body appended after a blank
// line.
func (n *SMTPNotifier) Send(msg Message) error {
	addr := net_join(n.cfg.Host, n.cfg.Port)
	// Validate the recipient is a single well-formed address before it reaches
	// either the SMTP RCPT or the To: header. Open operator signup means msg.To
	// is untrusted; this rejects CR/LF header-injection payloads. Fail closed.
	parsed, err := mail.ParseAddress(msg.To)
	if err != nil {
		return fmt.Errorf("notify: invalid recipient address %q: %w", msg.To, err)
	}
	msg.To = stripHeader(parsed.Address)
	var auth smtp.Auth
	if n.cfg.Username != "" {
		auth = smtp.PlainAuth("", n.cfg.Username, n.cfg.Password, n.cfg.Host)
	}
	payload := buildRFC5322(n.cfg.From, msg)
	if err := smtp.SendMail(addr, auth, n.cfg.From, []string{msg.To}, payload); err != nil {
		return fmt.Errorf("notify: smtp send to %q: %w", msg.To, err)
	}
	return nil
}

// net_join joins host and port into an SMTP dial address. Kept local so the
// package does not pull in net solely for JoinHostPort semantics on the simple
// host:port case.
func net_join(host, port string) string {
	if port == "" {
		return host
	}
	return host + ":" + port
}

// buildRFC5322 renders a minimal plain-text email. CRLF line endings per RFC.
func buildRFC5322(from string, msg Message) []byte {
	var b strings.Builder
	b.WriteString("From: " + stripHeader(from) + "\r\n")
	b.WriteString("To: " + stripHeader(msg.To) + "\r\n")
	b.WriteString("Subject: " + stripHeader(msg.Subject) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(msg.Body)
	b.WriteString("\r\n")
	return []byte(b.String())
}

// stripHeader removes CR and LF so an untrusted value placed on a header line
// cannot inject additional RFC 5322 headers (header/CRLF injection). Recipient
// addresses are additionally validated with mail.ParseAddress in Send.
func stripHeader(v string) string {
	v = strings.ReplaceAll(v, "\r", "")
	v = strings.ReplaceAll(v, "\n", "")
	return v
}

// LogNotifier is the dev/test transport. It never dials a mail server; it
// records every message in memory (guarded for concurrent use) so a dev can see
// what would be sent and a test can assert on it. It always reports success.
type LogNotifier struct {
	mu   sync.Mutex
	sent []Message
}

// NewLogNotifier constructs an empty LogNotifier.
func NewLogNotifier() *LogNotifier {
	return &LogNotifier{}
}

// Send records the message and returns nil. It never fails.
func (n *LogNotifier) Send(msg Message) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.sent = append(n.sent, msg)
	return nil
}

// Sent returns a copy of all messages recorded so far.
func (n *LogNotifier) Sent() []Message {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]Message, len(n.sent))
	copy(out, n.sent)
	return out
}

// Last returns the most recently recorded message and true, or a zero Message
// and false if nothing has been sent. Convenience for tests that assert on the
// last issued 2FA code.
func (n *LogNotifier) Last() (Message, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.sent) == 0 {
		return Message{}, false
	}
	return n.sent[len(n.sent)-1], true
}

// compile-time assertions.
var (
	_ Notifier = (*SMTPNotifier)(nil)
	_ Notifier = (*LogNotifier)(nil)
)
