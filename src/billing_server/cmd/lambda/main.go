// Command lambda 是 billing_server 的 Lambda 入口:Function URL(payload v2)事件
// 经 httpadapter 转给 httpserver 的同一个 http.Handler,路由/鉴权/计费逻辑与 cmd/server 完全一致。
//
// 与 cmd/server 的差异:
//   - 无 config.yaml,全部走环境变量:TABLE_REDIMO / CARD_PEPPER / SESSION_KEY(空回落 CARD_PEPPER)/
//     ADMIN_ROOT_PASSWORD(幂等种入 root,空则跳过);TOS_AK+LAS_API_KEY 均配置才注册 las 平台;
//     DDB_ENDPOINT 仅本地调试用。
//
// 在线去字幕为 TOS 直传协议(建单发预签名 PUT→客户端直传→submit 探测扣点提交算子→
// GET 轮询内联推进算子状态→download 302 到算子侧成片),全程 JSON 小请求、无 worker,
// Lambda 即可闭环;Function URL 6MB 请求体上限不再受影响(视频不经过 Lambda)。
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
		if err := tosUp.EnsureInputLifecycle(ctx, 1); err != nil {
			log.Printf("tos lifecycle: %v", err) // 不阻断:即时 Delete 仍在,仅失孤儿兜底
		}
		lasCli := las.New(os.Getenv("LAS_BASE_URL"), os.Getenv("LAS_API_KEY"),
			os.Getenv("LAS_OPERATOR_ID"), os.Getenv("LAS_OPERATOR_VERSION"))
		reg = provider.NewRegistry("las")
		reg.Register(provider.Provider{Name: "las", Uploader: tosUp, Operator: lasCli})
	}
	handler := httpserver.New(st, reg, []byte(sessionKey)).Handler()
	// Function URL 事件是 payload v2 结构,必须用 NewV2(New 按 v1 解析会得到空路径,全量 404)
	lambda.Start(httpadapter.NewV2(handler).ProxyWithContext)
}
