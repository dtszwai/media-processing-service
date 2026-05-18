//go:build smoke

package smoke

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	connect "connectrpc.com/connect"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamodbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	awscfg "github.com/dtszwai/media-processing-service/backend/internal/conf/aws"
	generationpb "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/generation/v1"
	generationconnect "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/generation/v1/generationv1connect"
)

const (
	defaultSmokeAPIURL       = "http://localhost:9000"
	defaultSmokeAWSEndpoint  = "http://localhost:4566"
	defaultSmokeAWSRegion    = "us-east-1"
	defaultSmokeDDBTable     = "media-v1"
	defaultSmokeS3Bucket     = "media-service-local"
	defaultSmokeTenantID     = "tenant_smoke_local"
	defaultSmokeUserID       = "user_smoke_local"
	defaultSmokePromQueryURL = "http://localhost:3000/api/datasources/proxy/uid/prometheus/api/v1/query"
)

type smokeConfig struct {
	APIURL               string
	AWSEndpointURL       string
	AWSRegion            string
	DDBTable             string
	S3Bucket             string
	TenantID             string
	UserID               string
	PromQueryURL         string
	ExpectPromptEnhancer string
}

type smokeRun struct {
	idempotencyKey string
	jobID          string
	mediaID        string
	assetID        string
	storageKey     string
	promptSpec     string
	metricName     string
}

func TestGenerationLocalSmoke(t *testing.T) {
	cfg := loadSmokeConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()

	ddbClient, s3Client := awsClients(t, ctx, cfg)
	client := generationconnect.NewGenerationServiceClient(
		&http.Client{Timeout: 15 * time.Second},
		cfg.APIURL,
	)
	run := &smokeRun{}

	t.Run("preflight", func(t *testing.T) {
		preflight(t, ctx, cfg)
	})
	t.Run("submit", func(t *testing.T) {
		submitGeneration(t, ctx, client, cfg, run)
	})
	t.Run("poll", func(t *testing.T) {
		pollGeneration(t, ctx, client, cfg, run)
	})
	t.Run("ddb job row", func(t *testing.T) {
		assertJobRow(t, ctx, ddbClient, cfg, run)
	})
	t.Run("ddb asset row", func(t *testing.T) {
		assertAssetRow(t, ctx, ddbClient, cfg, run)
	})
	t.Run("s3 object", func(t *testing.T) {
		assertS3Object(t, ctx, s3Client, cfg, run)
	})
	t.Run("gate audit row", func(t *testing.T) {
		assertGateAuditRow(t, ctx, ddbClient, cfg, run)
	})
	t.Run("prometheus metric", func(t *testing.T) {
		assertPromptMetric(t, ctx, cfg, run)
	})
	t.Run("summary", func(t *testing.T) {
		t.Logf("jobId=%s mediaId=%s assetId=%s storageKey=%s promptSpecVersion=%s promptEnhancementMetric=%s",
			run.jobID, run.mediaID, run.assetID, run.storageKey, run.promptSpec, run.metricName)
	})
}

func TestGenerationLocalCleanup(t *testing.T) {
	cfg := loadSmokeConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	ddbClient, s3Client := awsClients(t, ctx, cfg)
	scanned, deletedRows := cleanupDDB(t, ctx, ddbClient, cfg)
	listedObjects, deletedObjects := cleanupS3(t, ctx, s3Client, cfg)
	t.Logf("smoke cleanup scanned_ddb_rows=%d deleted_ddb_rows=%d listed_s3_objects=%d deleted_s3_objects=%d",
		scanned, deletedRows, listedObjects, deletedObjects)
}

