package ddb

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

type expectedJobRow struct {
	PK                      string                `dynamodbav:"PK"`
	SK                      string                `dynamodbav:"SK"`
	ItemType                string                `dynamodbav:"item_type"`
	GSIJobPK                string                `dynamodbav:"gsi_job_pk"`
	GSIJobSK                string                `dynamodbav:"gsi_job_sk"`
	ID                      string                `dynamodbav:"id"`
	TenantID                string                `dynamodbav:"tenant_id"`
	UserID                  string                `dynamodbav:"user_id,omitempty"`
	MediaID                 string                `dynamodbav:"media_id,omitempty"`
	ResultAssetID           string                `dynamodbav:"result_asset_id,omitempty"`
	OutputType              generation.OutputType `dynamodbav:"output_type"`
	Tier                    generation.Tier       `dynamodbav:"tier"`
	Status                  generation.Status     `dynamodbav:"status"`
	CurrentStage            generation.Stage      `dynamodbav:"current_stage"`
	StageVersion            uint64                `dynamodbav:"stage_version"`
	Provider                string                `dynamodbav:"provider,omitempty"`
	Model                   string                `dynamodbav:"model,omitempty"`
	Resolution              string                `dynamodbav:"resolution,omitempty"`
	Seed                    int64                 `dynamodbav:"seed,omitempty"`
	VariantCount            int                   `dynamodbav:"variant_count,omitempty"`
	PreparedPromptHash      string                `dynamodbav:"prepared_prompt_hash,omitempty"`
	PromptSpecVersion       string                `dynamodbav:"prompt_spec_version,omitempty"`
	GenerationParamsHash    string                `dynamodbav:"generation_parameters_hash,omitempty"`
	Attempts                int                   `dynamodbav:"attempts,omitempty"`
	ProviderJobID           string                `dynamodbav:"provider_job_id,omitempty"`
	ProviderRequestID       string                `dynamodbav:"provider_request_id,omitempty"`
	BudgetDate              string                `dynamodbav:"budget_date,omitempty"`
	BudgetMicroUSD          int64                 `dynamodbav:"budget_micro_usd,omitempty"`
	CreatedAt               time.Time             `dynamodbav:"created_at"`
	UpdatedAt               time.Time             `dynamodbav:"updated_at"`
	CompletedAt             *time.Time            `dynamodbav:"completed_at,omitempty"`
	EncryptedPrompt         []byte                `dynamodbav:"encrypted_prompt,omitempty"`
	EncryptedPreparedPrompt []byte                `dynamodbav:"encrypted_prepared_prompt,omitempty"`
	ErrorCode               string                `dynamodbav:"error_code,omitempty"`
	ErrorMessage            string                `dynamodbav:"error_message,omitempty"`
}

