package generation

import (
	"context"
	"errors"
	"maps"
	"sync"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
	"github.com/dtszwai/media-processing-service/backend/internal/util/randid"
)

// MemRepo is an in-memory JobRepository used by tests and the in-process
// stage poller. Production replaces it with the DDB JobRepository.
type MemRepo struct {
	mu                 sync.Mutex
	jobs               map[string]*generation.Job
	attempts           map[string][]StageAttempt
	providerRequests   map[string]ProviderRequest
	promptEnhancements map[string]PromptEnhancementRecord
	OutboxObserver     func(stage generation.Stage, body []byte)
}

func NewMemRepo() *MemRepo {
	return &MemRepo{
		jobs:               map[string]*generation.Job{},
		attempts:           map[string][]StageAttempt{},
		providerRequests:   map[string]ProviderRequest{},
		promptEnhancements: map[string]PromptEnhancementRecord{},
	}
}

func (r *MemRepo) CreateJob(_ context.Context, j generation.Job) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.jobs[j.ID]; ok {
		return errors.New("memrepo: duplicate job id")
	}
	if j.CurrentStage == "" {
		j.CurrentStage = generation.StageInputModeration
	}
	if j.StageVersion == 0 {
		j.StageVersion = 1
	}
	c := j
	r.jobs[j.ID] = &c
	return nil
}

func (r *MemRepo) GetJob(_ context.Context, tenantID, jobID string) (*generation.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[jobID]
	if !ok {
		return nil, errors.New("memrepo: job not found")
	}
	if tenantID != "" && j.TenantID != tenantID {
		return nil, errors.New("memrepo: cross-tenant lookup")
	}
	c := *j
	return &c, nil
}

// AdvanceStageAndEnqueue applies the StageResult atomically. The mem impl
// honours the CurrentStage condition so tests cover the stale-message guard
// the DDB impl uses.
func (r *MemRepo) AdvanceStageAndEnqueue(_ context.Context, job *generation.Job, result StageResult) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	persisted, ok := r.jobs[job.ID]
	if !ok {
		return errors.New("memrepo: job not found")
	}
	if persisted.CurrentStage != job.CurrentStage {
		return errors.New("memrepo: stage transition condition failed")
	}
	if persisted.StageVersion != job.StageVersion {
		return errors.New("memrepo: stage version condition failed")
	}
	if result.NextStage == StageTerminal && result.TerminalError == nil && result.CompletedAt == nil {
		return errors.New("memrepo: terminal complete requires CompletedAt")
	}

	now := time.Now().UTC()
	r.recordAttemptLocked(job, result, now)

	applyMutations(persisted, result)
	persisted.UpdatedAt = now

	if result.NextStage != StageTerminal {
		persisted.Status = generation.StatusRunning
	}

	if r.OutboxObserver != nil && len(result.OutboxBody) > 0 {
		r.OutboxObserver(result.NextStage, result.OutboxBody)
	}
	return nil
}

func (r *MemRepo) LastStageAttempt(_ context.Context, tenantID, jobID string, stage generation.Stage) (StageAttempt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if tenantID != "" {
		j, ok := r.jobs[jobID]
		if !ok {
			return StageAttempt{}, errors.New("memrepo: job not found")
		}
		if j.TenantID != tenantID {
			return StageAttempt{}, errors.New("memrepo: cross-tenant lookup")
		}
	}
	var latest StageAttempt
	for _, attempt := range r.attempts[jobID] {
		if attempt.Stage != stage {
			continue
		}
		if latest.CreatedAt.IsZero() || attempt.CreatedAt.After(latest.CreatedAt) ||
			(attempt.CreatedAt.Equal(latest.CreatedAt) && attempt.AttemptNo > latest.AttemptNo) {
			latest = attempt
		}
	}
	if latest.CreatedAt.IsZero() {
		return StageAttempt{}, errors.New("memrepo: stage attempt not found")
	}
	return latest, nil
}

