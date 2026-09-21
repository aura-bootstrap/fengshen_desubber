# admin_client — 峰神·去字幕 管理端

卡密管理端 Flutter 应用(Windows 桌面),对接 billing_server 的管理 API。

## 功能

- 登录:管理员用户名 + 密码(服务器地址编译期注入,见「构建」);root/admin 双角色,
  root 全功能,admin 仅业务页
- 授权码:列表(多条件筛选 + 分页)、批量发卡(明文仅发卡时展示一次,可一键复制)、
  按卡面充值、吊销/恢复、按卡 ID 解绑、卡状态查询、跳转交易流水
- 审计:授权码操作审计日志(时间倒序)
- 交易流水:按卡 ID 查积分流水
- 账户:修改密码;账号管理(仅 root:新建 admin、禁用/启用、重置密码、删除)

## 对接协议

全部走 `src/billing_server` 的 HTTP API,Bearer 会话 token 鉴权(`/v1/admin/login`
换取,12h TTL):`/v1/admin/cards/{generate,recharge,revoke,unrevoke,unbind}`、
`/v1/admin/cards[/audit]`、`/v1/admin/transactions`、`/v1/admin/accounts*`、
`/v1/admin/{password,logout}`、`/v1/cards/{card}/status`。

## 构建

服务器地址在构建期注入(不落 git,对齐 slicer 管理版固定地址思路):

```
flutter build windows --release \
  --dart-define=DESUB_BILLING_BASE=http://host:18080   # 产物在 build/windows/x64/runner/Release/
```

不注入时地址为空,登录直接报「服务端地址未配置」。
