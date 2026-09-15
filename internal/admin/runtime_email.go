package admin

import (
	"context"
	"strings"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/mail"
	"github.com/jcsvwinston/nucleus/pkg/outbox"
)

// adminMailHealthTimeout bounds the liveness check the email view runs. The
// SMTP sender's Healthy() dials the server, and a view that hangs on an
// unreachable mail host is worse than one that reports it cannot tell.
const adminMailHealthTimeout = 2 * time.Second

type emailRuntimeSnapshot struct {
	Enabled          bool     `json:"enabled"`
	Driver           string   `json:"driver"`
	From             string   `json:"from,omitempty"`
	Status           string   `json:"status"`
	Message          string   `json:"message"`
	ProviderType     string   `json:"provider_type"`
	BuiltinProviders []string `json:"builtin_providers"`
	SMTPHost         string   `json:"smtp_host,omitempty"`

	// Health is what the configured sender says about itself right now.
	// Configuration is not delivery: an SMTP host can be spelled correctly
	// in the config and refuse every connection.
	Health mailHealth `json:"health"`
	// Delivery is the queue mail waits in. Absent when the application
	// runs no outbox — mail then goes straight out of the handler and
	// there is no queue to show, which the view says rather than showing
	// zeros that would read as "nothing pending".
	Delivery *mailDelivery `json:"delivery,omitempty"`
}

// mailHealth reports the sender's own liveness check, and says plainly when
// there was none to run: a driver that cannot be probed is not a healthy one.
type mailHealth struct {
	Checked bool   `json:"checked"`
	Healthy bool   `json:"healthy"`
	Error   string `json:"error,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// mailDelivery is the outbox as the email view shows it: what is queued,
// what failed, and how old the oldest waiting message is — which is what an
// operator looks at when a message did not arrive.
//
// Scope is part of the payload on purpose. The framework's snapshot counts
// the WHOLE outbox, every topic, and mail is one topic in it (Topic below).
// An application that also queues webhooks would otherwise read "4 pending"
// on the email screen as four unsent emails. Counting by topic needs a
// framework call that does not exist yet (NU-76); until it does, the view
// says what it is counting instead of implying something narrower.
type mailDelivery struct {
	Enabled         bool   `json:"enabled"`
	Scope           string `json:"scope"`
	Topic           string `json:"topic"`
	Table           string `json:"table,omitempty"`
	Reason          string `json:"reason,omitempty"`
	Queued          int    `json:"queued"`
	Processing      int    `json:"processing"`
	Delivered       int    `json:"delivered"`
	Failed          int    `json:"failed"`
	Total           int    `json:"total"`
	OldestPendingAt string `json:"oldest_pending_at,omitempty"`
	LastDeliveredAt string `json:"last_delivered_at,omitempty"`
}

// inspectMailDelivery reads the application's outbox. A nil outbox is not a
// failure: an application that sends straight from its handlers has no queue,
// and saying so is the honest answer.
func inspectMailDelivery(ctx context.Context, managed *outbox.ManagedOutbox) *mailDelivery {
	if managed == nil {
		return &mailDelivery{
			Enabled: false,
			Scope:   "no outbox configured",
			Topic:   mail.OutboxTopic,
			Reason:  "this application sends mail directly; there is no queue to inspect",
		}
	}
	snapshot := managed.Snapshot(ctx)
	return &mailDelivery{
		Enabled:         snapshot.Enabled,
		Scope:           "the application outbox, every topic — not mail alone",
		Topic:           mail.OutboxTopic,
		Table:           snapshot.Table,
		Reason:          snapshot.Reason,
		Queued:          snapshot.Pending,
		Processing:      snapshot.Processing,
		Delivered:       snapshot.Delivered,
		Failed:          snapshot.Failed,
		Total:           snapshot.Total,
		OldestPendingAt: snapshot.OldestPendingAt,
		LastDeliveredAt: snapshot.LastDeliveredAt,
	}
}

// inspectMailHealth asks the sender whether it can deliver, when it is the
// kind of sender that can be asked.
func inspectMailHealth(ctx context.Context, sender mail.Sender) mailHealth {
	if sender == nil {
		return mailHealth{Reason: "no sender is wired into this application"}
	}
	checker, ok := sender.(mail.HealthChecker)
	if !ok {
		return mailHealth{Reason: "this driver does not report health"}
	}
	ctx, cancel := context.WithTimeout(ctx, adminMailHealthTimeout)
	defer cancel()
	if err := checker.Healthy(ctx); err != nil {
		return mailHealth{Checked: true, Error: err.Error()}
	}
	return mailHealth{Checked: true, Healthy: true}
}

func inspectEmailRuntime(ctx context.Context, cfg PanelConfig) emailRuntimeSnapshot {
	driver := strings.ToLower(strings.TrimSpace(cfg.MailDriver))
	if driver == "" {
		driver = "noop"
	}

	providers := mail.RegisteredProviders()
	snapshot := emailRuntimeSnapshot{
		Enabled:          driver != "noop",
		Driver:           driver,
		From:             strings.TrimSpace(cfg.MailFrom),
		Status:           "configured",
		Message:          "mail delivery is configured",
		ProviderType:     "external",
		BuiltinProviders: providers,
	}

	if containsString(providers, driver) {
		snapshot.ProviderType = "builtin"
	}

	switch driver {
	case "noop":
		snapshot.Enabled = false
		snapshot.Status = "disabled"
		snapshot.ProviderType = "builtin"
		snapshot.Message = "mail driver is noop"
	case "smtp":
		snapshot.SMTPHost = strings.TrimSpace(cfg.SMTPHost)
		if snapshot.SMTPHost == "" {
			snapshot.Status = "degraded"
			snapshot.Message = "smtp driver is selected but smtp_host is empty"
		} else {
			snapshot.Message = "smtp delivery is configured"
		}
	default:
		// External provider — discovered via `nucleus-plugin-<driver>` or
		// registered programmatically through `mail.RegisterProvider`.
		// The admin panel reports configuration as "external" without
		// modelling per-provider knobs (those live in the plugin binary).
		snapshot.Message = "mail driver is configured"
	}

	if snapshot.Enabled && snapshot.From == "" {
		snapshot.Status = "degraded"
		snapshot.Message = "mail driver is configured but mail_from is empty"
	}

	// Everything above is configuration. What follows is delivery: whether
	// the sender answers, and what is waiting in the queue (OR-50).
	snapshot.Health = inspectMailHealth(ctx, cfg.Mailer)
	snapshot.Delivery = inspectMailDelivery(ctx, cfg.Outbox)
	if snapshot.Enabled && snapshot.Health.Checked && !snapshot.Health.Healthy {
		snapshot.Status = "unhealthy"
		snapshot.Message = "mail is configured but the driver is not answering"
	}
	if snapshot.Delivery != nil && snapshot.Delivery.Failed > 0 && snapshot.Status == "configured" {
		snapshot.Status = "degraded"
		snapshot.Message = "mail is configured; the outbox holds failed messages"
	}

	return snapshot
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}
