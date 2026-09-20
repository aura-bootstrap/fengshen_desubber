; 峰神·去字幕(开发版)Windows 安装包(Inno Setup 6)—— 开发版 CLI(desub,全参数命令行,不连服务器)
; 构建: 先跑 src\dev_client\backend\build.sh 并组装 release\dev_client\, 再跑本目录 make.ps1
; 产物: installer\dev_client\output\峰神·去字幕-开发版-v1.0.0-installer.exe

#define MyAppName "峰神·去字幕(开发版)"
#define MyAppVersion "1.0.0"
#define MyAppPublisher "峰神引擎"
#define MyAppExeName "desub.exe"

[Setup]
; 独立 AppId:与用户版/管理版可并存安装
AppId={{9DD480FA-DC4A-427F-9DF6-63978971E27B}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
; 免管理员:装到用户目录
DefaultDirName={localappdata}\Programs\FengshenDesubberCLI
DefaultGroupName={#MyAppName}
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
OutputDir=installer\dev_client\output
OutputBaseFilename=峰神·去字幕-开发版-v{#MyAppVersion}-installer
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
; CLI 无窗口图标资源,安装器图标复用管理端 LOGO(同一品牌)
SetupIconFile=src\admin_client\windows\runner\resources\app_icon.ico
UninstallDisplayIcon={app}\{#MyAppExeName}
; 源目录:发布包 release\dev_client\(相对本脚本所在 installer\dev_client\ 目录上溯两级到仓根)
SourceDir=..\..

[Languages]
Name: "chinesesimplified"; MessagesFile: "installer\dev_client\ChineseSimplified.isl"

[Files]
Source: "release\dev_client\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{group}\卸载 {#MyAppName}"; Filename: "{uninstallexe}"
