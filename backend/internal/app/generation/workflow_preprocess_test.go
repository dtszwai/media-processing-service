package generation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	domaingen "github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/obs"
)

func TestStagePreprocessPromptEnhancer(t *testing.T) {
	identity := runPreprocess(t, nil, newPreprocessJob("gen_identity", domaingen.OutputImage))
	if identity.err != nil {
		t.Fatalf("nil enhancer err = %v", identity.err)
	}
	if identity.result.PreparedPrompt != "raw prompt" {
		t.Fatalf("nil enhancer prompt = %q, want raw prompt", identity.result.PreparedPrompt)
	}
	if identity.result.PromptSpecVersion != "prompt-policy-v1+enhancer-none" {
		t.Fatalf("nil enhancer prompt spec = %q", identity.result.PromptSpecVersion)
	}
	if len(identity.metrics) != 0 {
		t.Fatalf("nil enhancer metrics = %v, want none", identity.metrics)
	}

	passthrough := runPreprocess(t, &PassthroughEnhancer{}, newPreprocessJob("gen_passthrough", domaingen.OutputImage))
	if passthrough.err != nil {
		t.Fatalf("passthrough err = %v", passthrough.err)
	}
	if passthrough.result.PreparedPrompt != "raw prompt" {
		t.Fatalf("passthrough prompt = %q, want raw prompt", passthrough.result.PreparedPrompt)
	}
	if passthrough.result.PromptSpecVersion != "prompt-policy-v1+enhancer-passthrough-v0" {
		t.Fatalf("passthrough prompt spec = %q", passthrough.result.PromptSpecVersion)
	}
	if got := passthrough.metrics["passthrough|IMAGE|passthrough-v0"]; got != 1 {
		t.Fatalf("passthrough metric = %d, want 1; all=%v", got, passthrough.metrics)
	}

	prefix := runPreprocess(t, &PassthroughEnhancer{Prefixes: map[domaingen.OutputType]string{
		domaingen.OutputAudio: "TEST",
	}}, newPreprocessJob("gen_prefix", domaingen.OutputAudio))
	if prefix.err != nil {
		t.Fatalf("prefix err = %v", prefix.err)
	}
	if prefix.result.PreparedPrompt != "TEST\n\nraw prompt" {
		t.Fatalf("prefix prompt = %q", prefix.result.PreparedPrompt)
	}
	if prefix.result.PreparedPromptHash == identity.result.PreparedPromptHash {
		t.Fatalf("prefix hash matched identity hash %q", prefix.result.PreparedPromptHash)
	}
	if got := prefix.metrics["applied|AUDIO|passthrough-v0"]; got != 1 {
		t.Fatalf("applied metric = %d, want 1; all=%v", got, prefix.metrics)
	}
	if prefix.result.PromptEnhancementApplied == nil || !*prefix.result.PromptEnhancementApplied {
		t.Fatalf("prefix PromptEnhancementApplied = %#v, want true", prefix.result.PromptEnhancementApplied)
	}
}

func TestStagePreprocessRejectsEmptyRawPromptBeforeEnhancer(t *testing.T) {
	enhancer := &recordingEnhancer{}
	job := newPreprocessJob("gen_empty_raw", domaingen.OutputImage)
	job.Prompt = "  "
	out := runPreprocess(t, enhancer, job)
	if out.err == nil || !domaingen.IsTerminal(out.err) || domaingen.AsError(out.err).Code != "EMPTY_PROMPT" {
		t.Fatalf("empty raw err = %v, want terminal EMPTY_PROMPT", out.err)
	}
	if enhancer.calls != 0 {
		t.Fatalf("enhancer calls = %d, want 0", enhancer.calls)
	}
	if len(out.metrics) != 0 {
		t.Fatalf("empty raw metrics = %v, want none", out.metrics)
	}
}

