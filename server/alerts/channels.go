package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"strings"
	"time"
)

// Payload is what a channel carries: the alert and the rule it belongs to.
type Payload struct {
	State      string    `json:"state"`
	Alert      string    `json:"alert_id"`
	Rule       string    `json:"rule_id"`
	RuleName   string    `json:"rule_name"`
	Severity   string    `json:"severity"`
	NodeID     string    `json:"node_id"`
	Metric     string    `json:"metric"`
	Op         string    `json:"op"`
	Threshold  float64   `json:"threshold"`
	Value      float64   `json:"value"`
	FiredAt    time.Time `json:"fired_at"`
	ResolvedAt time.Time `json:"resolved_at,omitempty"`
	Message    string    `json:"message"`
}

func payloadOf(a Alert, r Rule) Payload {
	return Payload{
		State: a.State.String(), Alert: a.ID, Rule: r.ID, RuleName: r.Name, Severity: r.Severity,
		NodeID: a.NodeID, Metric: r.Metric, Op: r.Op, Threshold: r.Threshold, Value: a.Value,
		FiredAt: a.FiredAt, ResolvedAt: a.ResolvedAt, Message: a.Message,
	}
}

// Webhook POSTs the payload as JSON to a URL. Any 2xx is delivered.
type Webhook struct {
	name   string
	url    string
	client *http.Client
}

// NewWebhook builds a webhook channel; client nil uses a 10-second client.
func NewWebhook(name, url string, client *http.Client) (*Webhook, error) {
	name = strings.TrimSpace(name)
	url = strings.TrimSpace(url)
	if name == "" || url == "" {
		return nil, errors.New("alerts: a webhook needs a name and a URL")
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("alerts: webhook %q: URL must be http(s)://, got %q", name, url)
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Webhook{name: name, url: url, client: client}, nil
}

// Name is the channel name rules refer to.
func (w *Webhook) Name() string { return w.name }

// Notify POSTs the alert.
func (w *Webhook) Notify(ctx context.Context, a Alert, r Rule) error {
	body, err := json.Marshal(payloadOf(a, r))
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "orbit-admin-server")
	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook %q answered %d", w.name, resp.StatusCode)
	}
	return nil
}

// SMTPConfig configures the e-mail channel.
type SMTPConfig struct {
	// Name is the channel name rules refer to; default "email".
	Name string
	// Addr is host:port of the SMTP server. STARTTLS is used when the
	// server offers it.
	Addr string
	From string
	To   []string
	// Username and Password enable PLAIN authentication when set.
	Username string
	Password string
}

// SMTP sends the payload as a plain-text e-mail.
type SMTP struct {
	cfg  SMTPConfig
	send func(addr string, a smtp.Auth, from string, to []string, msg []byte) error
}

// NewSMTP builds the e-mail channel.
func NewSMTP(cfg SMTPConfig) (*SMTP, error) {
	cfg.Name = strings.TrimSpace(cfg.Name)
	if cfg.Name == "" {
		cfg.Name = "email"
	}
	if strings.TrimSpace(cfg.Addr) == "" || strings.TrimSpace(cfg.From) == "" || len(cfg.To) == 0 {
		return nil, errors.New("alerts: the e-mail channel needs an SMTP address, a sender and at least one recipient")
	}
	return &SMTP{cfg: cfg, send: smtp.SendMail}, nil
}

// Name is the channel name rules refer to.
func (s *SMTP) Name() string { return s.cfg.Name }

// Notify sends the e-mail.
func (s *SMTP) Notify(_ context.Context, a Alert, r Rule) error {
	p := payloadOf(a, r)
	subject := fmt.Sprintf("[%s] %s: %s on %s", strings.ToUpper(p.Severity), p.State, r.Name, a.NodeID)
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n",
		s.cfg.From, strings.Join(s.cfg.To, ", "), subject)
	fmt.Fprintf(&b, "%s\r\n\r\nrule: %s (%s)\r\nnode: %s\r\ncondition: %s %s %g\r\nvalue: %g\r\nfired: %s\r\n",
		a.Message, r.Name, r.ID, a.NodeID, r.Metric, r.Op, r.Threshold, a.Value, a.FiredAt.UTC().Format(time.RFC3339))
	if a.State == Resolved {
		fmt.Fprintf(&b, "resolved: %s\r\n", a.ResolvedAt.UTC().Format(time.RFC3339))
	}
	var auth smtp.Auth
	if s.cfg.Username != "" {
		host := s.cfg.Addr
		if i := strings.LastIndex(host, ":"); i > 0 {
			host = host[:i]
		}
		auth = smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, host)
	}
	return s.send(s.cfg.Addr, auth, s.cfg.From, s.cfg.To, []byte(b.String()))
}