func loadSmokeConfig() smokeConfig {
	return smokeConfig{
		APIURL:               envOr("SMOKE_API_URL", defaultSmokeAPIURL),
		AWSEndpointURL:       envOr("AWS_ENDPOINT_URL", defaultSmokeAWSEndpoint),
		AWSRegion:            envOr("AWS_REGION", defaultSmokeAWSRegion),
		DDBTable:             envOr("DDB_TABLE", defaultSmokeDDBTable),
		S3Bucket:             envOr("S3_BUCKET", defaultSmokeS3Bucket),
		TenantID:             envOr("SMOKE_TENANT_ID", defaultSmokeTenantID),
		UserID:               envOr("SMOKE_USER_ID", defaultSmokeUserID),
		PromQueryURL:         envOr("SMOKE_PROM_URL", defaultSmokePromQueryURL),
		ExpectPromptEnhancer: os.Getenv("SMOKE_EXPECT_PROMPT_ENHANCER"),
	}
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func awsClients(t *testing.T, ctx context.Context, cfg smokeConfig) (*dynamodb.Client, *s3.Client) {
	t.Helper()
	awsConfig, err := awscfg.Load(ctx, awscfg.Options{
		Region:      cfg.AWSRegion,
		EndpointURL: cfg.AWSEndpointURL,
		AccessKey:   "test",
		SecretKey:   "test",
	})
	if err != nil {
		t.Fatalf("smoke aws config: %v", err)
	}
	return dynamodb.NewFromConfig(awsConfig), s3.NewFromConfig(awsConfig, func(o *s3.Options) {
		o.UsePathStyle = true
	})
}

func preflight(t *testing.T, ctx context.Context, cfg smokeConfig) {
	t.Helper()
	healthURL := strings.TrimRight(cfg.APIURL, "/") + "/healthz"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		t.Fatalf("preflight: health request %s: %v", healthURL, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("preflight: GET %s: %v", healthURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preflight: GET %s status=%s", healthURL, resp.Status)
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	args := []string{
		"compose", "-f", "deploy/compose/local.yaml",
		"ps", "--status", "running",
		"api", "outbox-relay", "generation-worker", "localstack", "grafana",
	}
	out, err := exec.CommandContext(cmdCtx, "docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("preflight: docker %s: %v\n%s", strings.Join(args, " "), err, string(out))
	}
	assertComposeServicesRunning(t, string(out), []string{"api", "outbox-relay", "generation-worker", "localstack", "grafana"})
}

func assertComposeServicesRunning(t *testing.T, psOutput string, services []string) {
	t.Helper()
	for _, service := range services {
		container := "msg-" + service
		if strings.Contains(psOutput, service) || strings.Contains(psOutput, container) {
			continue
		}
		t.Fatalf("preflight: compose service %s not running\n%s", service, psOutput)
	}
}

func submitGeneration(t *testing.T, ctx context.Context, client generationconnect.GenerationServiceClient, cfg smokeConfig, run *smokeRun) {
	t.Helper()
	stamp := time.Now().UTC().Format("20060102T150405.000000000")
	model := "simulated-v1"
	tier := "free"
	resolution := "1024x1024"
	outputType := generationpb.OutputType_OUTPUT_TYPE_IMAGE
	run.idempotencyKey = "smoke-local-generation-" + stamp

	resp, err := client.CreateGeneration(ctx, connect.NewRequest(&generationpb.CreateGenerationRequest{
		Prompt:         "smoke-local image generation " + stamp,
		Model:          &model,
		Resolution:     &resolution,
		Tier:           &tier,
		IdempotencyKey: run.idempotencyKey,
		OutputType:     &outputType,
		Provider:       "simulated",
	}))
	if err != nil {
		t.Fatalf("submit: CreateGeneration idempotency_key=%s: %v", run.idempotencyKey, err)
	}
	gen := resp.Msg.GetGeneration()
	if gen == nil {
		t.Fatalf("submit: CreateGeneration returned nil generation")
	}
	run.jobID = gen.GetJobId()
	run.mediaID = gen.GetMediaId()
	if run.jobID == "" || run.mediaID == "" {
		t.Fatalf("submit: empty job/media ids: generation=%v", gen)
	}
	if gen.GetTenantId() != "" && gen.GetTenantId() != cfg.TenantID {
		t.Fatalf("submit: tenant_id=%q want %q", gen.GetTenantId(), cfg.TenantID)
	}
}

func pollGeneration(t *testing.T, ctx context.Context, client generationconnect.GenerationServiceClient, cfg smokeConfig, run *smokeRun) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	var last *generationpb.Generation
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.GetGeneration(ctx, connect.NewRequest(&generationpb.GetGenerationRequest{JobId: run.jobID}))
		if err != nil {
			lastErr = err
			time.Sleep(2 * time.Second)
			continue
		}
		last = resp.Msg.GetGeneration()
		if last == nil {
			lastErr = errors.New("nil generation")
			time.Sleep(2 * time.Second)
			continue
		}
		switch last.GetStatus() {
		case "COMPLETE":
			if last.GetTenantId() != cfg.TenantID {
				t.Fatalf("poll: tenant_id=%q want %q generation=%v", last.GetTenantId(), cfg.TenantID, last)
			}
			if last.GetJobId() == "" || last.GetMediaId() == "" {
				t.Fatalf("poll: missing job/media id generation=%v", last)
			}
			if last.GetStage() != "TERMINAL" {
				t.Fatalf("poll: stage=%q want TERMINAL generation=%v", last.GetStage(), last)
			}
			run.jobID = last.GetJobId()
			run.mediaID = last.GetMediaId()
			return
		case "FAILED", "CANCELLED":
			t.Fatalf("poll: terminal status=%s code=%s message=%s generation=%v",
				last.GetStatus(), last.GetErrorCode(), last.GetErrorMessage(), last)
		default:
			time.Sleep(2 * time.Second)
		}
	}
	t.Fatalf("poll: timed out waiting for COMPLETE job_id=%s last=%v last_err=%v", run.jobID, last, lastErr)
}

func assertJobRow(t *testing.T, ctx context.Context, client *dynamodb.Client, cfg smokeConfig, run *smokeRun) {
	t.Helper()
	var row struct {
		TenantID                 string `dynamodbav:"tenant_id"`
		MediaID                  string `dynamodbav:"media_id"`
		Status                   string `dynamodbav:"status"`
		CurrentStage             string `dynamodbav:"current_stage"`
		PromptSpecVersion        string `dynamodbav:"prompt_spec_version"`
		ResultAssetID            string `dynamodbav:"result_asset_id"`
		PromptEnhancementApplied bool   `dynamodbav:"prompt_enhancement_applied"`
		PromptEnhancementRef     string `dynamodbav:"prompt_enhancement_ref"`
	}
	getRow(t, ctx, client, cfg.DDBTable, "JOB#"+run.jobID, "JOB", &row)
	if row.TenantID != cfg.TenantID {
		t.Fatalf("ddb job row: tenant_id=%q want %q", row.TenantID, cfg.TenantID)
	}
	if row.Status != "COMPLETE" || row.CurrentStage != "TERMINAL" {
		t.Fatalf("ddb job row: status/stage=%s/%s want COMPLETE/TERMINAL", row.Status, row.CurrentStage)
	}
	if !strings.HasPrefix(row.PromptSpecVersion, "prompt-policy-v1+enhancer-") {
		t.Fatalf("ddb job row: prompt_spec_version=%q", row.PromptSpecVersion)
	}
	if row.ResultAssetID == "" {
		t.Fatalf("ddb job row: result_asset_id is empty")
	}
	wantSpec := "prompt-policy-v1+enhancer-passthrough-v0"
	if cfg.ExpectPromptEnhancer == "simulated" {
		wantSpec = "prompt-policy-v1+enhancer-llm-enhance-v1"
		if !row.PromptEnhancementApplied {
			t.Fatalf("ddb job row: prompt_enhancement_applied=false want true")
		}
		if row.PromptEnhancementRef == "" {
			t.Fatalf("ddb job row: prompt_enhancement_ref is empty")
		}
	}
	if row.PromptSpecVersion != wantSpec {
		t.Fatalf("ddb job row: prompt_spec_version=%q want %q", row.PromptSpecVersion, wantSpec)
	}
	if row.MediaID != "" {
		run.mediaID = row.MediaID
	}
	run.assetID = row.ResultAssetID
	run.promptSpec = row.PromptSpecVersion
}

func assertAssetRow(t *testing.T, ctx context.Context, client *dynamodb.Client, cfg smokeConfig, run *smokeRun) {
	t.Helper()
	var row struct {
		TenantID   string `dynamodbav:"tenant_id"`
		Lifecycle  string `dynamodbav:"lifecycle"`
		StorageKey string `dynamodbav:"storage_key"`
		Provenance *struct {
			AIGenerated bool   `dynamodbav:"ai_generated"`
			Disclosure  string `dynamodbav:"disclosure"`
			Watermark   string `dynamodbav:"watermark"`
			Safety      string `dynamodbav:"safety"`
		} `dynamodbav:"provenance"`
	}
	getRow(t, ctx, client, cfg.DDBTable, mediaPK(cfg.TenantID, run.mediaID), "ASSET#"+run.assetID, &row)
	if row.TenantID != cfg.TenantID {
		t.Fatalf("ddb asset row: tenant_id=%q want %q", row.TenantID, cfg.TenantID)
	}
	if row.Lifecycle != "COMPLETE" {
		t.Fatalf("ddb asset row: lifecycle=%q want COMPLETE", row.Lifecycle)
	}
	if row.StorageKey == "" {
		t.Fatalf("ddb asset row: storage_key is empty")
	}
	if row.Provenance == nil {
		t.Fatalf("ddb asset row: provenance is nil")
	}
	if !row.Provenance.AIGenerated || row.Provenance.Disclosure == "" || row.Provenance.Watermark == "" || row.Provenance.Safety == "" {
		t.Fatalf("ddb asset row: incomplete provenance=%+v", row.Provenance)
	}
	run.storageKey = row.StorageKey
}

func assertS3Object(t *testing.T, ctx context.Context, client *s3.Client, cfg smokeConfig, run *smokeRun) {
	t.Helper()
	out, err := client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(cfg.S3Bucket),
		Key:    aws.String(run.storageKey),
	})
	if err != nil {
		t.Fatalf("s3 object: HeadObject s3://%s/%s: %v", cfg.S3Bucket, run.storageKey, err)
	}
	for _, key := range []string{"disclosure", "visible_watermark", "watermark.fingerprint", "watermark.algo", "content_safety"} {
		if strings.TrimSpace(out.Metadata[key]) == "" {
			t.Fatalf("s3 object: metadata[%q] is empty; metadata=%v", key, out.Metadata)
		}
	}
}

