package alerts

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"strings"
	"sync"
	"testing"
	"time"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
)

func TestParseRules(t *testing.T) {
	rules, err := ParseRules(strings.NewReader(`{"rules":[{"name":"Hot CPU","metric":"cpu_percent","op":">","threshold":90,"for":"30s","channels":[]},
		{"name":"leak","metric":"goroutines","op":">=","threshold":5000,"severity":"critical","node_ids":["api-*"]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 || rules[0].ID != "hot-cpu" || rules[0].Severity != "warning" || time.Duration(rules[0].For) != 30*time.Second || rules[1].Severity != "critical" {
		t.Fatalf("normalized rules: %+v", rules)
	}
	for _, bad := range []string{
		`[{"name":"x","metric":"nope","op":">","threshold":1}]`,
		`[{"name":"x","metric":"cpu_percent","op":"~","threshold":1}]`,
		`[{"name":"","metric":"cpu_percent","op":">","threshold":1}]`,
		`[{"name":"x","metric":"cpu_percent","op":">","threshold":1,"severity":"loud"}]`,
		`[{"name":"x","metric":"cpu_percent","op":">","threshold":1},{"name":"X","metric":"cpu_percent","op":">","threshold":2}]`,
		`[{"name":"x","metric":"cpu_percent","op":">","threshold":1,"node_ids":["[bad"]}]`,
	} {
		if _, err := ParseRules(strings.NewReader(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

type recorder struct {
	mu    sync.Mutex
	calls []Payload
}

func (r *recorder) Name() string { return "rec" }
func (r *recorder) Notify(_ context.Context, a Alert, rule Rule) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, payloadOf(a, rule))
	return nil
}

func TestEngine_FiresAfterForAndResolves(t *testing.T) {
	rec := &recorder{}
	e, err := New([]Rule{{Name: "many goroutines", Metric: "goroutines", Op: ">", Threshold: 100, For: Duration(2 * time.Second), Channels: []string{"rec"}}}, []Channel{rec}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go e.Run(ctx)
	sub, unsub := e.Subscribe(8)
	defer unsub()

	t0 := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	hot := &adminv1.HostMetrics{Goroutines: 500}
	cold := &adminv1.HostMetrics{Goroutines: 10}
	if got := e.Observe("n1", hot, t0); len(got) != 0 {
		t.Fatalf("a first breaching sample must not fire before `for`: %+v", got)
	}
	if got := e.Observe("n1", hot, t0.Add(time.Second)); len(got) != 0 {
		t.Fatalf("still within `for`: %+v", got)
	}
	got := e.Observe("n1", hot, t0.Add(2*time.Second))
	if len(got) != 1 || got[0].State != Firing || got[0].Value != 500 || got[0].RuleID != "many-goroutines" || got[0].NodeID != "n1" {
		t.Fatalf("fires once the condition held for `for`: %+v", got)
	}
	if e.FiringCount() != 1 || len(e.Alerts(false, 0)) != 1 {
		t.Fatalf("one alert firing, got %d / %d", e.FiringCount(), len(e.Alerts(false, 0)))
	}
	if got := e.Observe("n1", hot, t0.Add(3*time.Second)); len(got) != 0 {
		t.Fatalf("a breaching sample while firing changes nothing: %+v", got)
	}
	got = e.Observe("n1", cold, t0.Add(4*time.Second))
	if len(got) != 1 || got[0].State != Resolved || got[0].ResolvedAt != t0.Add(4*time.Second) {
		t.Fatalf("resolves when the condition stops holding: %+v", got)
	}
	if e.FiringCount() != 0 || len(e.Alerts(false, 0)) != 0 || len(e.Alerts(true, 0)) != 1 {
		t.Fatalf("resolved alerts are history: firing=%d visible=%d withResolved=%d", e.FiringCount(), len(e.Alerts(false, 0)), len(e.Alerts(true, 0)))
	}
	// A breach that goes away before `for` never fires.
	e.Observe("n1", hot, t0.Add(10*time.Second))
	if got := e.Observe("n1", cold, t0.Add(11*time.Second)); len(got) != 0 {
		t.Fatalf("a breach shorter than `for` is not an alert: %+v", got)
	}

	// Subscribers saw both changes; the channel was notified twice.
	seen := 0
	for seen < 2 {
		select {
		case <-sub:
			seen++
		case <-time.After(time.Second):
			t.Fatalf("subscriber saw %d changes, want 2", seen)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		rec.mu.Lock()
		n := len(rec.calls)
		rec.mu.Unlock()
		if n == 2 || time.Now().After(deadline) {
			if n != 2 {
				t.Fatalf("channel notified %d times, want firing and resolution", n)
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.calls[0].State != "firing" || rec.calls[1].State != "resolved" || rec.calls[0].RuleName != "many goroutines" {
		t.Fatalf("payloads: %+v", rec.calls)
	}
}

func TestEngine_NodePatternsAndUnknownChannel(t *testing.T) {
	e, err := New([]Rule{{Name: "api only", Metric: "cpu_percent", Op: ">", Threshold: 50, NodeIDs: []string{"api-*"}}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	hot := &adminv1.HostMetrics{CpuPercent: 99}
	if got := e.Observe("db-1", hot, time.Now()); len(got) != 0 {
		t.Fatalf("a node outside the pattern is not evaluated: %+v", got)
	}
	if got := e.Observe("api-1", hot, time.Now()); len(got) != 1 {
		t.Fatalf("a node inside the pattern fires with for=0: %+v", got)
	}
	if _, err := New([]Rule{{Name: "x", Metric: "cpu_percent", Op: ">", Threshold: 1, Channels: []string{"nobody"}}}, nil, nil); err == nil {
		t.Fatal("a rule naming an unconfigured channel must be refused")
	}
}

func TestWebhook_PostsJSON(t *testing.T) {
	var got Payload
	var ctype string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctype = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	w, err := NewWebhook("ops", srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := Alert{ID: "r/n/1", RuleID: "r", NodeID: "n", State: Firing, Value: 7, FiredAt: time.Now(), Message: "m"}
	if err := w.Notify(context.Background(), a, Rule{ID: "r", Name: "R", Metric: "goroutines", Op: ">", Threshold: 5, Severity: "critical"}); err != nil {
		t.Fatal(err)
	}
	if ctype != "application/json" || got.State != "firing" || got.NodeID != "n" || got.Value != 7 || got.Severity != "critical" || got.RuleName != "R" {
		t.Fatalf("payload delivered: %s %+v", ctype, got)
	}
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer failing.Close()
	w2, _ := NewWebhook("bad", failing.URL, nil)
	if err := w2.Notify(context.Background(), a, Rule{}); err == nil {
		t.Fatal("a non-2xx answer is a failed delivery")
	}
	if _, err := NewWebhook("x", "ftp://nope", nil); err == nil {
		t.Fatal("a non-http URL is refused")
	}
}

func TestSMTP_BuildsAMessage(t *testing.T) {
	s, err := NewSMTP(SMTPConfig{Addr: "mail.example:587", From: "orbit@example", To: []string{"ops@example"}, Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	var gotAddr, gotFrom string
	var gotTo []string
	var gotMsg string
	var gotAuth smtp.Auth
	s.send = func(addr string, a smtp.Auth, from string, to []string, msg []byte) error {
		gotAddr, gotAuth, gotFrom, gotTo, gotMsg = addr, a, from, to, string(msg)
		return nil
	}
	a := Alert{ID: "r/n/1", RuleID: "r", NodeID: "n", State: Resolved, Value: 1, FiredAt: time.Now(), ResolvedAt: time.Now(), Message: "back to normal"}
	if err := s.Notify(context.Background(), a, Rule{ID: "r", Name: "Hot", Metric: "cpu_percent", Op: ">", Threshold: 90, Severity: "warning"}); err != nil {
		t.Fatal(err)
	}
	if s.Name() != "email" || gotAddr != "mail.example:587" || gotFrom != "orbit@example" || len(gotTo) != 1 || gotAuth == nil {
		t.Fatalf("send called with %s %s %v auth=%v", gotAddr, gotFrom, gotTo, gotAuth != nil)
	}
	if !strings.Contains(gotMsg, "Subject: [WARNING] resolved: Hot on n") || !strings.Contains(gotMsg, "back to normal") || !strings.Contains(gotMsg, "resolved:") {
		t.Fatalf("message:\n%s", gotMsg)
	}
	if _, err := NewSMTP(SMTPConfig{Addr: "x"}); err == nil {
		t.Fatal("an e-mail channel without sender or recipients is refused")
	}
}
