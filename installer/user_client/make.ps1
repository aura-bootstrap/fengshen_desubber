# Compile the Inno installer for user_client from release\user_client\ (assemble the release folder first).
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path   # installer\user_client
Set-Location (Split-Path -Parent (Split-Path -Parent $root))   # repo root

# 打包前清场:release\ 是组装暂存区,若在原地跑过程序会留下运行时产物
# (授权文件/任务库/工作区/日志)。setup.iss 的 Excludes 是第一道闸,这里再清一遍双保险。
$rel = 'release\user_client'
$dbNames = @(Get-ChildItem $rel -Filter '*.db*' -ErrorAction SilentlyContinue | ForEach-Object { $_.Name })
$residue = @('cardkey.json') + $dbNames
foreach ($n in $residue) {
  if ([string]::IsNullOrEmpty($n)) { continue }
  $p = Join-Path $rel $n
  if (Test-Path $p) { Remove-Item $p -Force; Write-Host "clean $p" -ForegroundColor DarkYellow }
}
foreach ($d in 'logs', 'workspace') {
  $p = Join-Path $rel $d
  if (Test-Path $p) { Remove-Item $p -Recurse -Force; Write-Host "clean $p\" -ForegroundColor DarkYellow }
}

# ISCC:优先便携解包版(W:\QoderCN\tools\InnoSetup6),其次旧约定路径,最后 PATH
$iscc = 'W:\QoderCN\tools\InnoSetup6\ISCC.exe'
if (-not (Test-Path $iscc)) { $iscc = 'W:\localhost\tools\InnoSetup6\ISCC.exe' }
if (-not (Test-Path $iscc)) {
    $iscc = (Get-Command iscc -ErrorAction SilentlyContinue).Source
}
if (-not $iscc) { throw 'ISCC.exe not found, install Inno Setup 6 first' }

& $iscc "$root\setup.iss"
if ($LASTEXITCODE -ne 0) { throw 'ISCC compile failed' }
Write-Host "Installer ready: $root\output\" -ForegroundColor Green
