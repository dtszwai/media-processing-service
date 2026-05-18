package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultLogLookback       = time.Hour
	tailLogLookback          = 24 * time.Hour
	maxLogLookback           = 7 * 24 * time.Hour
	defaultLogLimit    int32 = 200
	maxLogLimit        int32 = 1000
)

// LokiClient is a thin Loki HTTP-API client. The compose stack ships the
// otel-lgtm image with Loki on the same hostname as Grafana, so the address
// is typically http://localhost:3000/loki — the address comes from env so
// the container can also reach msg-grafana:3100 internally.
type LokiClient struct {
	BaseURL string
	HTTP    *http.Client
}

func NewLokiClient(baseURL string) *LokiClient {
	if baseURL == "" {
		return nil
	}
	return &LokiClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// LogLine is the operator-facing log row. Labels are the Loki stream
// labels that Promtail/grafana-agent attached (request_id, job_id, …).
type LogLine struct {
	Timestamp time.Time
	Service   string
	Level     string
	Body      string
	Labels    map[string]string
}

// LogFilter narrows the Loki query.
type LogFilter struct {
	Service         string
	JobID           string
	MediaID         string
	Level           string
	Contains        string
	TailLines       int32
	LookbackSeconds int32
}

// StreamLogs is a single-shot Loki range query. Connect server-streaming is
// wired on top of this in the transport layer; the handler calls
// StreamLogs every 2s and emits the diff so the UI shows a live tail
// without holding a long-lived WebSocket.
//
// Returns lines in chronological order. The caller is responsible for
// de-duping against the previous page.
func (l *LokiClient) StreamLogs(ctx context.Context, f LogFilter, since time.Time) ([]LogLine, error) {
	if l == nil {
		return nil, fmt.Errorf("ops: loki not wired")
	}
	q := buildLokiQuery(f)
	if q == "" {
		return nil, fmt.Errorf("ops: loki query empty")
	}
	end := time.Now()
	start := since
	direction := "forward"
	if start.IsZero() || start.After(end) {
		lookback := effectiveLogLookback(f.LookbackSeconds)
		if f.TailLines > 0 {
			lookback = maxDuration(lookback, tailLogLookback)
			direction = "backward"
		}
		start = end.Add(-lookback)
	}
	limit := effectiveLogLimit(f.TailLines)
	u := fmt.Sprintf("%s/loki/api/v1/query_range", l.BaseURL)
	params := url.Values{}
	params.Set("query", q)
	params.Set("start", strconv.FormatInt(start.UnixNano(), 10))
	params.Set("end", strconv.FormatInt(end.UnixNano(), 10))
	params.Set("limit", strconv.Itoa(int(limit)))
	params.Set("direction", direction)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := l.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("loki %d: %s", resp.StatusCode, string(body))
	}
	var envelope struct {
		Data struct {
			Result []struct {
				Stream map[string]string `json:"stream"`
				Values [][2]string       `json:"values"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("loki decode: %w", err)
	}
	out := make([]LogLine, 0, 128)
	for _, stream := range envelope.Data.Result {
		labels := stream.Stream
		service := labels["service_name"]
		level := labels["severity_text"]
		if level == "" {
			level = labels["detected_level"]
		}
		for _, pair := range stream.Values {
			ns, _ := strconv.ParseInt(pair[0], 10, 64)
			line := LogLine{
				Timestamp: time.Unix(0, ns).UTC(),
				Service:   service,
				Level:     level,
				Body:      pair[1],
				Labels:    labels,
			}
			out = append(out, line)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Timestamp.Before(out[j].Timestamp)
	})
	return out, nil
}

func effectiveLogLookback(seconds int32) time.Duration {
	if seconds <= 0 {
		return defaultLogLookback
	}
	d := time.Duration(seconds) * time.Second
	if d > maxLogLookback {
		return maxLogLookback
	}
	return d
}

func effectiveLogLimit(tailLines int32) int32 {
	if tailLines <= 0 {
		return defaultLogLimit
	}
	if tailLines > maxLogLimit {
		return maxLogLimit
	}
	return tailLines
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

// buildLokiQuery composes a LogQL selector from the filter struct. Empty
// fields are dropped; the result is at minimum `{job=~".+"}` which
// matches every stream.
func buildLokiQuery(f LogFilter) string {
	matchers := []string{}
	if f.Service != "" {
		matchers = append(matchers, fmt.Sprintf(`service_name=~"%s"`, escapeLoki(f.Service)))
	}
	if f.Level != "" {
		matchers = append(matchers, fmt.Sprintf(`severity_text=~"(?i)%s"`, escapeLoki(f.Level)))
	}
	// An empty filter must still resolve to a label that *exists* in this stack,
	// or the query returns zero streams.
	selector := `{service_name=~".+"}`
	if len(matchers) > 0 {
		selector = "{" + strings.Join(matchers, ",") + "}"
	}
	q := selector
	if f.JobID != "" {
		q += fmt.Sprintf(` |= "%s"`, escapeLoki(f.JobID))
	}
	if f.MediaID != "" {
		q += fmt.Sprintf(` |= "%s"`, escapeLoki(f.MediaID))
	}
	if f.Contains != "" {
		q += fmt.Sprintf(` |= "%s"`, escapeLoki(f.Contains))
	}
	return q
}

// escapeLoki escapes user input for embedding in a LogQL double-quoted
// string. The console is LOCAL_ONLY so the threat is "operator types a
// regex meta character by accident" rather than active LogQL injection.
func escapeLoki(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return r.Replace(s)
}
