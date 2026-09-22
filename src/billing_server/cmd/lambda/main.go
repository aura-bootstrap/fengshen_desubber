// Command lambda 是 billing_server 的 Lambda 入口:Function URL(payload v2)事件
// 经 httpadapter 转给 httpserver 的同一个 http.Handler,路由/鉴权/计费逻辑与 cmd/server 完全一致。
//
// 与 cmd/server 的差异:
//   - 无 config.yaml,全部走环境变量:TABLE_REDIMO / CARD_PEPPER / SESSION_KEY(空回落 CARD_PEPPER)/
//     ADMIN_ROOT_PASSWORD(幂等种入 root,空则跳过);DDB_ENDPOINT 仅本地调试用。
//   - 不起 worker 常驻协程(Lambda 无调用间后台执行)。
//   - TOS_AK+LAS_API_KEY 均配置时注册 las 平台(TOS 上传 + LAS 算子),在线建单不再 503;
//     但无 worker 推进云端阶段,任务会停在排队——火山中转要闭环仍需常驻形态
//     (installer/billing_server Docker)。另注意 Function URL 请求体上限 6MB,大视频上传会被拒。
//     卡激活/余额/会话/账户/审计等计费鉴权接口不受影响。
//
// 构建:GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o bootstrap ./cmd/lambda
package main

import (
	"context"
	"log"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/awslabs/aws-lambda-go-api-proxy/httpadapter"

	"fengshen-desubber/billing_server/internal/ddbstore"
	"fengshen-desubber/billing_server/internal/httpserver"
	"fengshen-desubber/billing_server/internal/las"
	"fengshen-desubber/billing_server/internal/provider"
	"fengshen-desubber/billing_server/internal/tosstore"
)

func main() {
	ctx := context.Background()
	st, err := ddbstore.NewFromEnv(ctx)
	if err != nil {
		log.Fatalf("ddbstore: %v", err)
	}
	if st.Pepper == "" {
		log.Fatalf("CARD_PEPPER 未配置")
	}
	if err := st.EnsureRoot(ctx, os.Getenv("ADMIN_ROOT_PASSWORD")); err != nil {
		log.Printf("种入 root 跳过/失败: %v", err)
	}
	sessionKey := os.Getenv("SESSION_KEY")
	if sessionKey == "" {
		sessionKey = st.Pepper
	}
	reg := provider.NewRegistry("")
	if os.Getenv("TOS_AK") != "" && os.Getenv("LAS_API_KEY") != "" {
		tosUp, err := tosstore.New(os.Getenv("TOS_ENDPOINT"), os.Getenv("TOS_REGION"),
			os.Getenv("TOS_AK"), os.Getenv("TOS_SK"), os.Getenv("TOS_BUCKET"))
		if err != nil {
			log.Fatalf("tos: %v", err)
		}
		lasCli := las.New(os.Getenv("LAS_BASE_URL"), os.Getenv("LAS_API_KEY"),
			os.Getenv("LAS_OPERATOR_ID"), os.Getenv("LAS_OPERATOR_VERSION"))
		reg = provider.NewRegistry("las")
		reg.Register(provider.Provider{Name: "las", Uploader: tosUp, Operator: lasCli})
	}
	handler := httpserver.New(st, "/tmp/desub-src", reg, []byte(sessionKey)).Handler()
	// Function URL 事件是 payload v2 结构,必须用 NewV2(New 按 v1 解析会得到空路径,全量 404)
	lambda.Start(httpadapter.NewV2(handler).ProxyWithContext)
}
