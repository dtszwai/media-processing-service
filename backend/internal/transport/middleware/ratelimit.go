package middleware

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	appratelimit "github.com/dtszwai/media-processing-service/backend/internal/app/ratelimit"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/obs"
	"github.com/dtszwai/media-processing-service/backend/internal/util/jwtauth"
)

const defaultLimiterCacheSize = 10_000

var rateLimitStoreErrors metric.Int64Counter

func init() {
	rateLimitStoreErrors, _ = otel.GetMeterProvider().Meter(obs.MeterName).Int64Counter(
		"ratelimit.store_errors_total",
		metric.WithDescription("Rate limiter backing-store failures, by route class and fail-open decision."),
		metric.WithUnit("1"),
	)
}

// RateLimitOptions configures the per-key token-bucket rate limit.
type RateLimitOptions struct {
	// RequestsPerMinute caps the per-key rate.
	RequestsPerMinute int
	// Burst is the bucket size; requests beyond Burst are 429'd.
	Burst int
	// Store persists token buckets. Nil uses a bounded in-process store for
	// tests and single-process fallback; production wires Redis.
	Store appratelimit.Store
	// KeyFunc identifies all buckets a request must pass. Defaults to
	// BucketKeys: authenticated traffic consumes tenant and principal buckets,
	// unauthenticated traffic consumes one IP bucket. Override only in tests.
	KeyFunc func(*http.Request) []string
	// FailOpenOnStoreError allows read-like routes through when Redis is down.
	// Mutation routes still fail closed.
	FailOpenOnStoreError bool
}

// RateLimitMiddleware is a per-key token-bucket limiter. Limiter storage is
// bounded so a unique-key flood cannot grow memory unbounded. Defaults: 100 rpm,
// burst 10.
//
// Installed AFTER AuthMiddleware so authenticated requests bucket per tenant +
// API key, not per source IP — a single tenant behind NAT can otherwise share
// one IP and starve each other; a single key fanning out across hosts must not
// dodge the limit by changing source IP.
func RateLimitMiddleware(opts RateLimitOptions) func(http.Handler) http.Handler {
	if opts.RequestsPerMinute <= 0 {
		opts.RequestsPerMinute = 100
	}
	if opts.Burst <= 0 {
		opts.Burst = 10
	}
	if opts.KeyFunc == nil {
		opts.KeyFunc = BucketKeys
	}
	if opts.Store == nil {
		opts.Store = newMemoryStore(defaultLimiterCacheSize)
	}
	bucket := appratelimit.Bucket{
		Capacity:        opts.Burst,
		RefillPerSecond: float64(opts.RequestsPerMinute) / 60.0,
		TTL:             2 * time.Minute,
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b := bucket
			b.Now = time.Now().UTC()
			decision, err := opts.Store.Allow(r.Context(), opts.KeyFunc(r), b)
			if err != nil {
				if opts.FailOpenOnStoreError && routeIsReadLike(classifyRoute(r)) {
					recordRateLimitStoreError(r.Context(), classifyRoute(r), true)
					next.ServeHTTP(w, r)
					return
				}
				recordRateLimitStoreError(r.Context(), classifyRoute(r), false)
				http.Error(w, "rate limiter unavailable", http.StatusServiceUnavailable)
				return
			}
			setRateLimitHeaders(w, decision)
			if !decision.Allowed {
				http.Error(w, "rate limited", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func recordRateLimitStoreError(ctx context.Context, routeClass string, failOpen bool) {
	if rateLimitStoreErrors == nil {
		return
	}
	rateLimitStoreErrors.Add(ctx, 1, metric.WithAttributes(
		attribute.String("route_class", routeClass),
		attribute.Bool("fail_open", failOpen),
	))
}

func setRateLimitHeaders(w http.ResponseWriter, d appratelimit.Decision) {
	w.Header().Set("X-RateLimit-Limit", strconv.Itoa(d.Limit))
	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(max(d.Remaining, 0)))
	if d.ResetAfter > 0 {
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(d.ResetAfter).Unix(), 10))
	}
	if !d.Allowed {
		retry := d.RetryAfter
		if retry <= 0 {
			retry = time.Second
		}
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retry.Seconds()))))
	}
}

func routeIsReadLike(class string) bool {
	return class == routeClassRead || class == routeClassAuth || class == routeClassMediaDownload
}

type memoryStore struct {
	mu      sync.Mutex
	buckets map[string]memoryBucket
	maxKeys int
}

type memoryBucket struct {
	tokens      float64
	refreshedAt time.Time
}

func newMemoryStore(maxKeys int) *memoryStore {
	return &memoryStore{buckets: map[string]memoryBucket{}, maxKeys: maxKeys}
}