func assertGateAuditRow(t *testing.T, ctx context.Context, client *dynamodb.Client, cfg smokeConfig, run *smokeRun) {
	t.Helper()
	out, err := client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(cfg.DDBTable),
		ConsistentRead:         aws.Bool(true),
		KeyConditionExpression: aws.String("PK = :pk"),
		ExpressionAttributeValues: map[string]dynamodbtypes.AttributeValue{
			":pk": &dynamodbtypes.AttributeValueMemberS{Value: "AUDIT#GATE#" + run.jobID},
		},
	})
	if err != nil {
		t.Fatalf("gate audit row: Query AUDIT#GATE#%s: %v", run.jobID, err)
	}
	for _, item := range out.Items {
		var row struct {
			Decision          string `dynamodbav:"decision"`
			DisclosurePresent bool   `dynamodbav:"disclosure_present"`
			WatermarkPresent  bool   `dynamodbav:"watermark_present"`
			SafetyPresent     bool   `dynamodbav:"safety_present"`
		}
		if err := attributevalue.UnmarshalMap(item, &row); err != nil {
			t.Fatalf("gate audit row: unmarshal: %v item=%v", err, item)
		}
		if row.Decision == "PASS" && row.DisclosurePresent && row.WatermarkPresent && row.SafetyPresent {
			return
		}
	}
	t.Fatalf("gate audit row: no PASS row with disclosure/watermark/safety present; rows=%d", len(out.Items))
}

