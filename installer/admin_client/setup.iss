; 峰神·去字幕(管理版)Windows 安装包(Inno Setup 6)—— 管理端(卡密/审计/交易流水后台,连 billing_server)
; 构建: 先组装 release\admin_client\windows\, 再跑本目录 make.ps1
; 产物: installer\admin_client\output\峰神·去字幕-管理版-v1.0.1-installer.exe

#define MyAppName "峰神·去字幕(管理版)"
#define MyAppVersion "1.0.1"
#define MyAppPublisher "峰神引擎"
#define MyAppExeName "峰神引擎-字幕去除器-管理版.exe"

[Setup]
; 独立 AppId:与用户版可并存安装
AppId={{DDA1747F-4905-414C-9A75-BDF4A08E93EF}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
; 免管理员:装到用户目录(与用户版不同目录,可并存)
DefaultDirName={localappdata}\Programs\FengshenDesubberAdmin
DefaultGroupName={#MyAppName}
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
OutputDir=installer\admin_client\output
OutputBaseFilename=峰神·去字幕-管理版-v{#MyAppVersion}-installer
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
SetupIconFile=src\admin_client\windows\runner\resources\app_icon.ico
UninstallDisplayIcon={app}\{#MyAppExeName}
; 源目录:发布包 release\admin_client\windows\(相对本脚本所在 installer\admin_client\ 目录上溯两级到仓根)
SourceDir=..\..

[Languages]
Name: "chinesesimplified"; MessagesFile: "installer\admin_client\ChineseSimplified.isl"

[Tasks]
Name: "desktopicon"; Description: "创建桌面快捷方式"; GroupDescription: "附加任务:"

[Files]
; 管理端只连 billing_server,无本地运行时产物,整包照拷
Source: "release\admin_client\windows\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{group}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"
Name: "{group}\卸载 {#MyAppName}"; Filename: "{uninstallexe}"
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Tasks: desktopicon

[Run]
Filename: "{app}\{#MyAppExeName}"; Description: "立即运行 {#MyAppName}"; Flags: nowait postinstall skipifsilent
