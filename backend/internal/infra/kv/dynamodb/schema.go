package dynamodb

import (
	"context"
	"errors"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// DefaultTableName is the canonical single-table name.
const DefaultTableName = "media-v1"

// EnsureMediaTable creates the media-v1 single-table layout with its GSIs.
// Idempotent: returns nil if the table already exists. Intended for
// tests/local-up bootstrap only; production tables are owned by Terraform.
func EnsureMediaTable(ctx context.Context, c *dynamodb.Client, name string) error {
	desc, err := c.DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(name)})
	if err == nil && desc.Table != nil {
		return waitTableActive(ctx, c, name)
	}
	var rnf *types.ResourceNotFoundException
	if !errors.As(err, &rnf) {
		return err
	}

	attrs := []types.AttributeDefinition{
		{AttributeName: aws.String("PK"), AttributeType: types.ScalarAttributeTypeS},
		{AttributeName: aws.String("SK"), AttributeType: types.ScalarAttributeTypeS},
		{AttributeName: aws.String("gsi_job_pk"), AttributeType: types.ScalarAttributeTypeS},
		{AttributeName: aws.String("gsi_job_sk"), AttributeType: types.ScalarAttributeTypeS},
		{AttributeName: aws.String("gsi_tenant_media_pk"), AttributeType: types.ScalarAttributeTypeS},
		{AttributeName: aws.String("gsi_tenant_media_sk"), AttributeType: types.ScalarAttributeTypeS},
		{AttributeName: aws.String("gsi_lease_pk"), AttributeType: types.ScalarAttributeTypeS},
		{AttributeName: aws.String("gsi_lease_sk"), AttributeType: types.ScalarAttributeTypeS},
		{AttributeName: aws.String("gsi_lifecycle_pk"), AttributeType: types.ScalarAttributeTypeS},
		{AttributeName: aws.String("gsi_lifecycle_sk"), AttributeType: types.ScalarAttributeTypeS},
		{AttributeName: aws.String("gsi_audit_entity_pk"), AttributeType: types.ScalarAttributeTypeS},
		{AttributeName: aws.String("gsi_audit_entity_sk"), AttributeType: types.ScalarAttributeTypeS},
		{AttributeName: aws.String("gsi_audit_actor_pk"), AttributeType: types.ScalarAttributeTypeS},
		{AttributeName: aws.String("gsi_audit_actor_sk"), AttributeType: types.ScalarAttributeTypeS},
		{AttributeName: aws.String("gsi_asset_role_pk"), AttributeType: types.ScalarAttributeTypeS},
		{AttributeName: aws.String("gsi_asset_role_sk"), AttributeType: types.ScalarAttributeTypeS},
	}
	gsis := []types.GlobalSecondaryIndex{
		{
			IndexName: aws.String("gsi_job"),
			KeySchema: []types.KeySchemaElement{
				{AttributeName: aws.String("gsi_job_pk"), KeyType: types.KeyTypeHash},
				{AttributeName: aws.String("gsi_job_sk"), KeyType: types.KeyTypeRange},
			},
			Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll},
		},
		{
			IndexName: aws.String("gsi_tenant_media"),
			KeySchema: []types.KeySchemaElement{
				{AttributeName: aws.String("gsi_tenant_media_pk"), KeyType: types.KeyTypeHash},
				{AttributeName: aws.String("gsi_tenant_media_sk"), KeyType: types.KeyTypeRange},
			},
			Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll},
		},
		{
			IndexName: aws.String("gsi_lease_expiry"),
			KeySchema: []types.KeySchemaElement{
				{AttributeName: aws.String("gsi_lease_pk"), KeyType: types.KeyTypeHash},
				{AttributeName: aws.String("gsi_lease_sk"), KeyType: types.KeyTypeRange},
			},
			Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll},
		},
		{
			IndexName: aws.String("gsi_lifecycle"),
			KeySchema: []types.KeySchemaElement{
				{AttributeName: aws.String("gsi_lifecycle_pk"), KeyType: types.KeyTypeHash},
				{AttributeName: aws.String("gsi_lifecycle_sk"), KeyType: types.KeyTypeRange},
			},
			Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll},
		},
		{
			IndexName: aws.String("gsi_audit_entity"),
			KeySchema: []types.KeySchemaElement{
				{AttributeName: aws.String("gsi_audit_entity_pk"), KeyType: types.KeyTypeHash},
				{AttributeName: aws.String("gsi_audit_entity_sk"), KeyType: types.KeyTypeRange},
			},
			Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll},
		},
		{
			IndexName: aws.String("gsi_audit_actor"),
			KeySchema: []types.KeySchemaElement{
				{AttributeName: aws.String("gsi_audit_actor_pk"), KeyType: types.KeyTypeHash},
				{AttributeName: aws.String("gsi_audit_actor_sk"), KeyType: types.KeyTypeRange},
			},
			Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll},
		},
		// gsi_asset_role: role-keyed selector index. /preview-url, /thumbnail-url,
		// and /download-url resolve to "highest-priority COMPLETE asset of role X"
		// via this GSI rather than presigning an arbitrary asset id.
		{
			IndexName: aws.String("gsi_asset_role"),
			KeySchema: []types.KeySchemaElement{
				{AttributeName: aws.String("gsi_asset_role_pk"), KeyType: types.KeyTypeHash},
				{AttributeName: aws.String("gsi_asset_role_sk"), KeyType: types.KeyTypeRange},
			},
			Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll},
		},
	}

	_, err = c.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName:              aws.String(name),
		KeySchema:              []types.KeySchemaElement{{AttributeName: aws.String("PK"), KeyType: types.KeyTypeHash}, {AttributeName: aws.String("SK"), KeyType: types.KeyTypeRange}},
		AttributeDefinitions:   attrs,
		GlobalSecondaryIndexes: gsis,
		BillingMode:            types.BillingModePayPerRequest,
	})
	if err != nil {
		// Lost the race with a parallel bootstrap (multiple workers) — fine.
		var inUse *types.ResourceInUseException
		if !errors.As(err, &inUse) {
			return err
		}
	}
	return waitTableActive(ctx, c, name)
}

func waitTableActive(ctx context.Context, c *dynamodb.Client, name string) error {
	deadline := time.Now().Add(60 * time.Second)
	for {
		out, err := c.DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(name)})
		if err != nil {
			return err
		}
		if out.Table != nil && out.Table.TableStatus == types.TableStatusActive {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("ddb: table did not become ACTIVE within 60s")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}