func TestJobRowSealsPromptsAndPreservesDynamoDBShape(t *testing.T) {
	ctx := context.Background()
	created := time.Date(2026, 5, 17, 10, 11, 12, 13, time.UTC)
	updated := created.Add(time.Minute)
	completed := created.Add(10 * time.Minute)
	job := generation.Job{
		ID:                   "job_1",
		TenantID:             "tenant_1",
		UserID:               "user_1",
		MediaID:              "media_1",
		ResultAssetID:        "asset_1",
		OutputType:           generation.OutputImage,
		Tier:                 generation.TierPaid,
		Status:               generation.StatusRunning,
		CurrentStage:         generation.StageProviderSubmit,
		StageVersion:         7,
		Provider:             "codex",
		Model:                "image-1",
		Resolution:           "1024x768",
		Seed:                 12345,
		VariantCount:         3,
		Prompt:               "raw prompt",
		PreparedPrompt:       "prepared prompt",
		PreparedPromptHash:   "prepared-hash",
		PromptSpecVersion:    "prompt-v1",
		GenerationParamsHash: "params-hash",
		Attempts:             2,
		ProviderJobID:        "provider-job-1",
		ProviderRequestID:    "provider-request-1",
		BudgetDate:           "20260517",
		BudgetMicroUSD:       25000,
		Error:                &generation.Error{Code: "IGNORED", Message: "domain error is not a job-row field", Terminal: true},
		CreatedAt:            created,
		UpdatedAt:            updated,
		CompletedAt:          &completed,
	}
	repo := NewJobRepo(nil, prefixSealer{})
	row, err := repo.row(ctx, job)
	if err != nil {
		t.Fatalf("row: %v", err)
	}

	want := expectedJobRow{
		PK:                      JobPK(job.ID),
		SK:                      JobSK,
		ItemType:                "GEN",
		GSIJobPK:                "TENANT#" + job.TenantID + "#STATUS#" + string(job.Status),
		GSIJobSK:                job.CreatedAt.UTC().Format(time.RFC3339Nano) + "#" + job.ID,
		ID:                      job.ID,
		TenantID:                job.TenantID,
		UserID:                  job.UserID,
		MediaID:                 job.MediaID,
		ResultAssetID:           job.ResultAssetID,
		OutputType:              job.OutputType,
		Tier:                    job.Tier,
		Status:                  job.Status,
		CurrentStage:            job.CurrentStage,
		StageVersion:            job.StageVersion,
		Provider:                job.Provider,
		Model:                   job.Model,
		Resolution:              job.Resolution,
		Seed:                    job.Seed,
		VariantCount:            job.VariantCount,
		PreparedPromptHash:      job.PreparedPromptHash,
		PromptSpecVersion:       job.PromptSpecVersion,
		GenerationParamsHash:    job.GenerationParamsHash,
		Attempts:                job.Attempts,
		ProviderJobID:           job.ProviderJobID,
		ProviderRequestID:       job.ProviderRequestID,
		BudgetDate:              job.BudgetDate,
		BudgetMicroUSD:          job.BudgetMicroUSD,
		CreatedAt:               job.CreatedAt,
		UpdatedAt:               job.UpdatedAt,
		CompletedAt:             job.CompletedAt,
		EncryptedPrompt:         []byte("sealed:raw prompt"),
		EncryptedPreparedPrompt: []byte("sealed:prepared prompt"),
	}
	assertSameJobDynamoDBShape(t, row, want)

	av := mustMarshalJobRow(t, row)
	for _, plaintextAttr := range []string{"Prompt", "PreparedPrompt", "prompt", "prepared_prompt", "Error", "error"} {
		if _, ok := av[plaintextAttr]; ok {
			t.Fatalf("plaintext/domain-only attribute %q persisted in job row", plaintextAttr)
		}
	}

	expectedDomain := job
	expectedDomain.Prompt = ""
	expectedDomain.PreparedPrompt = ""
	expectedDomain.Error = nil
	if got := row.toDomain(); !reflect.DeepEqual(got, expectedDomain) {
		t.Fatalf("toDomain = %+v, want %+v", got, expectedDomain)
	}
}

func TestGetJobUnsealsPromptsAndProjectsError(t *testing.T) {
	ctx := context.Background()
	created := time.Date(2026, 5, 17, 10, 11, 12, 13, time.UTC)
	row := jobRowFromDomain(generation.Job{
		ID:           "job_1",
		TenantID:     "tenant_1",
		MediaID:      "media_1",
		OutputType:   generation.OutputImage,
		Tier:         generation.TierPaid,
		Status:       generation.StatusFailed,
		CurrentStage: generation.StageTerminal,
		StageVersion: 9,
		CreatedAt:    created,
		UpdatedAt:    created.Add(time.Minute),
	})
	row.EncryptedPrompt = []byte("sealed:raw prompt")
	row.EncryptedPreparedPrompt = []byte("sealed:prepared prompt")
	row.ErrorCode = "PROVIDER_FAILED"
	row.ErrorMessage = "provider failed"

	store := newJobRowStore(t, row)
	repo := NewJobRepo(store, prefixSealer{})

	got, err := repo.GetJob(ctx, "tenant_1", "job_1")
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got.Prompt != "raw prompt" || got.PreparedPrompt != "prepared prompt" {
		t.Fatalf("prompts = (%q, %q), want unsealed prompts", got.Prompt, got.PreparedPrompt)
	}
	if got.Error == nil || got.Error.Code != "PROVIDER_FAILED" || got.Error.Message != "provider failed" || !got.Error.Terminal {
		t.Fatalf("error projection = %#v, want terminal PROVIDER_FAILED", got.Error)
	}
}