func (r *MemRepo) recordAttemptLocked(job *generation.Job, result StageResult, now time.Time) {
	attempt := StageAttempt{
		Stage:        job.CurrentStage,
		StageVersion: job.StageVersion,
		AttemptNo:    job.Attempts + 1,
		Result:       "SUCCESS",
		CreatedAt:    now,
	}
	if result.TerminalError != nil {
		attempt.Result = "TERMINAL_FAILURE"
		attempt.ErrorCode = result.TerminalError.Code
		attempt.ErrorMessage = result.TerminalError.Message
	} else if result.AttemptsDelta > 0 && result.NextStage == job.CurrentStage {
		attempt.Result = "TRANSIENT_FAILURE"
		if result.TransientError != nil {
			attempt.ErrorCode = result.TransientError.Code
			attempt.ErrorMessage = result.TransientError.Message
		}
	}
	r.attempts[job.ID] = append(r.attempts[job.ID], attempt)
}

func (r *MemRepo) PutProviderRequest(_ context.Context, req ProviderRequest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := req.JobID + "#" + req.ID
	if _, ok := r.providerRequests[key]; ok {
		return errors.New("memrepo: duplicate provider request")
	}
	r.providerRequests[key] = req
	return nil
}

func (r *MemRepo) UpdateProviderRequest(_ context.Context, _, jobID, requestID string, status ProviderRequestStatus, providerJobID string, reqErr error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := jobID + "#" + requestID
	req, ok := r.providerRequests[key]
	if !ok {
		return errors.New("memrepo: provider request not found")
	}
	req.Status = status
	req.ProviderJobID = providerJobID
	req.UpdatedAt = time.Now().UTC()
	if reqErr != nil {
		ge := generation.AsError(reqErr)
		req.ErrorCode = ge.Code
		req.ErrorMessage = reqErr.Error()
	}
	r.providerRequests[key] = req
	return nil
}

func (r *MemRepo) PutPromptEnhancement(_ context.Context, rec PromptEnhancementRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := rec.JobID + "#" + rec.Ref
	if existing, ok := r.promptEnhancements[key]; ok {
		if existing.RawPromptHash != rec.RawPromptHash {
			return errors.New("memrepo: prompt enhancement ref collision")
		}
		return nil
	}
	c := rec
	c.EncryptedPrompt = append([]byte(nil), rec.EncryptedPrompt...)
	r.promptEnhancements[key] = c
	return nil
}

func (r *MemRepo) GetPromptEnhancement(_ context.Context, tenantID, jobID, ref string) (PromptEnhancementRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.promptEnhancements[jobID+"#"+ref]
	if !ok || (tenantID != "" && rec.TenantID != tenantID) {
		return PromptEnhancementRecord{}, kv.ErrNotFound
	}
	rec.EncryptedPrompt = append([]byte(nil), rec.EncryptedPrompt...)
	return rec, nil
}

// MemIdempotency is the in-memory idempotency.Store that honours the token +
// lease contract.
type MemIdempotency struct {
	mu   sync.Mutex
	rows map[string]*memClaim
	Now  func() time.Time
}

type memClaim struct {
	inputHash  string
	status     idempotency.Status
	result     string
	errorCode  string
	claimToken string
	leaseUntil time.Time
	attempts   int
}

func NewMemIdempotency() *MemIdempotency {
	return &MemIdempotency{rows: map[string]*memClaim{}, Now: func() time.Time { return time.Now().UTC() }}
}

