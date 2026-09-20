# Compile the Inno installer for dev_client from release\dev_client\ (assemble the release folder first).
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path   # installer\dev_client
Set-Location (Split-Path -Parent (Split-Path -Parent $root))   # repo root

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
