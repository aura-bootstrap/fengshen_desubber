; 峰神·去字幕 Windows 安装包(Inno Setup 6)—— 用户版(credit 卡密,可选在线云端引擎)
; 构建: 先组装 release\user_client\, 再跑本目录 make.ps1
; 产物: installer\user_client\output\峰神·去字幕-v1.0.2-installer.exe

#define MyAppName "峰神·去字幕"
#define MyAppVersion "1.0.2"
#define MyAppPublisher "峰神引擎"
#define MyAppExeName "峰神引擎-字幕去除器-用户版.exe"

[Setup]
AppId={{C5D06CAE-CD28-4D3E-9156-93D1943B896C}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
; 免管理员:装到用户目录
DefaultDirName={localappdata}\Programs\FengshenDesubber
DefaultGroupName={#MyAppName}
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
OutputDir=installer\user_client\output
OutputBaseFilename=峰神·去字幕-v{#MyAppVersion}-installer
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
SetupIconFile=src\user_client\frontend\windows\runner\resources\app_icon.ico
UninstallDisplayIcon={app}\{#MyAppExeName}
; 源目录:发布包 release\user_client\(相对本脚本所在 installer\user_client\ 目录上溯两级到仓根)
SourceDir=..\..

[Languages]
Name: "chinesesimplified"; MessagesFile: "installer\user_client\ChineseSimplified.isl"

[Tasks]
Name: "desktopicon"; Description: "创建桌面快捷方式"; GroupDescription: "附加任务:"

[Files]
; 排除运行时产物(任务库/工作区/日志)与本地授权文件
; (cardkey.json 是本机激活态,DPAPI 绑定机器;打进安装包会污染全体用户)
Source: "release\user_client\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs; \
  Excludes: "cardkey.json,tasks.db*,workspace\*,logs\*,*.log"

[Icons]
Name: "{group}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"
Name: "{group}\卸载 {#MyAppName}"; Filename: "{uninstallexe}"
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Tasks: desktopicon

[Run]
Filename: "{app}\{#MyAppExeName}"; Description: "立即运行 {#MyAppName}"; Flags: nowait postinstall skipifsilent
