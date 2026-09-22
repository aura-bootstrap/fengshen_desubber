// Package ddbstore 单表 redimo 存储：admin 用户/卡账户/机器账户/任务/流水/限频/审计。
// 取代原 SQLite 版 internal/store。点数记在机器账户：卡=一次性充值券，激活即核销转账，
// 余额在 mach JSON 内，单键 CAS 为提交点。
//
// 键族（命名空间 fengshen-desubber:，各键族互不相交）：
//
//	user:<token>           旧 token 管理员 JSON（已废，由 acct: 取代）
//	acct:<username>        管理员账户 JSON（String+meta；root 全功能，admin 仅业务）
//	card:<hash>            卡账户 JSON（String+meta；hash=HMAC-SHA256(pepper,归一化卡面)）
//	cardid:<id>            卡 id → hash 二级键（String+meta）
//	cardidx:<batch>        批次索引（Hash，field=hash12，value=status）
//	mach:<machine_hash>    机器账户 JSON（String+meta；余额唯一权威）
//	seq:<name>             计数器（INCR）
//	task:<uuid>            任务 JSON（String+meta）
//	queue:current          待处理任务 id 列表 JSON（整值 CAS）
//	proc:current           处理中任务 id 列表 JSON（整值 CAS）
//	tx:mach:<machine_hash> 资金流水（Hash，field=ts#seq#rand，sk 字节序=时间序）
//	tx:<card_id>           旧模型卡流水（迁移遗留，只读）
//	rl:<scope>:<id>:<win>  限频窗口计数（INCR，TTL 2×window）
//	rl:fail:<scope>:<id>   连败锁 JSON（整值 CAS）
//	audit:<scope>          审计链（Hash 只增）
//
// 资金一致性：单键 CAS 为提交点（余额在 mach JSON 内），流水/审计为追加留痕，
// 追加失败仅记日志不回滚（与 slicer D1=A 一致）。核销转账=先入账后 CAS 卡，
// CAS 失败者冲正，并发下净入账恰好一份卡面点数。
package ddbstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"

	redimo "github.com/aura-studio/redimo/v2"
)

const keyNS = "fengshen-desubber:"

const (
	keyUser     = keyNS + "user:"    // user:<token>(旧 token 管理员,已废,残留行不影响)
	keyAcct     = keyNS + "acct:"    // acct:<username>(管理员账户,root/admin 双角色)
	keyCard     = keyNS + "card:"    // card:<hash>
	keyCardID   = keyNS + "cardid:"  // cardid:<id>
	keyCardIdx  = keyNS + "cardidx:" // cardidx:<batch>
	keyMach     = keyNS + "mach:"    // mach:<machine_hash>
	keySeq      = keyNS + "seq:"     // seq:<name>
	keyTask     = keyNS + "task:"    // task:<uuid>
	keyTx       = keyNS + "tx:"      // tx:<card_id>
	keyRate     = keyNS + "rl:"      // rl:<scope>:<id>:<win>
	keyRateFail = keyNS + "rl:fail:" // rl:fail:<scope>:<id>
	keyAudit    = keyNS + "audit:"   // audit:<scope>
)

var (
	ErrNotFound            = errors.New("not found")
	ErrInsufficientBalance = errors.New("insufficient balance")
	ErrCardNotFound        = errors.New("card not found")
	ErrCardRevoked         = errors.New("card revoked")
	ErrCardRedeemed        = errors.New("card redeemed")
	ErrDuplicate           = errors.New("duplicate")
	ErrTaskState           = errors.New("task state conflict")
)

// Store 持有强一致 redimo 客户端与可注入时钟。Pepper 为卡哈希 HMAC 密钥
// （须沿用 shipinhao 的 LICENSE_CARD_PEPPER，否则迁移来的卡无法鉴权）。
type Store struct {
	cli    redimo.Client
	ddb    *dynamodb.Client
	table  string
	Pepper string
	Now    func() time.Time
}

// New 以强一致读装配，指向 tableName。
func New(ddb *dynamodb.Client, tableName string) *Store {
	cli := redimo.NewClient(ddb).Table(tableName).StronglyConsistent()
	return &Store{cli: cli, ddb: ddb, table: tableName, Now: time.Now}
}

// NewFromEnv 按环境变量构建：DDB_ENDPOINT 非空走本地（固定假凭据），
// TABLE_REDIMO 缺省 fengshen-desubber，AWS_REGION 缺省 us-east-1。
func NewFromEnv(ctx context.Context) (*Store, error) {
	var opts []func(*config.LoadOptions) error
	if ep := os.Getenv("DDB_ENDPOINT"); ep != "" {
		opts = append(opts,
			config.WithBaseEndpoint(ep),
			config.WithRegion(envOr("AWS_REGION", "us-east-1")),
			config.WithCredentialsProvider(aws.CredentialsProviderFunc(
				func(context.Context) (aws.Credentials, error) {
					return aws.Credentials{AccessKeyID: "x", SecretAccessKey: "x"}, nil
				})),
		)
	}
	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, err
	}
	st := New(dynamodb.NewFromConfig(cfg), envOr("TABLE_REDIMO", "fengshen-desubber"))
	st.Pepper = os.Getenv("CARD_PEPPER")
	return st, nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// mustMarshal 确定性 JSON（Go 结构体字段序固定，输出字节稳定，SETCAS 基线前提）。
func mustMarshal(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic("ddbstore: marshal: " + err.Error())
	}
	return string(b)
}

func itoa(n int64) string { return fmt.Sprintf("%d", n) }