func (m *MemIdempotency) Claim(_ context.Context, scope, inputHash string, lease time.Duration) (idempotency.Outcome, string, error) {
	if scope == "" || inputHash == "" {
		return "", "", errors.New("idem: scope + inputHash required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.Now()
	row, ok := m.rows[scope]
	if !ok {
		token := randid.New()
		m.rows[scope] = &memClaim{
			inputHash:  inputHash,
			status:     idempotency.StatusClaimed,
			claimToken: token,
			leaseUntil: now.Add(lease),
			attempts:   1,
		}
		return idempotency.OutcomeNew, token, nil
	}
	if row.inputHash != inputHash {
		return idempotency.OutcomeConflict, "", nil
	}
	switch row.status {
	case idempotency.StatusCompleted:
		return idempotency.OutcomeReplayCompleted, "", nil
	case idempotency.StatusFailed:
		return idempotency.OutcomeReplayFailed, "", nil
	}
	if row.leaseUntil.After(now) {
		return idempotency.OutcomeReplayClaimedFresh, "", nil
	}
	return idempotency.OutcomeReplayClaimedStale, "", nil
}

func (m *MemIdempotency) Complete(_ context.Context, scope, token, ref string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[scope]
	if !ok {
		return errors.New("idem: missing row")
	}
	if row.claimToken != token {
		return errors.New("idem: token mismatch")
	}
	if row.status != idempotency.StatusClaimed {
		return errors.New("idem: claim is not active")
	}
	row.status = idempotency.StatusCompleted
	row.result = ref
	return nil
}

func (m *MemIdempotency) Fail(_ context.Context, scope, token, code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[scope]
	if !ok {
		return errors.New("idem: missing row")
	}
	if row.claimToken != token {
		return errors.New("idem: token mismatch")
	}
	if row.status != idempotency.StatusClaimed {
		return errors.New("idem: claim is not active")
	}
	row.status = idempotency.StatusFailed
	row.errorCode = code
	return nil
}

func (m *MemIdempotency) GetResult(_ context.Context, scope string) (string, idempotency.Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[scope]
	if !ok {
		return "", "", errors.New("idem: missing row")
	}
	return row.result, row.status, nil
}

func (m *MemIdempotency) Reclaim(_ context.Context, scope string, lease time.Duration) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[scope]
	if !ok {
		return "", errors.New("idem: missing row")
	}
	now := m.Now()
	if row.status != idempotency.StatusClaimed {
		return "", errors.New("idem: claim is not active")
	}
	if row.leaseUntil.After(now) {
		return "", errors.New("idem: lease still fresh")
	}
	newToken := randid.New()
	row.claimToken = newToken
	row.leaseUntil = now.Add(lease)
	row.attempts++
	return newToken, nil
}

func (m *MemIdempotency) Abandon(_ context.Context, scope, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[scope]
	if !ok {
		return nil
	}
	if row.claimToken != token {
		return errors.New("idem: token mismatch")
	}
	delete(m.rows, scope)
	return nil
}

// MemSink is a tiny in-memory ArtifactSink for tests; records each invocation.
type MemSink struct {
	mu          sync.Mutex
	Stored      []generation.Artifact
	NextAssetID string
}

func NewMemSink() *MemSink { return &MemSink{NextAssetID: "ast-mem-1"} }

func (s *MemSink) StoreFinalArtifact(_ context.Context, _ generation.Job, art generation.Artifact) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Stored = append(s.Stored, art)
	id := s.NextAssetID
	if id == "" {
		id = "ast-mem"
	}
	return id, nil
}

// MemStaging is the in-memory StagedArtifactStore used by tests and the
// in-process poller. Keyed by storage key so multiple jobs can stage
// concurrently without collision.
type MemStaging struct {
	mu      sync.Mutex
	objects map[string]stagedObject
	Now     func() time.Time
}

type stagedObject struct {
	bytes []byte
	ref   StagedRef
}

func NewMemStaging() *MemStaging {
	return &MemStaging{
		objects: map[string]stagedObject{},
		Now:     func() time.Time { return time.Now().UTC() },
	}
}

