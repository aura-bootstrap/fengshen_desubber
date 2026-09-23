<#
.SYNOPSIS
  billing_server 生产 Lambda 热更:重新编译 bootstrap 并 update-function-code。

.DESCRIPTION
  发布流程的服务端步骤。流程:交叉编译 cmd/lambda(GOOS=linux GOARCH=arm64)→
  打 function.zip(仅 bootstrap,zip 根)→ aws lambda update-function-code →
  等待生效并打印 CodeSha256 → 探活 Function URL(未授权 /v1/account 应回 401)。

  仅用于日常代码热更;首部署或 template.yaml 变更走 aws cloudformation deploy。
  Function URL 不入库,由 get-function-url-config 现查。

    (默认)      编译 + 打包 + 热更 + 探活
    -BuildOnly  只编译打包自检,不动生产

.EXAMPLE
  powershell -File installer\billing_server\update_lambda.ps1            # 热更生产
  powershell -File installer\billing_server\update_lambda.ps1 -BuildOnly # 只编译自检
#>
[CmdletBinding()]
param(
  [switch]$BuildOnly,
  [string]$Function = 'fengshen-desubber',
  [string]$Region   = 'ap-east-1'
)
$ErrorActionPreference = 'Stop'

$repoRoot  = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$serverDir = Join-Path $repoRoot 'src\billing_server'
$dist      = Join-Path $serverDir 'dist'
New-Item -ItemType Directory -Force $dist | Out-Null

function Assert-Tool([string]$name, [string]$hint) {
  if (-not (Get-Command $name -ErrorAction SilentlyContinue)) { throw "未找到 $name。$hint" }
}
Assert-Tool 'go' ''

# --- 1. 交叉编译 Lambda 入口(provided.al2023 arm64,Handler=bootstrap)---
Write-Host '== go build cmd/lambda -> dist\bootstrap (linux/arm64)' -ForegroundColor Cyan
Push-Location $serverDir
try {
  $env:GOOS = 'linux'; $env:GOARCH = 'arm64'; $env:CGO_ENABLED = '0'
  & go build -trimpath -ldflags '-s -w' -o (Join-Path $dist 'bootstrap') .\cmd\lambda
  if ($LASTEXITCODE -ne 0) { throw 'go build 失败' }
} finally {
  Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue
  Pop-Location
}

# --- 2. 打包 function.zip(仅 bootstrap 于 zip 根)---
$zip = Join-Path $dist 'function.zip'
Compress-Archive -Path (Join-Path $dist 'bootstrap') -DestinationPath $zip -Force
$mb = [math]::Round((Get-Item $zip).Length / 1MB, 1)
Write-Host "== function.zip 就绪 ($MB MB)" -ForegroundColor Cyan

if ($BuildOnly) {
  Write-Host 'BuildOnly:编译打包完成,未动生产。' -ForegroundColor Yellow
  return
}

# --- 3. 热更生产 Lambda ---
Assert-Tool 'aws' '需要 AWS CLI 且已配置有 Lambda 写权限的凭据'
Write-Host "== aws lambda update-function-code ($Function @ $Region)" -ForegroundColor Cyan
& aws lambda update-function-code --function-name $Function --region $Region `
    --zip-file "fileb://$zip" --publish | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'update-function-code 失败' }

& aws lambda wait function-updated --function-name $Function --region $Region
if ($LASTEXITCODE -ne 0) { throw '等待函数生效超时/失败' }
$info = & aws lambda get-function --function-name $Function --region $Region `
    --query 'Configuration.[CodeSha256,LastModified,Version]' --output text
Write-Host "== 已生效: CodeSha256/LastModified/Version = $info" -ForegroundColor Green

# --- 4. 探活:未授权请求应回 401(路由与鉴权存活;URL 现查,不入库)---
$url = & aws lambda get-function-url-config --function-name $Function --region $Region `
    --query 'FunctionUrl' --output text
if ($LASTEXITCODE -eq 0 -and $url -match '^https://') {
  try {
    $null = Invoke-WebRequest -Uri ($url.TrimEnd('/') + '/v1/account') -Method Get -TimeoutSec 15
    Write-Host 'WARN: /v1/account 无令牌未回 401,检查鉴权!' -ForegroundColor Yellow
  } catch {
    $code = [int]$_.Exception.Response.StatusCode
    if ($code -eq 401) {
      Write-Host '== 探活正常: /v1/account 无令牌 401' -ForegroundColor Green
    } else {
      Write-Host "WARN: 探活返回 $code(预期 401),请人工复核" -ForegroundColor Yellow
    }
  }
} else {
  Write-Host 'WARN: 未能查到 Function URL,跳过探活' -ForegroundColor Yellow
}
