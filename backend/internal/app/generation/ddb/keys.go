// Package ddb is the DynamoDB-backed adapter for the generation app's
// persistence ports (JobRepository, idempotency.Store, ArtifactSink,
// StagedArtifactStore, ResourceLessor). Each port's interface
// lives in app/generation (or app/idempotency); this package supplies one
// concrete implementation per port.
//
// Idempotency-claim row keys and the completed-claim builder live in
// app/idempotency/persist; this package consumes them rather than redefining
// the layout so one package owns the claim row shape.
package ddb

import "strconv"

// JobPK returns the job-owned workflow partition key.
func JobPK(jobID string) string { return "JOB#" + jobID }

const (
	JobSK      = "JOB"
	TerminalSK = "TERMINAL"
)

func StageAttemptSK(stage string, version uint64, attempt int) string {
	return "ATTEMPT#" + stage + "#V#" + u64(version) + "#A#" + itoa(attempt)
}

func ProviderRequestSK(requestID string) string { return "PROVIDER_REQUEST#" + requestID }

func PromptEnhancementSK(ref string) string { return "PROMPT_ENHANCEMENT#" + ref }

func GenerationSK() string { return "GENERATION" }

func identitySuffix(jobID string) string {
	if len(jobID) >= 4 && jobID[:4] == "gen_" {
		return jobID[4:]
	}
	return jobID
}

func GenerationID(jobID string) string { return "gen_" + identitySuffix(jobID) }

func OutputID(jobID string) string { return "out_" + identitySuffix(jobID) }

func VariantID(jobID string, index int) string {
	return "var_" + identitySuffix(jobID) + "_" + strconv.Itoa(index)
}

func OutputSK(outputID string) string { return "OUTPUT#" + outputID }

func VariantSK(variantID string) string { return "VARIANT#" + variantID }

// StagedPK returns the partition key for a staged artifact row.
func StagedPK(_, jobID string) string { return JobPK(jobID) }

func u64(v uint64) string { return strconv.FormatUint(v, 10) }

func itoa(v int) string { return strconv.Itoa(v) }

// AuditGatePK returns the partition key for an immutable gate audit row.
func AuditGatePK(jobID string) string { return "AUDIT#GATE#" + jobID }
