package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// FreshTable 建一次性 redimo 单表（pk/sk B 键，结构同 cmd/mktables），
// 通过 t.Setenv 注入 TABLE_REDIMO/CARD_PEPPER，测试结束删表。
// 共享表会跨测试/跨轮残留队列任务与机器余额，干扰断言，故每测试独立表。
// 返回表名；DDB_ENDPOINT 未设置时 Skip。
func FreshTable(t *testing.T) string {
	t.Helper()
	ep := os.Getenv("DDB_ENDPOINT")
	if ep == "" {
		t.Skip("需要 DDB_ENDPOINT 指向 DynamoDB Local")
	}
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-east-1"),
		config.WithBaseEndpoint(ep),
		config.WithCredentialsProvider(aws.CredentialsProviderFunc(
			func(context.Context) (aws.Credentials, error) {
				return aws.Credentials{AccessKeyID: "x", SecretAccessKey: "x"}, nil
			})),
	)
	if err != nil {
		t.Fatalf("aws config: %v", err)
	}
	db := dynamodb.NewFromConfig(cfg)

	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	table := "fengshen-desubber-test-" + hex.EncodeToString(b)
	if _, err := db.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName: aws.String(table),
		AttributeDefinitions: []types.AttributeDefinition{
			{AttributeName: aws.String("pk"), AttributeType: types.ScalarAttributeTypeB},
			{AttributeName: aws.String("sk"), AttributeType: types.ScalarAttributeTypeB},
		},
		KeySchema: []types.KeySchemaElement{
			{AttributeName: aws.String("pk"), KeyType: types.KeyTypeHash},
			{AttributeName: aws.String("sk"), KeyType: types.KeyTypeRange},
		},
		BillingMode: types.BillingModePayPerRequest,
	}); err != nil {
		t.Fatalf("create table: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.DeleteTable(context.Background(), &dynamodb.DeleteTableInput{
			TableName: aws.String(table),
		}); err != nil {
			t.Logf("delete table %s: %v", table, err)
		}
	})
	t.Setenv("TABLE_REDIMO", table)
	t.Setenv("CARD_PEPPER", "test-pepper")
	return table
}
