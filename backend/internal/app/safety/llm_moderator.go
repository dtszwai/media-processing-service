package safety

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/safety"
	"github.com/dtszwai/media-processing-service/backend/internal/util/randid"
)

const defaultLLMModeratorPolicyVersion = "llm-moderation-v1"

var errLLMModerationProviderFailed = errors.New("llm moderator: provider call failed")

type ModerationModel interface {
	Moderate(ctx context.Context, req ModerationModelRequest) (ModerationModelResult, error)
}

type ModerationModelRequest struct {
	PolicyVersion string
	Layer         safety.Layer
	OutputType    generation.OutputType
	Model         string
	Prompt        string
	Artifact      *generation.Artifact
}

type ModerationModelResult struct {
	Decision            safety.Decision
	Categories          map[string]float64
	ReasonCode          string
	Provider            string
	Model               string
	EvidenceHashes      []string
	ServiceCostMicroUSD int64
	VendorRequestID     string
}

// Stage-level idempotency in generation already guards the external call, so
// LLMModerator deliberately does not create a second claim row.
type LLMModerator struct {
	Model         ModerationModel
	PolicyVersion string
	Clock         func() time.Time
	NewID         func() string
	UsageMeter    ServiceCostMeter
}

func (m *LLMModerator) Moderate(ctx context.Context, in ModerateInput) (safety.Verdict, error) {
	if m == nil || m.Model == nil {
		return safety.Verdict{}, errors.New("llm moderator: model required")
	}
	if v, ok := m.prefilter(in); ok {
		return v, nil
	}
	out, err := m.Model.Moderate(ctx, ModerationModelRequest{
		PolicyVersion: m.policyVersion(),
		Layer:         in.Layer,
		OutputType:    in.OutputType,
		Model:         in.Model,
		Prompt:        in.Prompt,
		Artifact:      in.Artifact,
	})
	if err != nil {
		return safety.Verdict{}, errLLMModerationProviderFailed
	}
	m.recordCost(ctx, in, out)
	subjectType, subjectID := moderationSubject(in)
	return safety.Verdict{
		ID:             m.newID(),
		TenantID:       in.TenantID,
		SubjectType:    subjectType,
		SubjectID:      subjectID,
		Layer:          in.Layer,
		Decision:       out.Decision,
		Categories:     out.Categories,
		PolicyVersion:  m.policyVersion(),
		Provider:       out.Provider,
		Model:          out.Model,
		EvidenceHashes: out.EvidenceHashes,
		ReasonCode:     out.ReasonCode,
		CreatedAt:      m.now(),
	}, nil
}

func (m *LLMModerator) prefilter(in ModerateInput) (safety.Verdict, bool) {
	if !strings.Contains(in.Prompt, SimulatedSentinel) && !artifactMetadataContains(in.Artifact, SimulatedSentinel) {
		return safety.Verdict{}, false
	}
	subjectType, subjectID := moderationSubject(in)
	return safety.Verdict{
		ID:            m.newID(),
		TenantID:      in.TenantID,
		SubjectType:   subjectType,
		SubjectID:     subjectID,
		Layer:         in.Layer,
		Decision:      safety.DecisionFail,
		PolicyVersion: m.policyVersion(),
		Provider:      "prefilter",
		Model:         "keyword-v1",
		ReasonCode:    "MODERATION_PREFILTER_BLOCK",
		CreatedAt:     m.now(),
	}, true
}

func (m *LLMModerator) recordCost(ctx context.Context, in ModerateInput, out ModerationModelResult) {
	if m.UsageMeter == nil || out.ServiceCostMicroUSD <= 0 || in.JobID == "" {
		return
	}
	source := ServiceCostSourceInputModeration
	if in.Layer == safety.LayerOutputModeration {
		source = ServiceCostSourceOutputModeration
	}
	requestID := out.VendorRequestID
	if requestID == "" {
		requestID = in.JobID
	}
	_ = m.UsageMeter.RecordServiceCost(ctx, in.JobID, source, requestID, out.ServiceCostMicroUSD)
}

func moderationSubject(in ModerateInput) (string, string) {
	if in.Artifact != nil {
		return "ARTIFACT", in.Artifact.SHA256
	}
	return "PROMPT", in.JobID
}

func artifactMetadataContains(a *generation.Artifact, needle string) bool {
	if a == nil {
		return false
	}
	for _, v := range a.Metadata {
		if strings.Contains(v, needle) {
			return true
		}
	}
	return false
}

func (m *LLMModerator) policyVersion() string {
	if m.PolicyVersion != "" {
		return m.PolicyVersion
	}
	return defaultLLMModeratorPolicyVersion
}

func (m *LLMModerator) now() time.Time {
	if m.Clock != nil {
		return m.Clock().UTC()
	}
	return time.Now().UTC()
}

func (m *LLMModerator) newID() string {
	if m.NewID != nil {
		return m.NewID()
	}
	return randid.New()
}
