// mktables 本地开发辅助:在 DDB_ENDPOINT(默认 http://127.0.0.1:8000)建单表 redimo
// (pk(B)+sk(B),无 GSI/LSI,TTL on exp),已存在则跳过,幂等。
// 用法:go run ./cmd/mktables
package main

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

func main() {
	ctx := context.Background()
	ep := os.Getenv("DDB_ENDPOINT")
	if ep == "" {
		ep = "http://127.0.0.1:8000"
	}
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-east-1"),
		config.WithBaseEndpoint(ep),
		config.WithCredentialsProvider(aws.CredentialsProviderFunc(
			func(context.Context) (aws.Credentials, error) {
				return aws.Credentials{AccessKeyID: "x", SecretAccessKey: "x"}, nil
			})),
	)
	if err != nil {
		log.Fatal(err)
	}
	db := dynamodb.NewFromConfig(cfg)

	table := envOr("TABLE_REDIMO", "redimo")
	_, err = db.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName: aws.String(table),
		AttributeDefinitions: []types.AttributeDefinition{
			{AttributeName: aws.String("pk"), AttributeType: types.ScalarAttributeTypeB},
			{AttributeName: aws.String("sk"), AttributeType: types.ScalarAttributeTypeB},
		},
		KeySchema: []types.KeySchemaElement{
			{AttributeName: aws.String("pk"), KeyType: types.KeyTypeHash},
			{AttributeName: aws.String("sk"), KeyType: types.KeyTypeRange},
		},
		BillingMode:           types.BillingModeProvisioned,
		ProvisionedThroughput: &types.ProvisionedThroughput{ReadCapacityUnits: aws.Int64(25), WriteCapacityUnits: aws.Int64(25)},
	})
	if err != nil && !inUse(err) {
		log.Fatalf("建 redimo 表: %v", err)
	}
	ttl := true
	if _, err = db.UpdateTimeToLive(ctx, &dynamodb.UpdateTimeToLiveInput{
		TableName: aws.String(table),
		TimeToLiveSpecification: &types.TimeToLiveSpecification{
			AttributeName: aws.String("exp"), Enabled: &ttl,
		},
	}); err != nil && !strings.Contains(err.Error(), "already enabled") {
		log.Fatalf("开 TTL: %v", err)
	}
	log.Printf("就绪: %s @ %s", table, ep)
}

func inUse(err error) bool {
	var r *types.ResourceInUseException
	return errors.As(err, &r)
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