func assertPromptMetric(t *testing.T, ctx context.Context, cfg smokeConfig, run *smokeRun) {
	t.Helper()
	metricName := discoverPromptMetric(t, ctx, cfg)
	policyVersion := "passthrough-v0"
	if cfg.ExpectPromptEnhancer == "simulated" {
		policyVersion = "llm-enhance-v1"
	}
	query := fmt.Sprintf(`sum(%s{output_type="IMAGE",policy_version="%s"})`, metricName, policyVersion)
	deadline := time.Now().Add(45 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		value, raw, err := prometheusQuery(ctx, cfg.PromQueryURL, query)
		last = raw
		if err == nil && value > 0 {
			run.metricName = metricName
			return
		}
		time.Sleep(3 * time.Second)
	}
	t.Fatalf("prometheus metric: query %q did not become visible at %s; last=%s", query, cfg.PromQueryURL, last)
}

func discoverPromptMetric(t *testing.T, ctx context.Context, cfg smokeConfig) string {
	t.Helper()
	seriesURL, err := prometheusSeriesURL(cfg.PromQueryURL)
	if err != nil {
		t.Fatalf("prometheus metric: series url from %q: %v", cfg.PromQueryURL, err)
	}
	deadline := time.Now().Add(45 * time.Second)
	var lastStatus string
	var lastNames []string
	for time.Now().Before(deadline) {
		names, status := fetchPromptMetricNames(t, ctx, seriesURL)
		if len(names) == 1 {
			return names[0]
		}
		lastStatus = status
		lastNames = names
		time.Sleep(3 * time.Second)
	}
	t.Fatalf("prometheus metric: prompt enhancement metric names=%v status=%s", lastNames, lastStatus)
	return ""
}