type prefixSealer struct{}

func (prefixSealer) Seal(_ context.Context, _, _ string, plaintext string) ([]byte, error) {
	return []byte("sealed:" + plaintext), nil
}

func (prefixSealer) Unseal(_ context.Context, _, _ string, ciphertext []byte) (string, error) {
	text := string(ciphertext)
	if !strings.HasPrefix(text, "sealed:") {
		return "", errors.New("missing sealed prefix")
	}
	return strings.TrimPrefix(text, "sealed:"), nil
}

type jobRowStore struct {
	rows map[string]map[string]types.AttributeValue
}

func newJobRowStore(t *testing.T, rows ...jobRow) *jobRowStore {
	t.Helper()
	store := &jobRowStore{rows: map[string]map[string]types.AttributeValue{}}
	for _, row := range rows {
		store.rows[row.PK+"\x00"+row.SK] = mustMarshalJobRow(t, row)
	}
	return store
}

func (s *jobRowStore) Put(_ context.Context, item kv.Item, _ kv.PutOptions) error {
	row, ok := item.(jobRow)
	if !ok {
		return errors.New("jobRowStore: Put requires jobRow")
	}
	s.rows[row.PK+"\x00"+row.SK] = mustMarshalJobRowNoT(row)
	return nil
}

func (s *jobRowStore) Get(_ context.Context, key kv.Key, out any) error {
	row, ok := s.rows[key.PK+"\x00"+key.SK]
	if !ok {
		return kv.ErrNotFound
	}
	return attributevalue.UnmarshalMap(row, out)
}

func (s *jobRowStore) Update(context.Context, kv.UpdateOp) error {
	return errors.New("jobRowStore: Update unsupported")
}

func (s *jobRowStore) UpdateReturning(context.Context, kv.UpdateOp) (kv.UpdateOutput, error) {
	return kv.UpdateOutput{}, errors.New("jobRowStore: UpdateReturning unsupported")
}

func (s *jobRowStore) Delete(context.Context, kv.DeleteOp) error {
	return errors.New("jobRowStore: Delete unsupported")
}

func (s *jobRowStore) Query(context.Context, kv.QueryRequest) (kv.QueryResult, error) {
	return kv.QueryResult{}, errors.New("jobRowStore: Query unsupported")
}

func (s *jobRowStore) TransactWrite(context.Context, []kv.WriteOp) error {
	return errors.New("jobRowStore: TransactWrite unsupported")
}

func assertSameJobDynamoDBShape(t *testing.T, got, want any) {
	t.Helper()
	gotAV := mustMarshalJobRow(t, got)
	wantAV := mustMarshalJobRow(t, want)
	if !reflect.DeepEqual(gotAV, wantAV) {
		t.Fatalf("DynamoDB shape mismatch\ngot keys:  %v\nwant keys: %v\ngot:  %#v\nwant: %#v",
			jobAVKeys(gotAV), jobAVKeys(wantAV), gotAV, wantAV)
	}
}

func mustMarshalJobRow(t *testing.T, row any) map[string]types.AttributeValue {
	t.Helper()
	av, err := attributevalue.MarshalMap(row)
	if err != nil {
		t.Fatalf("marshal job row: %v", err)
	}
	return av
}

func mustMarshalJobRowNoT(row any) map[string]types.AttributeValue {
	av, err := attributevalue.MarshalMap(row)
	if err != nil {
		panic(err)
	}
	return av
}

func jobAVKeys(av map[string]types.AttributeValue) []string {
	keys := make([]string, 0, len(av))
	for k := range av {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
