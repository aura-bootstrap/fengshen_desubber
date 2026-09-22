# 峰神·去字幕 发布流程

## 0. 前置条件

- Docker Desktop 运行中（引擎容器 `desub:cu124` 等镜像已构建）
- Inno Setup 6：便携版在 `W:\QoderCN\tools\InnoSetup6\ISCC.exe`（make.ps1 自动探测，回落旧约定路径与 PATH）
- Go 工具链 1.25.5（GOPROXY 用 goproxy.cn）、Flutter Windows 桌面工具链
- 工作树干净、在 `master`，无其他会话在并发改同一仓库

## 1. 功能开发与验证（每个功能独立提交）

1. 改代码，遵循「单一功能单一提交」。
2. 质量门禁（全绿才提交）：
   - 双端引擎：`go build ./... && go vet ./... && go test ./...`（`src/user_client/backend`、`src/dev_client/backend`，判定看 exit code，不走管道）
   - 前端：`flutter analyze`（三端各自前端目录）
3. 客户端改动必须重打包 + UI 实测（`flutter build windows`，禁止热替换 exe）：跑真实任务验证，窗口截图只信 computer-use WGC。

## 2. 引擎二进制升级（仅当引擎逻辑有改动）

```sh
# 在 src/dev_client/backend 下（cmd/desub 是容器内引擎 CLI）
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" \
  -o ../../../bin/desub-lx-v<N> ./cmd/desub
file bin/desub-lx-v<N>   # 必须是 Linux ELF，PE 会在容器里 exit 1
```

- 同步把默认版本号指到新二进制：`src/user_client/backend/cmd/desub-engine/docker.go` 与 `src/user_client/backend/internal/runner/runner.go` 的 `/src/bin/desub-lx-v<N>`（容器内仓库挂载在 `/src`）。
- 不删旧版本二进制（bin/ 保留 v3…vN 全系列）。

## 3. 版本号三处同步（三端各自三处，共九处）

| 位置 | 用户版 | 开发版 | 管理版 |
|---|---|---|---|
| pubspec `version: X.Y.Z+B` | src/user_client/frontend/pubspec.yaml | src/dev_client/frontend/pubspec.yaml | src/admin_client/pubspec.yaml |
| setup.iss `MyAppVersion` | installer/user_client/setup.iss | installer/dev_client/setup.iss | installer/admin_client/setup.iss |
| `kAppVersion` | src/user_client/frontend/lib/app_shell.dart | src/dev_client/frontend/lib/app_shell.dart | src/admin_client/lib/main.dart |

构建号 `+B` 随发版递增。

## 4. 组装 release/ 发布目录

**用户版** `release/user_client/`：

```sh
cd src/user_client/backend && go build -o bin/desub-engine.exe ./cmd/desub-engine
cd ../frontend && flutter build windows
# 把 build/windows/x64/runner/Release/* 拷入 release/user_client/
# 再补 desub-engine.exe
```

**开发版** `release/dev_client/`：同上（frontend build + desub-engine.exe），另加：

```sh
sh src/dev_client/backend/build.sh          # 产出 bin/desub-windows-amd64.exe（+ linux 版）
cp bin/desub-windows-amd64.exe release/dev_client/desub.exe
# release/dev_client/bin/ 下保留 ffmpeg.exe + ffprobe.exe
```

**管理版** `release/admin_client/`（纯 Flutter，地址编译期注入生产 Lambda）：

```sh
cd src/admin_client && flutter build windows --dart-define=DESUB_BILLING_BASE=<生产Lambda Function URL>
# 注入值可在产物 data/app.so 里 grep 核验
```

注意：

- 重打包时保留 release/ 下已注入的引擎/数据文件（排除同步）。
- setup.iss 打包时已排除运行时产物与本机授权态：`cardkey.json,tasks.db*,workspace\*,logs\*,*.log`（cardkey.json 是 DPAPI 绑定机器的激活态，打进安装包会污染全体用户）。

## 5. 编译安装包（三端）

```powershell
powershell -File installer\user_client\make.ps1
powershell -File installer\dev_client\make.ps1
powershell -File installer\admin_client\make.ps1
# 产物在 installer\<端>\output\峰神·去字幕[-开发版/-管理版]-vX.Y.Z-installer.exe
```

## 6. 安装实测（每端）

```sh
<installer>.exe /VERYSILENT   # 一律静默安装
```

逐端核验：进程名/任务栏中文名正确、版本号显示为新版本、核心链路跑通：

- 用户版：跑一个真实任务 + 云在线余额
- 开发版：引擎已连接 + 建任务
- 管理版：登录进授权码页

## 7. 服务端（仅当 billing_server 有改动）

热更生产 Lambda `fengshen-desubber`（ap-east-1）：`GOOS=linux GOARCH=arm64` 构建 bootstrap → 更新函数 → 冒烟（无凭据 401 / root 登录 200 / 账户列表 200），提交信息里记 CodeSha256。

## 8. 发版提交

单独一个提交，格式沿用历史：

```
发版 vX.Y.Z:版本号三处同步(pubspec X.Y.Z+B / setup.iss MyAppVersion X.Y.Z / kAppVersion X.Y.Z,含三端)。本版内容:<本版各功能一句话汇总>
```

不擅自 push（等明确指示）。
