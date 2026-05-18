package webhook

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"time"

	"github.com/cenkalti/backoff/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/dtszwai/media-processing-service/backend/internal/app/events"
	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/obs"
)

// Dispatcher sends signed webhook payloads to customer URLs. Defaults: 3
// retries on transient errors (1s/2s/4s ladder), 30s per-request timeout, no
// retry on 4xx.
//
// Idempotency: the dispatcher stakes a claim around the POST keyed on
// (deliveryID, eventID). On crash-after-2xx-before-SQS-delete the redelivery
// sees REPLAY_COMPLETED and skips straight to SQS delete.
type Dispatcher struct {
	Client      *http.Client
	Resolver    IPResolver
	Secret      []byte
	MaxRetries  int
	Now         func() time.Time
	Idempotency idempotency.Store
	Secrets     SecretResolver
	Lease       time.Duration
	Instruments *obs.Instruments
}

func NewDispatcher(secret []byte) *Dispatcher {
	return &Dispatcher{
		Client:      &http.Client{Timeout: 30 * time.Second},
		Secret:      secret,
		MaxRetries:  3,
		Now:         func() time.Time { return time.Now().UTC() },
		Lease:       2 * time.Minute,
		Instruments: obs.Noop(),
	}
}

// IPResolver is the DNS surface used to reject unsafe webhook targets after
// resolution and immediately before dispatch.
type IPResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// Send delivers a single webhook envelope. Returns nil iff a 2xx response
// was received within the retry budget. 4xx responses are terminal.
// Returns DeliveryDuplicate sentinel if the claim shows the POST was already
// completed (replay path).
func (d *Dispatcher) Send(ctx context.Context, env events.WebhookDeliveryEnvelope) error {
	if env.WebhookURL == "" {
		return errors.New("webhook: missing url")
	}
	if len(env.Payload) == 0 {
		return errors.New("webhook: missing payload")
	}

	scope := "WEBHOOK#" + env.DeliveryID + "#" + env.EventID
	var token string
	if d.Idempotency != nil {
		acquired, err := idempotency.Acquire(ctx, d.Idempotency, scope, env.EventID, d.Lease)
		if err != nil {
			return fmt.Errorf("webhook: %w", err)
		}
		switch acquired.Kind {
		case idempotency.AcquireOwned, idempotency.AcquireReclaimed:
			token = acquired.Token
		case idempotency.AcquireCompleted:
			// Already delivered; caller treats as success and deletes SQS.
			return nil
		case idempotency.AcquirePermanentlyFailed:
			return errors.New("webhook: prior delivery permanently failed")
		case idempotency.AcquireInFlight:
			return errors.New("webhook: another dispatcher holds the claim")
		case idempotency.AcquireInputConflict:
			return errors.New("webhook: idempotency input hash conflict")
		}
	}

	b := backoff.NewExponentialBackOff()
	b.InitialInterval = time.Second
	b.Multiplier = 2
	b.MaxInterval = 4 * time.Second
	b.RandomizationFactor = 0
	op := func() (struct{}, error) {
		status, err := d.attempt(ctx, env)
		if err == nil {
			return struct{}{}, nil
		}
		if status >= 400 && status < 500 {
			return struct{}{}, backoff.Permanent(fmt.Errorf("webhook: terminal %d: %w", status, err))
		}
		return struct{}{}, err
	}
	_, err := backoff.Retry(ctx, op,
		backoff.WithBackOff(b),
		backoff.WithMaxTries(uint(d.MaxRetries+1)),
	)
	if err != nil {
		if d.Idempotency != nil && token != "" {
			_ = d.Idempotency.Fail(ctx, scope, token, "PUBLISH_FAILED")
		}
		return fmt.Errorf("webhook: exhausted retries: %w", err)
	}
	if d.Idempotency != nil && token != "" {
		if cerr := d.Idempotency.Complete(ctx, scope, token, "200"); cerr != nil {
			// The 2xx already happened. Returning an error here would cause
			// SQS to redeliver, which means a duplicate POST — strictly worse
			// than a stale claim row, since the customer endpoint is the one
			// thing we cannot retract. Log and swallow.
			slog.WarnContext(ctx, "webhook: claim complete failed after 2xx; not redelivering",
				"scope", scope, "err", cerr)
		}
	}
	return nil
}

