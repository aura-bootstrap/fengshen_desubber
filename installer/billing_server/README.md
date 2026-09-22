# billing_server 部署(Docker,一键)

billing_server 是常驻 HTTP 服务(卡密 credit / 审计 / 交易流水 / 在线任务中转),以 Docker 容器运行,不走 Inno 安装包。本目录提供 Windows 一键部署脚本 `deploy.ps1`。

## 架构

- 镜像:`src\billing_server\Dockerfile`(golang 构建 → alpine 运行,非 root)
- 配置:本目录 `config.yaml`(从 `src\billing_server\config.example.yaml` 复制),只读挂进容器 `/etc/desubber/config.yaml`
- 数据:本目录 `data\`(源视频/结果暂存),挂进容器 `/data`
- DynamoDB / TOS / LAS 机密:配置文件或 `-EnvFile` 环境文件注入,不进镜像、不进 git(本目录 `config.yaml`/`data/` 已 gitignore)

## 用法

```powershell
# 首次:构建镜像 + 从模板复制 config.yaml(记得先编辑机密)+ 建 data\ + 起容器
powershell -File installer\billing_server\deploy.ps1 -First

# 之后:重建镜像并替换容器(配置/数据不动)
powershell -File installer\billing_server\deploy.ps1

# 只构建镜像自检,不动容器
powershell -File installer\billing_server\deploy.ps1 -BuildOnly
```

常用参数:`-Image` / `-Container`(默认 fengshen-desubber-billing)、`-Port`(默认 18080)、`-Config`(默认本目录 config.yaml)、`-EnvFile`(DDB_ENDPOINT、AWS_REGION、TABLE_REDIMO 等环境变量文件)。

启动后脚本自动探测 `127.0.0.1:<Port>` 是否已在监听;不通时用 `docker logs fengshen-desubber-billing` 排查。

## 说明

- 免管理员:走当前用户的 Docker Desktop,无需 sudo/服务注册。
- 多实例:用不同 `-Container` / `-Port` / `-Config` 即可并存(如沙盒 18180、生产 18080)。
- 数据表:生产直接复用现有 DynamoDB 表 `redimo`,业务键统一带 `fengshen-desubber:` 前缀;不迁移旧专用表数据。`cmd\mktables` 仅用于本地 DynamoDB。

## 生产 Lambda 热更(发布流程的服务端步骤)

生产计费跑在 AWS Lambda `fengshen-desubber`(ap-east-1),不是 Docker。发版时若 `src/billing_server` 有改动,打完客户端安装包后须热更服务端:

```powershell
# 编译 + 打包 + update-function-code + 探活(未授权 /v1/balance 应回 401)
powershell -File installer\billing_server\update_lambda.ps1

# 只编译打包自检,不动生产
powershell -File installer\billing_server\update_lambda.ps1 -BuildOnly
```

脚本流程:交叉编译 `cmd/lambda`(GOOS=linux GOARCH=arm64)→ 打 `dist\function.zip`(仅 bootstrap)→ `aws lambda update-function-code --publish` → 等待生效并打印 CodeSha256 → 探活 Function URL(现查,不入库)。

注意:

- 仅用于日常代码热更;首部署或 `deploy/template.yaml` 变更走 `aws cloudformation deploy`。生产表 `redimo` 为外部既有资源,模板只授权访问、不创建或管理表生命周期。
- 需要 AWS CLI 且有 Lambda 写权限的凭据。
- 热更的是计费/鉴权/账户/审计与在线去字幕中转;在线链路已改 TOS 直传协议(客户端直传对象存储,服务端只做预签名/扣点/轮询算子),Lambda 无 worker 也能闭环。
