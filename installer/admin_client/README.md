# admin_client 安装包(管理版)

峰神·去字幕管理端(卡密/审计/交易流水后台,连 billing_server)的 Windows Inno 安装包。

## 构建

```powershell
# 1. 构建 Flutter Windows release 并组装发布目录
cd src\admin_client
flutter build windows --release
mkdir ..\..\release\admin_client\windows
xcopy /E /I build\windows\x64\runner\Release\* ..\..\release\admin_client\windows\

# 2. 编译安装包(产物在 installer\admin_client\output\)
cd ..\..
powershell -File installer\admin_client\make.ps1
```

## 说明

- 免管理员安装:装到 `%LOCALAPPDATA%\Programs\FengshenDesubberAdmin`,与用户版(`FengshenDesubber`)可并存。
- 管理端只连 billing_server,无本地运行时产物,整包照拷,无需排除清单。
- 图标与用户版同源:`src\admin_client\windows\runner\resources\app_icon.ico`(蓝色)。