func (s *MemStaging) PutStaged(_ context.Context, j generation.Job, art generation.Artifact, ttl time.Duration) (StagedRef, error) {
	if j.ID == "" || j.TenantID == "" {
		return StagedRef{}, errors.New("mem staging: tenant + job id required")
	}
	ext := art.Extension
	if ext == "" {
		ext = "bin"
	}
	now := s.now()
	ref := StagedRef{
		StorageKey:  StagingKey(j, ext),
		TenantID:    j.TenantID,
		JobID:       j.ID,
		ContentType: art.ContentType,
		Extension:   ext,
		SHA256Hex:   art.SHA256,
		SizeBytes:   int64(len(art.Bytes)),
		Metadata:    maps.Clone(art.Metadata),
		CreatedAt:   now,
		ExpiresAt:   now.Add(ttl),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	buf := make([]byte, len(art.Bytes))
	copy(buf, art.Bytes)
	s.objects[ref.StorageKey] = stagedObject{bytes: buf, ref: ref}
	return ref, nil
}

func (s *MemStaging) LoadStaged(_ context.Context, ref StagedRef) (generation.Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	obj, ok := s.objects[ref.StorageKey]
	if !ok {
		return generation.Artifact{}, ErrStagedNotFound
	}
	if !obj.ref.ExpiresAt.IsZero() && !obj.ref.ExpiresAt.After(s.now()) {
		delete(s.objects, ref.StorageKey)
		return generation.Artifact{}, ErrStagedNotFound
	}
	buf := make([]byte, len(obj.bytes))
	copy(buf, obj.bytes)
	// Read attributes from the stored ref (the canonical record), not from
	// the caller's ref — callers reconstruct the ref from just the storage
	// key on the replay path and don't carry the original provider metadata.
	return generation.Artifact{
		Bytes:       buf,
		ContentType: obj.ref.ContentType,
		Extension:   obj.ref.Extension,
		SHA256:      obj.ref.SHA256Hex,
		Metadata:    maps.Clone(obj.ref.Metadata),
	}, nil
}

func (s *MemStaging) DeleteStaged(_ context.Context, ref StagedRef) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, ref.StorageKey)
	return nil
}

// Drop forcibly evicts the staged bytes so LoadStaged returns ErrStagedNotFound.
// Used by tests to simulate the S3 lifecycle sweep that runs after ExpiresAt.
func (s *MemStaging) Drop(storageKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, storageKey)
}

func (s *MemStaging) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

// MemResourceLessor enforces a per-resource-class concurrency cap in-process.
// Used in tests and the local-only path; production swaps in a DDB-backed
// implementation.
type MemResourceLessor struct {
	mu      sync.Mutex
	caps    map[generation.ResourceClass]int
	current map[generation.ResourceClass]int
	leases  map[string]generation.ResourceClass
	clock   func() time.Time
}

func NewMemResourceLessor(caps map[generation.ResourceClass]int) *MemResourceLessor {
	return &MemResourceLessor{
		caps:    caps,
		current: map[generation.ResourceClass]int{},
		leases:  map[string]generation.ResourceClass{},
		clock:   func() time.Time { return time.Now().UTC() },
	}
}

func (l *MemResourceLessor) AcquireResource(_ context.Context, req LeaseRequest) (*ResourceLease, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	max, ok := l.caps[req.ResourceClass]
	if !ok {
		max = 1
	}
	if l.current[req.ResourceClass] >= max {
		return nil, errors.New("RESOURCE_CAPACITY_UNAVAILABLE")
	}
	id := "lease_" + randid.New()
	l.current[req.ResourceClass]++
	l.leases[id] = req.ResourceClass
	ttl := req.TTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &ResourceLease{
		ID:            id,
		ResourceClass: req.ResourceClass,
		TenantID:      req.TenantID,
		JobID:         req.JobID,
		ExpiresAt:     l.clock().Add(ttl),
	}, nil
}

func (l *MemResourceLessor) ReleaseResource(_ context.Context, lease *ResourceLease) error {
	if lease == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	class, ok := l.leases[lease.ID]
	if !ok {
		return nil
	}
	delete(l.leases, lease.ID)
	if l.current[class] > 0 {
		l.current[class]--
	}
	return nil
}
