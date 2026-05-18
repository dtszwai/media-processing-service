// Package app is the single source of truth for non-secret, non-generated
// application configuration. Values come from (lowest to highest precedence):
// embedded defaults, then environment variables.
//
// Secrets and Terraform-generated values (ARNs, KMS IDs, signing keys)
// intentionally live outside this package — bootstrap reads them directly from
// env so they never appear in a checked-in YAML file.
package app

import (
	"bytes"
	"embed"
	"fmt"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

//go:embed default.yaml
var embeddedConfigs embed.FS

// Config is the structured top-level config.
type Config struct {
	// Env is a free-form deployment label surfaced as the OTel
	// `deployment.environment` semantic attribute. Defaulted to "local"; set
	// MSG_ENV to override.
	Env         string            `mapstructure:"env"`
	AWS         AWSConfig         `mapstructure:"aws"`
	API         APIConfig         `mapstructure:"api"`
	Generation  GenerationConfig  `mapstructure:"generation"`
	Safety      SafetyConfig      `mapstructure:"safety"`
	Quota       QuotaConfig       `mapstructure:"quota"`
	LeaseReaper LeaseReaperConfig `mapstructure:"lease_reaper"`
	Telemetry   TelemetryConfig   `mapstructure:"telemetry"`
}

// AWSConfig groups the non-secret AWS topology — resource names, region,
// LocalStack endpoint override. Topic/queue ARNs are NOT here: those are
// Terraform-injected and stay in env.
type AWSConfig struct {
	Region      string `mapstructure:"region"`
	EndpointURL string `mapstructure:"endpoint_url"`
	// Externally-reachable host for presigned URLs; rewritten from
	// EndpointURL when set. Empty in production.
	PublicEndpointURL string     `mapstructure:"public_endpoint_url"`
	DDBTable          string     `mapstructure:"ddb_table"`
	S3Bucket          string     `mapstructure:"s3_bucket"`
	Topics            TopicNames `mapstructure:"topics"`
	Queues            QueueNames `mapstructure:"queues"`
}

type TopicNames struct {
	Media        string `mapstructure:"media"`
	MediaCleanup string `mapstructure:"media_cleanup"`
	Generation   string `mapstructure:"generation"`
	Analytics    string `mapstructure:"analytics"`
}

type QueueNames struct {
	Media             string `mapstructure:"media"`
	MediaCleanup      string `mapstructure:"media_cleanup"`
	MediaUploadEvents string `mapstructure:"media_upload_events"`
	Analytics         string `mapstructure:"analytics"`
	Webhook           string `mapstructure:"webhook"`
}

// APIConfig groups HTTP server + middleware knobs for cmd/api.
type APIConfig struct {
	Addr            string `mapstructure:"addr"`
	CORSOrigins     string `mapstructure:"cors_origins"`
	AuthEnforcement bool   `mapstructure:"auth_enforcement"`
}

// GenerationConfig holds host-local generation backend config. The provider
// name is request-driven (clients pick from the catalog ListGenerationModels
// returns); only adapter wiring lives here. NotebookLM paths stay empty by
// default; the consumer (genprovider_registry) fills in $HOME-relative
// fallbacks since YAML can't express ~ portably.
type GenerationConfig struct {
	NotebookLM     NotebookLMConfig     `mapstructure:"notebooklm"`
	PromptEnhancer PromptEnhancerConfig `mapstructure:"prompt_enhancer"`
}

type PromptEnhancerConfig struct {
	Provider      string `mapstructure:"provider"`
	PolicyVersion string `mapstructure:"policy_version"`
	// TTLDays is the lifetime of a stored prompt-enhancement row, in days.
	// Zero leaves the LLMEnhancer default in place (30 days). The row is a
	// replay cache, not an audit row, so the value can stay short.
	TTLDays int `mapstructure:"ttl_days"`
}

type NotebookLMConfig struct {
	ScriptPath       string `mapstructure:"script_path"`
	StatePath        string `mapstructure:"state_path"`
	StateDisplayPath string `mapstructure:"state_display_path"`
	PythonBin        string `mapstructure:"python_bin"`
}

type SafetyConfig struct {
	Moderator ModeratorConfig `mapstructure:"moderator"`
}

type ModeratorConfig struct {
	Provider      string `mapstructure:"provider"`
	PolicyVersion string `mapstructure:"policy_version"`
}

// QuotaConfig holds the cap values seeded into the tenant cost Reservoir on
// first call. Only TenantCostCapMicroUSD is wired today; API-key, vendor,
// and service-global caps land alongside their call sites.
type QuotaConfig struct {
	TenantCostCapMicroUSD int64 `mapstructure:"tenant_cost_cap_micro_usd"`
}

// LeaseReaperConfig is the list of tenants the reaper scans each pass.
type LeaseReaperConfig struct {
	Tenants []string `mapstructure:"tenants"`
}

// TelemetryConfig groups OTel / slog knobs. OTLP endpoint stays here even
// though Terraform always provides it via env — keeping it in the schema
// gives operators a place to override on a single box for triage.
type TelemetryConfig struct {
	LogLevel      string  `mapstructure:"log_level"`
	TracesSampler float64 `mapstructure:"traces_sampler"`
	LogsDisabled  bool    `mapstructure:"logs_disabled"`
	OTLPEndpoint  string  `mapstructure:"otlp_endpoint"`
}

// Load returns the resolved config. It never panics — callers receive a
// non-nil error on malformed YAML or unparseable env values.
func Load() (Config, error) {
	v := viper.New()
	v.SetConfigType("yaml")

	defaultsBytes, err := embeddedConfigs.ReadFile("default.yaml")
	if err != nil {
		return Config{}, fmt.Errorf("app: read embedded default.yaml: %w", err)
	}
	if err := v.ReadConfig(bytes.NewReader(defaultsBytes)); err != nil {
		return Config{}, fmt.Errorf("app: parse default.yaml: %w", err)
	}

	bindEnvAliases(v)

	var cfg Config
	if err := v.Unmarshal(&cfg, viper.DecodeHook(mapstructure.ComposeDecodeHookFunc(
		mapstructure.StringToSliceHookFunc(","),
	))); err != nil {
		return Config{}, fmt.Errorf("app: unmarshal: %w", err)
	}
	cfg.LeaseReaper.Tenants = normalizeStringList(cfg.LeaseReaper.Tenants)
	return cfg, nil
}

// normalizeStringList trims whitespace, splits on internal whitespace (so
// space-separated and comma-separated env values both work), and dedupes.
// Returns nil on empty so callers can branch on the nil check.
func normalizeStringList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		for _, f := range strings.Fields(v) {
			if _, dup := seen[f]; dup {
				continue
			}
			seen[f] = struct{}{}
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// bindEnvAliases maps unprefixed env vars (DDB_TABLE, AWS_REGION, …) onto
// the dotted YAML keys so Terraform and shipping-environment variables drive
// the same config struct.
func bindEnvAliases(v *viper.Viper) {
	aliases := map[string]string{
		"env": "MSG_ENV",

		"aws.region":                     "AWS_REGION",
		"aws.endpoint_url":               "AWS_ENDPOINT_URL",
		"aws.public_endpoint_url":        "AWS_PUBLIC_ENDPOINT_URL",
		"aws.ddb_table":                  "DDB_TABLE",
		"aws.s3_bucket":                  "S3_BUCKET",
		"aws.topics.media":               "SNS_MEDIA_TOPIC",
		"aws.topics.media_cleanup":       "SNS_MEDIA_CLEANUP_TOPIC",
		"aws.topics.generation":          "SNS_GENERATION_TOPIC",
		"aws.topics.analytics":           "SNS_ANALYTICS_TOPIC",
		"aws.queues.media":               "SQS_MEDIA_QUEUE",
		"aws.queues.media_cleanup":       "SQS_MEDIA_CLEANUP_QUEUE",
		"aws.queues.media_upload_events": "SQS_MEDIA_UPLOAD_EVENTS_QUEUE",
		"aws.queues.analytics":           "SQS_ANALYTICS_QUEUE",
		"aws.queues.webhook":             "SQS_WEBHOOK_QUEUE",

		"api.addr":             "API_HTTP_ADDR",
		"api.cors_origins":     "CORS_ALLOWED_ORIGINS",
		"api.auth_enforcement": "AUTH_ENFORCEMENT_ENABLED",

		"generation.notebooklm.script_path":         "NOTEBOOKLM_SCRIPT_PATH",
		"generation.notebooklm.state_path":          "NOTEBOOKLM_STORAGE_STATE_PATH",
		"generation.notebooklm.state_display_path":  "NOTEBOOKLM_STORAGE_STATE_DISPLAY_PATH",
		"generation.notebooklm.python_bin":          "NOTEBOOKLM_PYTHON",
		"generation.prompt_enhancer.provider":       "PROMPT_ENHANCER_PROVIDER",
		"generation.prompt_enhancer.policy_version": "PROMPT_ENHANCER_POLICY_VERSION",
		"generation.prompt_enhancer.ttl_days":       "PROMPT_ENHANCER_TTL_DAYS",

		"safety.moderator.provider":       "SAFETY_MODERATOR_PROVIDER",
		"safety.moderator.policy_version": "SAFETY_MODERATOR_POLICY_VERSION",

		"quota.tenant_cost_cap_micro_usd": "QUOTA_TENANT_COST_CAP_MICRO_USD",

		"lease_reaper.tenants": "LEASE_REAPER_TENANTS",

		"telemetry.log_level":      "LOG_LEVEL",
		"telemetry.traces_sampler": "OTEL_TRACES_SAMPLER_ARG",
		"telemetry.logs_disabled":  "OTEL_LOGS_DISABLED",
		"telemetry.otlp_endpoint":  "OTEL_EXPORTER_OTLP_ENDPOINT",
	}
	for key, envName := range aliases {
		_ = v.BindEnv(key, envName)
	}
}