func (s *memoryStore) Allow(_ context.Context, keys []string, cfg appratelimit.Bucket) (appratelimit.Decision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cfg.Now.IsZero() {
		cfg.Now = time.Now().UTC()
	}
	if cfg.Capacity <= 0 {
		cfg.Capacity = 1
	}
	if cfg.RefillPerSecond <= 0 {
		cfg.RefillPerSecond = 1
	}
	if len(keys) == 0 {
		keys = []string{"anonymous"}
	}
	current := make(map[string]memoryBucket, len(keys))
	allowed := true
	minRemaining := cfg.Capacity
	var retryAfter time.Duration
	for _, key := range keys {
		b := s.buckets[key]
		if b.refreshedAt.IsZero() {
			b = memoryBucket{tokens: float64(cfg.Capacity), refreshedAt: cfg.Now}
		}
		elapsed := cfg.Now.Sub(b.refreshedAt).Seconds()
		if elapsed > 0 {
			b.tokens = math.Min(float64(cfg.Capacity), b.tokens+elapsed*cfg.RefillPerSecond)
			b.refreshedAt = cfg.Now
		}
		current[key] = b
		remaining := int(math.Floor(b.tokens))
		if remaining < minRemaining {
			minRemaining = remaining
		}
		if b.tokens < 1 {
			allowed = false
			wait := time.Duration(math.Ceil((1-b.tokens)/cfg.RefillPerSecond*1000)) * time.Millisecond
			if wait <= 0 {
				wait = time.Second
			}
			if retryAfter == 0 || wait > retryAfter {
				retryAfter = wait
			}
		}
	}
	if !allowed {
		return appratelimit.Decision{
			Allowed:    false,
			Limit:      cfg.Capacity,
			Remaining:  max(minRemaining, 0),
			ResetAfter: retryAfter,
			RetryAfter: retryAfter,
		}, nil
	}
	for key, b := range current {
		if _, exists := s.buckets[key]; !exists && len(s.buckets) >= s.maxKeys {
			for k := range s.buckets {
				delete(s.buckets, k)
				break
			}
		}
		b.tokens--
		s.buckets[key] = b
		if rem := int(math.Floor(b.tokens)); rem < minRemaining {
			minRemaining = rem
		}
	}
	return appratelimit.Decision{
		Allowed:    true,
		Limit:      cfg.Capacity,
		Remaining:  max(minRemaining, 0),
		ResetAfter: time.Duration(math.Ceil((float64(cfg.Capacity)-float64(minRemaining))/cfg.RefillPerSecond*1000)) * time.Millisecond,
	}, nil
}

// Route classes — a coarse partition of the URL space so that bursts on one
// surface (e.g. signed downloads) can't starve another (e.g. generation
// submits). The class is part of the bucket key.
const (
	routeClassAuth             = "AUTH"
	routeClassMediaDownload    = "MEDIA_DOWNLOAD"
	routeClassGenerationSubmit = "GENERATION_SUBMIT"
	routeClassWrite            = "WRITE"
	routeClassRead             = "READ"
)

// classifyRoute maps a request to a coarse route class. The classifier is
// path+method-based on purpose: it must be cheap and stay stable even when
// new handlers are added under an existing prefix.
func classifyRoute(r *http.Request) string {
	p := r.URL.Path
	switch {
	case strings.HasPrefix(p, "/mediaservice.auth.v1.AuthService/"):
		return routeClassAuth
	case strings.HasPrefix(p, "/mediaservice.generation.v1.GenerationService/"):
		if strings.HasSuffix(p, "/CreateGeneration") || strings.HasSuffix(p, "/CreateAudioOverview") {
			return routeClassGenerationSubmit
		}
		return routeClassRead
	case p == "/mediaservice.media.v1.MediaService/PresignAssetDownload" || p == "/mediaservice.media.v1.MediaService/GetMediaRoleURL":
		return routeClassMediaDownload
	case strings.HasPrefix(p, "/mediaservice.shorturl.v1.ShortURLService/"):
		return routeClassMediaDownload
	}
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return routeClassWrite
	default:
		return routeClassRead
	}
}

// BucketKeys produces all rate-limit bucket identifiers for r. Authenticated
// traffic must pass both the tenant bucket and the credential bucket; public
// traffic falls back to one remote-IP bucket.
func BucketKeys(r *http.Request) []string {
	class := classifyRoute(r)
	if p, err := jwtauth.FromContext(r.Context()); err == nil {
		keys := []string{"tenant:" + p.TenantID + ":route:" + class}
		switch {
		case p.APIKeyID != "":
			keys = append(keys, "tenant:"+p.TenantID+":api_key:"+p.APIKeyID+":route:"+class)
		case p.UserID != "":
			keys = append(keys, "tenant:"+p.TenantID+":user:"+p.UserID+":route:"+class)
		}
		return keys
	}
	return []string{"ip:" + remoteIP(r) + ":route:" + class}
}

func remoteIP(r *http.Request) string {
	// X-Forwarded-For is a comma-separated chain "client, proxy1, proxy2".
	// Bucketing on the full chain splits a single client's budget across
	// every proxy combination it transits through; take the leftmost hop.
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		if comma := strings.IndexByte(v, ','); comma >= 0 {
			v = v[:comma]
		}
		return strings.TrimSpace(v)
	}
	return r.RemoteAddr
}
