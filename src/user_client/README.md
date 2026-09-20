# user_client — 峰神·去字幕 桌面客户端

去字幕管线的本地任务管理台：选视频、建任务、看阶段与进度、取产物。
风格与架构仿照 fengshen-slicer 的 user_client。

## 架构

```
frontend/  Flutter (Windows 桌面)          backend/  Go 引擎进程 (stdlib + modernc.org/sqlite)
  ├─ 启动时拉起 desub-engine.exe            ├─ cmd/desub-engine   HTTP + SSE 服务
  │    -serve -port 0 → 首行 PORT=<n> 握手  │    └─ internal/runner  docker run 执行器 + 进度解析
  ├─ REST: /api/tasks CRUD + run/stop       │    └─ internal/store   SQLite 任务库
  └─ SSE: /api/events 推 stage/progress/log/done
```

- 单 GPU 槽位：同时只跑一个任务，其余入队（queued）自动接续。
- 每个任务独立工作目录 `backend/bin/workspace/tasks/<id>/`：输入视频硬链接（失败回退复制）为
  `input.mp4` 挂载到容器 `/task`，产物与 `report.json` 落同目录。
- 容器命令与 `scripts/run_container.sh` 等价：镜像 `desub:cu124`，挂载 lab→/work、
  repo→/src、paddlex 缓存、workspace→/task，默认二进制 `/src/bin/desub-lx-v13`。
- 进度解析：识别 desub stdout 的阶段行（probe/cuts/detect/ocr/engine/verify）、
  `repair n/m` 回车滴答（按字节切 token）与末尾 JSON 报告。
- 停止 = context cancel → `docker kill desub-task-<id>`。

## 启动

```bash
# 1. 构建后端(产物到 backend/bin/desub-engine.exe,与工作区同目录)
cd backend && go build -o bin/desub-engine.exe ./cmd/desub-engine

# 2. 跑前端(自动查找并拉起 ../backend/bin/desub-engine.exe;
#    或把 desub-engine.exe 放到 desub_client.exe 同目录)
cd ../frontend && flutter run -d windows    # 或 flutter build windows 后运行 exe
```

前置条件：Docker Desktop 运行中、`desub:cu124` 镜像已构建、`bin/desub-lx-v13` 已编译
（见仓库根 README / scripts/run_container.sh）。模型下载走 `host.docker.internal:7897` 代理。

## 目录约定

| 路径 | 内容 |
|---|---|
| `backend/bin/desub-engine.exe` | 引擎二进制（gitignore） |
| `backend/bin/workspace/desub.db` | SQLite 任务库（journal_mode=DELETE，兼容 Docker Desktop 挂载） |
| `backend/bin/workspace/tasks/<id>/` | 每任务工作区：input.mp4、产物、report.json |
| `frontend/build/` | Flutter 构建产物（gitignore） |

## 测试

```bash
cd backend && go test ./...
cd frontend && flutter test
```
