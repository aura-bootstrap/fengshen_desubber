# dev_client 安装包(开发版 CLI)

峰神·去字幕开发版命令行(`desub`,全参数可调,不连服务器)的 Windows Inno 安装包。

## 构建

```sh
# 1. 编译 Windows CLI(同时产出 linux 二进制,供容器内使用)
sh src/dev_client/backend/build.sh

# 2. 组装发布目录(把 Windows 二进制改名 desub.exe 放入)
mkdir release/dev_client
cp bin/desub-windows-amd64.exe release/dev_client/desub.exe
```

```powershell
# 3. 编译安装包(产物在 installer\dev_client\output\)
powershell -File installer\dev_client\make.ps1
```

## 说明

- 免管理员安装:装到 `%LOCALAPPDATA%\Programs\FengshenDesubberCLI`,与用户版/管理版可并存。
- 纯 CLI:安装包不改 PATH、不建桌面图标;使用时在终端里以完整路径调用,或自行把安装目录加进 PATH。
- CLI 修复任务依赖 Docker 引擎容器,安装本包前需先装 Docker Desktop。
