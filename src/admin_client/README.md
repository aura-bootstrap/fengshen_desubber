# admin_client — 峰神·去字幕 管理端(占位)

卡密管理端 Flutter 应用,仿照 fengshen-slicer 的 src/admin_client。

待实现。管理操作目前走 billing_server 的 HTTP API(`src/billing_server`,
`/v1/admin/cards/{generate,recharge,revoke,unrevoke,unbind}`、`/v1/admin/cards[/audit]`、
`/v1/admin/transactions`,Bearer admin token 鉴权,见 `config.example.yaml` 的 `admin_token`)。