func fetchPromptMetricNames(t *testing.T, ctx context.Context, seriesURL string) ([]string, string) {
	t.Helper()
	u, err := url.Parse(seriesURL)
	if err != nil {
		t.Fatalf("prometheus metric: parse series url %q: %v", seriesURL, err)
	}
	q := u.Query()
	q.Set("match[]", `{__name__=~".*prompt.*enhancement.*"}`)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		t.Fatalf("prometheus metric: series request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("prometheus metric: series query %s: %v", u.String(), err)
	}
	defer resp.Body.Close()
	var body struct {
		Status string              `json:"status"`
		Data   []map[string]string `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("prometheus metric: decode series response: %v", err)
	}
	names := map[string]struct{}{}
	for _, series := range body.Data {
		name := series["__name__"]
		if normalizedMetricName(name) == "workflow_prompt_enhancement_attempts_total" {
			names[name] = struct{}{}
		}
	}
	return sortedKeys(names), body.Status
}

func prometheusSeriesURL(queryURL string) (string, error) {
	if strings.HasSuffix(queryURL, "/api/v1/query") {
		return strings.TrimSuffix(queryURL, "/api/v1/query") + "/api/v1/series", nil
	}
	u, err := url.Parse(queryURL)
	if err != nil {
		return "", err
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/v1/series"
	u.RawQuery = ""
	return u.String(), nil
}

func prometheusQuery(ctx context.Context, queryURL, query string) (float64, string, error) {
	u, err := url.Parse(queryURL)
	if err != nil {
		return 0, "", err
	}
	q := u.Query()
	q.Set("query", query)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	var body struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Value []any `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, "", err
	}
	raw, _ := json.Marshal(body)
	if body.Status != "success" || len(body.Data.Result) == 0 || len(body.Data.Result[0].Value) < 2 {
		return 0, string(raw), errors.New("empty prometheus result")
	}
	value, ok := body.Data.Result[0].Value[1].(string)
	if !ok {
		return 0, string(raw), errors.New("unexpected prometheus value")
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, string(raw), err
	}
	return parsed, string(raw), nil
}

func getRow(t *testing.T, ctx context.Context, client *dynamodb.Client, table, pk, sk string, out any) {
	t.Helper()
	item, err := client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      aws.String(table),
		ConsistentRead: aws.Bool(true),
		Key: map[string]dynamodbtypes.AttributeValue{
			"PK": &dynamodbtypes.AttributeValueMemberS{Value: pk},
			"SK": &dynamodbtypes.AttributeValueMemberS{Value: sk},
		},
	})
	if err != nil {
		t.Fatalf("ddb row: GetItem PK=%s SK=%s: %v", pk, sk, err)
	}
	if len(item.Item) == 0 {
		t.Fatalf("ddb row: missing PK=%s SK=%s", pk, sk)
	}
	if err := attributevalue.UnmarshalMap(item.Item, out); err != nil {
		t.Fatalf("ddb row: unmarshal PK=%s SK=%s: %v item=%v", pk, sk, err, item.Item)
	}
}