func TestStagePreprocessRejectsEmptyEnhancerOutput(t *testing.T) {
	enhancer := &recordingEnhancer{out: EnhanceOutput{
		Prompt:        " ",
		PolicyVersion: "test-v1",
		Provider:      "test",
	}}
	out := runPreprocess(t, enhancer, newPreprocessJob("gen_empty_enhanced", domaingen.OutputImage))
	if out.err == nil || !domaingen.IsTerminal(out.err) || domaingen.AsError(out.err).Code != "EMPTY_PROMPT_AFTER_ENHANCEMENT" {
		t.Fatalf("empty enhancer err = %v, want terminal EMPTY_PROMPT_AFTER_ENHANCEMENT", out.err)
	}
	if got := out.metrics["empty|IMAGE|test-v1"]; got != 1 {
		t.Fatalf("empty metric = %d, want 1; all=%v", got, out.metrics)
	}
}

func TestStagePreprocessEnhancerErrorIsTransient(t *testing.T) {
	enhancer := &recordingEnhancer{err: errors.New("provider down")}
	out := runPreprocess(t, enhancer, newPreprocessJob("gen_enhance_error", domaingen.OutputImage))
	if out.err == nil || domaingen.IsTerminal(out.err) || domaingen.AsError(out.err).Code != "PROMPT_ENHANCEMENT_PROVIDER_ERROR" {
		t.Fatalf("enhancer err = %v, want transient PROMPT_ENHANCEMENT_PROVIDER_ERROR", out.err)
	}
	if got := out.metrics["error|IMAGE|unknown"]; got != 1 {
		t.Fatalf("error metric = %d, want 1; all=%v", got, out.metrics)
	}
}

type preprocessRun struct {
	result  StageResult
	err     error
	metrics map[string]int64
}

func runPreprocess(t *testing.T, enhancer PromptEnhancer, job *domaingen.Job) preprocessRun {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := obs.NewInstruments(mp.Meter(obs.MeterName))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	w := &Workflow{
		PromptEnhancer: enhancer,
		PromptSealer:   preprocessSealer{},
		Instruments:    inst,
		Clock:          func() time.Time { return time.Unix(100, 0).UTC() },
	}
	result, runErr := w.stagePreprocess(context.Background(), job)
	return preprocessRun{
		result:  result,
		err:     runErr,
		metrics: collectPromptEnhancementMetrics(t, reader),
	}
}

func newPreprocessJob(id string, outputType domaingen.OutputType) *domaingen.Job {
	return &domaingen.Job{
		ID:           id,
		TenantID:     "tenant-test",
		MediaID:      "media-test",
		OutputType:   outputType,
		Tier:         domaingen.TierFree,
		Status:       domaingen.StatusRunning,
		CurrentStage: domaingen.StagePromptPrepare,
		StageVersion: 2,
		Prompt:       "raw prompt",
		Provider:     "simulated",
		Model:        "simulated-v1",
		Resolution:   "1024x1024",
		VariantCount: 1,
	}
}

type recordingEnhancer struct {
	out   EnhanceOutput
	err   error
	calls int
}

func (e *recordingEnhancer) Enhance(context.Context, EnhanceInput) (EnhanceOutput, error) {
	e.calls++
	return e.out, e.err
}

type preprocessSealer struct{}

func (preprocessSealer) Seal(_ context.Context, _, _ string, plaintext string) ([]byte, error) {
	return []byte("sealed:" + plaintext), nil
}

func (preprocessSealer) Unseal(_ context.Context, _, _ string, ciphertext []byte) (string, error) {
	return strings.TrimPrefix(string(ciphertext), "sealed:"), nil
}

func collectPromptEnhancementMetrics(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	out := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "workflow.prompt_enhancement_attempts_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("prompt enhancement metric data = %T, want Sum[int64]", m.Data)
			}
			for _, dp := range sum.DataPoints {
				out[promptEnhancementMetricKey(dp.Attributes.ToSlice())] += dp.Value
			}
		}
	}
	return out
}

func promptEnhancementMetricKey(attrs []attribute.KeyValue) string {
	parts := map[string]string{}
	for _, attr := range attrs {
		parts[string(attr.Key)] = attr.Value.AsString()
	}
	return parts["outcome"] + "|" + parts["output_type"] + "|" + parts["policy_version"]
}
