; 峰神·去字幕(开发版)Windows 安装包(Inno Setup 6)—— 开发版 GUI(desub_dev_ui + desub-engine + desub CLI,
; 全参数面板可调,本地直连不连服务器;配置 JSON 可拷贝到管理版发布)
; 构建: 先组装 release\dev_client\, 再跑本目录 make.ps1
; 产物: installer\dev_client\output\峰神·去字幕-开发版-v1.0.0-installer.exe

#define MyAppName "峰神·去字幕(开发版)"
#define MyAppVersion "1.0.0"
#define MyAppPublisher "峰神引擎"
#define MyAppExeName "desub_dev_ui.exe"

[Setup]
; 独立 AppId:与用户版/管理版可并存安装
AppId={{9DD480FA-DC4A-427F-9DF6-63978971E27B}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
; 免管理员:装到用户目录
DefaultDirName={localappdata}\Programs\FengshenDesubberDev
DefaultGroupName={#MyAppName}
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
OutputDir=installer\dev_client\output
OutputBaseFilename=峰神·去字幕-开发版-v{#MyAppVersion}-installer
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
; 开发版绿底山峰图标(与任务栏 LOGO 同源)
SetupIconFile=src\dev_client\backend\assets\app_icon.ico
UninstallDisplayIcon={app}\{#MyAppExeName}
; 源目录:发布包 release\dev_client\(相对本脚本所在 installer\dev_client\ 目录上溯两级到仓根)
SourceDir=..\..

[Languages]
Name: "chinesesimplified"; MessagesFile: "installer\dev_client\ChineseSimplified.isl"

[Tasks]
Name: "desktopicon"; Description: "创建桌面快捷方式"; GroupDescription: "附加任务:"

[Files]
; 排除运行时产物(单实例锁/本地配置/任务库/工作区),只打程序与资源
Source: "release\dev_client\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs; \
  Excludes: "app.lock,config.json,tasks.db*,workspace\*,logs\*,*.log"

[Icons]
Name: "{group}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"
Name: "{group}\卸载 {#MyAppName}"; Filename: "{uninstallexe}"
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Tasks: desktopicon

[Run]
Filename: "{app}\{#MyAppExeName}"; Description: "立即运行 {#MyAppName}"; Flags: nowait postinstall skipifsilent