func cleanupDDB(t *testing.T, ctx context.Context, client *dynamodb.Client, cfg smokeConfig) (int, int) {
	t.Helper()
	paginator := dynamodb.NewScanPaginator(client, &dynamodb.ScanInput{
		TableName: aws.String(cfg.DDBTable),
	})
	type rowKey struct {
		PK string
		SK string
	}
	var rows []map[string]dynamodbtypes.AttributeValue
	jobIDs := map[string]struct{}{}
	scanned := 0
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			t.Fatalf("smoke cleanup ddb: scan: %v", err)
		}
		for _, item := range page.Items {
			scanned++
			rows = append(rows, item)
			if attrString(item, "tenant_id") == cfg.TenantID {
				if jobID := attrString(item, "job_id"); jobID != "" {
					jobIDs[jobID] = struct{}{}
				}
				if strings.HasPrefix(attrString(item, "PK"), "JOB#") {
					jobIDs[strings.TrimPrefix(attrString(item, "PK"), "JOB#")] = struct{}{}
				}
			}
		}
	}

	deleteKeys := map[rowKey]struct{}{}
	for _, item := range rows {
		pk := attrString(item, "PK")
		sk := attrString(item, "SK")
		switch {
		case attrString(item, "tenant_id") == cfg.TenantID:
			deleteKeys[rowKey{PK: pk, SK: sk}] = struct{}{}
		case strings.HasPrefix(pk, "IDEMPOTENCY#SUBMIT#"+cfg.TenantID+"#smoke-local-"):
			deleteKeys[rowKey{PK: pk, SK: sk}] = struct{}{}
		case smokeWorkflowClaim(pk, jobIDs):
			deleteKeys[rowKey{PK: pk, SK: sk}] = struct{}{}
		}
	}

	deleted := 0
	for key := range deleteKeys {
		_, err := client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
			TableName: aws.String(cfg.DDBTable),
			Key: map[string]dynamodbtypes.AttributeValue{
				"PK": &dynamodbtypes.AttributeValueMemberS{Value: key.PK},
				"SK": &dynamodbtypes.AttributeValueMemberS{Value: key.SK},
			},
		})
		if err != nil {
			t.Fatalf("smoke cleanup ddb: delete PK=%s SK=%s: %v", key.PK, key.SK, err)
		}
		deleted++
	}
	return scanned, deleted
}

func smokeWorkflowClaim(pk string, jobIDs map[string]struct{}) bool {
	if !strings.HasPrefix(pk, "IDEMPOTENCY#GEN#") {
		return false
	}
	for jobID := range jobIDs {
		if strings.HasPrefix(pk, "IDEMPOTENCY#GEN#"+jobID+"#") {
			return true
		}
	}
	return false
}

func cleanupS3(t *testing.T, ctx context.Context, client *s3.Client, cfg smokeConfig) (int, int) {
	t.Helper()
	listed, deleted := 0, 0
	for _, prefix := range []string{
		cfg.TenantID + "/",
		"provider-staging/generation/" + cfg.TenantID + "/",
	} {
		paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
			Bucket: aws.String(cfg.S3Bucket),
			Prefix: aws.String(prefix),
		})
		for paginator.HasMorePages() {
			page, err := paginator.NextPage(ctx)
			if err != nil {
				t.Fatalf("smoke cleanup s3: list prefix=%s: %v", prefix, err)
			}
			for _, obj := range page.Contents {
				listed++
				_, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{
					Bucket: aws.String(cfg.S3Bucket),
					Key:    obj.Key,
				})
				if err != nil {
					t.Fatalf("smoke cleanup s3: delete %s: %v", aws.ToString(obj.Key), err)
				}
				deleted++
			}
		}
	}
	return listed, deleted
}

func attrString(item map[string]dynamodbtypes.AttributeValue, key string) string {
	if v, ok := item[key].(*dynamodbtypes.AttributeValueMemberS); ok {
		return v.Value
	}
	return ""
}

func mediaPK(tenantID, mediaID string) string {
	return "TENANT#" + tenantID + "#MEDIA#" + mediaID
}

func normalizedMetricName(name string) string {
	return strings.ReplaceAll(name, ".", "_")
}

func sortedKeys(in map[string]struct{}) []string {
	out := make([]string, 0, len(in))
	for key := range in {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
