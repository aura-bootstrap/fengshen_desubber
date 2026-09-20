# admin_client — 峰神·去字幕 管理端

卡密管理端 Flutter 应用(Windows 桌面),对接 billing_server 的管理 API。

## 功能

- 登录:计费服务器地址 + admin token(登录时以拉取卡列表校验凭证)
- 卡密:列表(可按批次过滤)、批量发卡(明文仅发卡时展示一次,可一键复制)、
  按卡面充值、吊销/恢复(单卡或整批)、按卡 ID 解绑、卡状态查询、跳转交易流水
- 审计:卡密操作审计日志(时间倒序)
- 交易流水:按卡 ID 查积分流水

## 对接协议

全部走 `src/billing_server` 的 HTTP API,Bearer admin token 鉴权:
`/v1/admin/cards/{generate,recharge,revoke,unrevoke,unbind}`、`/v1/admin/cards[/audit]`、
`/v1/admin/transactions`、`/v1/cards/{card}/status`。

## 构建

```
flutter build windows --release   # 产物在 build/windows/x64/runner/Release/
```