func (d *Dispatcher) attempt(ctx context.Context, env events.WebhookDeliveryEnvelope) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, env.WebhookURL, bytes.NewReader(env.Payload))
	if err != nil {
		d.emitDelivery(ctx, 0, obs.ResultTerminalFail)
		return 0, err
	}
	if err := d.validateWebhookTarget(ctx, req.URL); err != nil {
		d.emitDelivery(ctx, 0, obs.ResultTerminalFail)
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(HeaderEventID, env.EventID)
	req.Header.Set(HeaderEventType, string(env.EventType))
	req.Header.Set("X-Tenant-Id", env.TenantID)
	req.Header.Set("X-Media-Id", env.MediaID)
	req.Header.Set("Content-Length", strconv.Itoa(len(env.Payload)))
	now := d.Now()
	secret, keyID, err := d.secretFor(ctx, env)
	if err != nil {
		d.emitDelivery(ctx, 0, obs.ResultTerminalFail)
		return 0, err
	}
	if keyID != "" {
		req.Header.Set("X-Webhook-Key-Id", keyID)
	}
	SetHeaders(req, secret, env.Payload, now)
	resp, err := d.httpClient().Do(req)
	if err != nil {
		d.emitDelivery(ctx, 0, obs.ResultTransientFail)
		return 0, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		d.emitDelivery(ctx, resp.StatusCode, obs.ResultSuccess)
		return resp.StatusCode, nil
	}
	d.emitDelivery(ctx, resp.StatusCode, resultForStatus(resp.StatusCode))
	return resp.StatusCode, fmt.Errorf("status %d", resp.StatusCode)
}

func (d *Dispatcher) httpClient() *http.Client {
	if d.Client == nil {
		return &http.Client{
			Timeout:       30 * time.Second,
			CheckRedirect: d.redirectGuard(nil),
		}
	}
	client := *d.Client
	client.CheckRedirect = d.redirectGuard(d.Client.CheckRedirect)
	return &client
}

func (d *Dispatcher) redirectGuard(next func(*http.Request, []*http.Request) error) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("webhook: stopped after 10 redirects")
		}
		if err := d.validateWebhookTarget(req.Context(), req.URL); err != nil {
			return err
		}
		if next != nil {
			return next(req, via)
		}
		return nil
	}
}

func (d *Dispatcher) validateWebhookTarget(ctx context.Context, u *url.URL) error {
	if u == nil || u.Hostname() == "" {
		return errors.New("webhook: invalid url: host required")
	}
	host := u.Hostname()
	if addr, err := netip.ParseAddr(host); err == nil {
		if !isAllowedWebhookAddr(addr) {
			return fmt.Errorf("webhook: unsafe target address %s", addr)
		}
		return nil
	}
	resolver := d.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	addrs, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("webhook: resolve target: %w", err)
	}
	if len(addrs) == 0 {
		return errors.New("webhook: resolve target: no addresses")
	}
	for _, resolved := range addrs {
		addr, err := netip.ParseAddr(resolved.IP.String())
		if err != nil || !isAllowedWebhookAddr(addr) {
			if err != nil {
				return fmt.Errorf("webhook: unsafe target address %s", resolved.IP.String())
			}
			return fmt.Errorf("webhook: unsafe target address %s", addr)
		}
	}
	return nil
}

func isAllowedWebhookAddr(addr netip.Addr) bool {
	if !addr.IsValid() {
		return false
	}
	if addr.Is4In6() {
		addr = addr.Unmap()
	}
	if addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() {
		return false
	}
	for _, prefix := range blockedWebhookPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

var blockedWebhookPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("255.255.255.255/32"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
}

func (d *Dispatcher) emitDelivery(ctx context.Context, statusCode int, result string) {
	if d.Instruments == nil {
		return
	}
	d.Instruments.WebhookDeliveries.Add(ctx, 1, metric.WithAttributes(
		attribute.String("status_code_class", statusCodeClass(statusCode)),
		attribute.String("result", result),
	))
}

func statusCodeClass(statusCode int) string {
	if statusCode < 100 {
		return "none"
	}
	return strconv.Itoa(statusCode/100) + "xx"
}

func resultForStatus(statusCode int) string {
	if statusCode >= 500 {
		return obs.ResultTransientFail
	}
	return obs.ResultTerminalFail
}

func (d *Dispatcher) secretFor(ctx context.Context, env events.WebhookDeliveryEnvelope) ([]byte, string, error) {
	if d.Secrets == nil {
		return d.Secret, env.SecretKeyID, nil
	}
	if env.SecretKeyID != "" {
		secret, err := d.Secrets.ResolveSecret(ctx, env.TenantID, env.SecretKeyID)
		if err == nil {
			return secret, env.SecretKeyID, nil
		}
		if len(d.Secret) == 0 {
			return nil, "", err
		}
		return d.Secret, env.SecretKeyID, nil
	}
	keyID, secret, err := d.Secrets.ActiveSecret(ctx, env.TenantID)
	if err == nil {
		return secret, keyID, nil
	}
	if len(d.Secret) == 0 {
		return nil, "", err
	}
	return d.Secret, "", nil
}
