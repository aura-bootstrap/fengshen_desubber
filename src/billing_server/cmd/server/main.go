package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"fengshen-desubber/billing_server/internal/config"
	"fengshen-desubber/billing_server/internal/httpserver"
	"fengshen-desubber/billing_server/internal/las"
	"fengshen-desubber/billing_server/internal/ddbstore"
	"fengshen-desubber/billing_server/internal/provider"
	"fengshen-desubber/billing_server/internal/tosstore"
	"fengshen-desubber/billing_server/internal/worker"
)

func main() {
	cfgPath := "config.yaml"
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	for _, d := range []string{cfg.SrcDir, cfg.ResultDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			log.Fatalf("mkdir %s: %v", d, err)
		}
	}

	ctx0 := context.Background()
	st, err := ddbstore.NewFromEnv(ctx0)
	if err != nil {
		log.Fatalf("ddbstore: %v", err)
	}
	if st.Pepper == "" {
		st.Pepper = cfg.CardPepper
	}
	if st.Pepper == "" {
		log.Fatalf("CARD_PEPPER 未配置（env 或 config.yaml card_pepper）")
	}
	if err := st.EnsureRoot(ctx0, cfg.RootPassword); err != nil {
		log.Fatalf("ensure root: %v", err)
	}
	sessionKey := cfg.SessionKey
	if sessionKey == "" {
		sessionKey = st.Pepper // 未单独配置会话密钥时回落卡胡椒(同属服务端保密串)
	}

	tosUp, err := tosstore.New(cfg.TOS.Endpoint, cfg.TOS.Region, cfg.TOS.AK, cfg.TOS.SK, cfg.TOS.Bucket)
	if err != nil {
		log.Fatalf("tos: %v", err)
	}
	lasCli := las.New(cfg.LAS.BaseURL, cfg.LAS.APIKey, cfg.LAS.OperatorID, cfg.LAS.OperatorVersion)

	// 平台注册表：当前仅火山 LAS 一家（TOS 上传 + LAS 算子），注册为 "las" 并设为默认。
	reg := provider.NewRegistry("las")
	reg.Register(provider.Provider{Name: "las", Uploader: tosUp, Operator: lasCli})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	w := worker.New(st, reg, cfg.ResultDir)
	go w.Run(ctx)

	srv := &http.Server{Addr: cfg.Listen, Handler: httpserver.New(st, cfg.SrcDir, reg, []byte(sessionKey)).Handler()}
	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()
	log.Printf("listening on %s", cfg.Listen)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("http: %v", err)
	}
}
