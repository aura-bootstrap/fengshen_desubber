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
- 表初始化:DynamoDB 表由 `src\billing_server\cmd\mktables` 一次性创建(本地 DDB 容器或 AWS),详见源码注释。
