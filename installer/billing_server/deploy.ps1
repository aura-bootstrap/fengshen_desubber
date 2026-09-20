<#
.SYNOPSIS
  billing_server 一键部署(Docker):构建镜像并以容器运行峰神·去字幕计费服务端。

.DESCRIPTION
  流程:docker build(src/billing_server/Dockerfile)→ 准备配置与数据目录 → docker run。
    -First      首次:从 config.example.yaml 复制出本目录 config.yaml(须先编辑机密),建 data\ 目录
    (默认)      日常:重新构建镜像并替换运行中的容器(配置/数据不动)
    -BuildOnly  只构建镜像自检,不动容器

  配置文件默认用本目录 config.yaml(-Config 可改),只读挂到容器 /etc/desubber/config.yaml。
  DynamoDB / TOS / LAS 等机密全部走配置文件或环境变量(-EnvFile),不进镜像、不进 git。

.EXAMPLE
  powershell -File installer\billing_server\deploy.ps1 -First      # 首次:建配置模板+数据目录+起容器
  powershell -File installer\billing_server\deploy.ps1             # 之后:重建镜像并替换容器
  powershell -File installer\billing_server\deploy.ps1 -BuildOnly  # 只构建自检
#>
[CmdletBinding()]
param(
  [switch]$First,
  [switch]$BuildOnly,
  [string]$Image     = 'fengshen-desubber-billing',
  [string]$Container = 'fengshen-desubber-billing',
  [int]$Port         = 18080,
  [string]$Config    = '',
  [string]$EnvFile   = ''
)
$ErrorActionPreference = 'Stop'

# --- 路径解析(脚本在 installer\billing_server\,上溯两级到仓根)---
$repoRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$serverDir = Join-Path $repoRoot 'src\billing_server'
if (-not $Config) { $Config = Join-Path $PSScriptRoot 'config.yaml' }
$dataDir = Join-Path $PSScriptRoot 'data'

function Assert-Tool([string]$name, [string]$hint) {
  if (-not (Get-Command $name -ErrorAction SilentlyContinue)) { throw "未找到 $name。$hint" }
}
Assert-Tool 'docker' '需要 Docker Desktop 并在运行中'

# --- 1. 构建镜像 ---
Write-Host "== docker build -t $Image (src\billing_server)" -ForegroundColor Cyan
& docker build -t $Image $serverDir
if ($LASTEXITCODE -ne 0) { throw 'docker build 失败' }

if ($BuildOnly) {
  Write-Host "BuildOnly:镜像 $Image 已就绪,未动容器。" -ForegroundColor Yellow
  return
}

# --- 2. 配置与数据目录 ---
if (-not (Test-Path $Config)) {
  if (-not $First) { throw "配置不存在: $Config(首次请用 -First 从模板复制,或 -Config 指定)" }
  Copy-Item (Join-Path $serverDir 'config.example.yaml') $Config
  Write-Host "!! 已从模板复制 $Config —— 先把 admin_token / card_pepper / tos / las 改掉再上线!" -ForegroundColor Yellow
}
$raw = Get-Content $Config -Raw
if ($raw -match 'change-me') {
  Write-Host "WARN: $Config 仍含 change-me 占位机密,请勿用于生产" -ForegroundColor Yellow
}
New-Item -ItemType Directory -Force (Join-Path $dataDir 'src'), (Join-Path $dataDir 'result') | Out-Null

# --- 3. 替换运行中的容器 ---
& docker inspect $Container 1>$null 2>$null
if ($LASTEXITCODE -eq 0) {
  Write-Host "== 停止并删除旧容器 $Container" -ForegroundColor Cyan
  & docker rm -f $Container | Out-Null
}

$runArgs = @('run', '-d', '--name', $Container, '--restart', 'unless-stopped',
  '-p', "${Port}:18080",
  '-v', "${Config}:/etc/desubber/config.yaml:ro",
  '-v', "${dataDir}:/data")
if ($EnvFile) { $runArgs += @('--env-file', $EnvFile) }
$runArgs += $Image

Write-Host "== docker run $Container (127.0.0.1:$Port)" -ForegroundColor Cyan
& docker @runArgs | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'docker run 失败' }

# --- 4. 起容器后探活(服务端无 /healthz,TCP 能连即视为已在监听)---
$ok = $false
foreach ($i in 1..15) {
  Start-Sleep -Seconds 1
  $c = New-Object System.Net.Sockets.TcpClient
  try {
    $c.Connect('127.0.0.1', $Port)
    $ok = $true; break
  } catch {} finally { $c.Close() }
}
if ($ok) {
  Write-Host "== 部署完成: http://127.0.0.1:$Port (端口已监听)" -ForegroundColor Green
} else {
  Write-Host "WARN: 容器已启动但 $Port 未监听,用 docker logs $Container 排查" -ForegroundColor Yellow
}
