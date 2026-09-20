package billing

// ServerURL 计费服务端地址,同 slicer 授权服务一样写死进客户端二进制,
// UI 不再让用户填写。发布构建用
//
//	go build -ldflags "-X fengshen-desub/internal/billing.ServerURL=https://host" ...
//
// 覆盖为线上地址;置空时激活接口直接报错,提示联系发行方。
var ServerURL = "http://127.0.0.1:18080"
