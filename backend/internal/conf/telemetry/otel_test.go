package telemetry

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/go-logr/logr"
)

// TestOTelLogger_VerbosityMapping pins the OTel SDK verbosity → slog.Level
// mapping (Error → ERROR, V(1) → WARN, V(4) → INFO, V(8) → DEBUG). The
// upstream logr.FromSlogHandler maps V(n).Info to slog.Level(-n), which would
// otherwise bury SDK warnings below LevelDebug.
func TestOTelLogger_VerbosityMapping(t *testing.T) {
	cases := []struct {
		name      string
		emit      func(l logr.Logger)
		wantLevel string
		wantMsg   string
		wantErr   string
	}{
		{
			name:      "Error preserves ERROR level",
			emit:      func(l logr.Logger) { l.Error(errors.New("boom"), "exporter failed") },
			wantLevel: "ERROR",
			wantMsg:   "exporter failed",
			wantErr:   "boom",
		},
		{
			name:      "V(1) emits WARN",
			emit:      func(l logr.Logger) { l.V(1).Info("queue full") },
			wantLevel: "WARN",
			wantMsg:   "queue full",
		},
		{
			name:      "V(4) emits INFO",
			emit:      func(l logr.Logger) { l.V(4).Info("collected") },
			wantLevel: "INFO",
			wantMsg:   "collected",
		},
		{
			name:      "V(8) emits DEBUG",
			emit:      func(l logr.Logger) { l.V(8).Info("detail") },
			wantLevel: "DEBUG",
			wantMsg:   "detail",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			handler := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
			logger := logr.FromSlogHandler(otelSlogHandler{next: handler})

			tc.emit(logger)

			got := decodeRecord(t, buf)
			if got["level"] != tc.wantLevel {
				t.Fatalf("level=%v want %s", got["level"], tc.wantLevel)
			}
			if got["msg"] != tc.wantMsg {
				t.Fatalf("msg=%v want %s", got["msg"], tc.wantMsg)
			}
			if tc.wantErr != "" && got["err"] != tc.wantErr {
				t.Fatalf("err=%v want %s", got["err"], tc.wantErr)
			}
		})
	}
}

// TestOtelErrorHandler_LogsAtError covers the otel.Handle(err) path: every
// SDK error must surface at slog.LevelError so the OTLP-export-failure noise
// stops looking like benign INFO chatter.
func TestOtelErrorHandler_LogsAtError(t *testing.T) {
	buf := &bytes.Buffer{}
	handler := otelErrorHandler{logger: slog.New(slog.NewJSONHandler(buf, nil))}

	handler.Handle(errors.New("dial tcp: connection refused"))

	got := decodeRecord(t, buf)
	if got["level"] != "ERROR" {
		t.Fatalf("level=%v want ERROR", got["level"])
	}
	if got["err"] != "dial tcp: connection refused" {
		t.Fatalf("err=%v", got["err"])
	}
}

// TestOTelLogger_RespectsDownstreamLevelFilter ensures that after remap, the
// downstream handler's Level filter sees the corrected level. Without this,
// configuring LogLevel=info would either silently drop SDK warnings (input
// V(1)=slog.Level(-1) is below LevelInfo) or admit SDK debug noise.
func TestOTelLogger_RespectsDownstreamLevelFilter(t *testing.T) {
	buf := &bytes.Buffer{}
	handler := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := logr.FromSlogHandler(otelSlogHandler{next: handler})

	logger.V(1).Info("must surface as warn")
	logger.V(8).Info("must be filtered as debug")

	lines := bytes.Split(bytes.TrimRight(buf.Bytes(), "\n"), []byte("\n"))
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1; raw=%q", len(lines), buf.String())
	}
	var got map[string]any
	if err := json.Unmarshal(lines[0], &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["level"] != "WARN" || got["msg"] != "must surface as warn" {
		t.Fatalf("unexpected record: %v", got)
	}
}

func decodeRecord(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (raw=%q)", err, buf.String())
	}
	return got
}
