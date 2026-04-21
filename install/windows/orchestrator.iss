#ifndef AppVersion
  #define AppVersion "dev"
#endif
#ifndef AppRevision
  #define AppRevision "unknown"
#endif
#ifndef AppBuildTime
  #define AppBuildTime "unknown"
#endif
#ifndef PortableDir
  #define PortableDir "..\..\dist\windows-amd64\portable"
#endif
#ifndef ReleaseDir
  #define ReleaseDir "..\..\dist\windows-amd64\installer"
#endif

[Setup]
AppId={{F4E6E164-B40D-4EEB-90C6-3EF5B4AA0F71}
AppName=Orchestrator
AppVersion={#AppVersion}
AppVerName=Orchestrator {#AppVersion}
AppPublisher=Orchestrator Project
AppComments=Planner-led orchestration CLI
DefaultDirName={autopf}\Orchestrator
DefaultGroupName=Orchestrator
DisableProgramGroupPage=yes
ArchitecturesAllowed=x64
ArchitecturesInstallIn64BitMode=x64
Compression=lzma
SolidCompression=yes
ChangesEnvironment=yes
WizardStyle=modern
OutputDir={#ReleaseDir}
OutputBaseFilename=orchestrator_{#AppVersion}_windows_installer
UninstallDisplayName=Orchestrator
VersionInfoTextVersion={#AppVersion}
VersionInfoDescription=Orchestrator CLI
VersionInfoProductName=Orchestrator
SetupLogging=yes

[Tasks]
Name: "addtopath"; Description: "Add 'orchestrator' to your PATH"; Flags: unchecked
Name: "desktopicon"; Description: "Create a desktop shortcut"; Flags: unchecked

[Files]
Source: "{#PortableDir}\orchestrator.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#PortableDir}\README.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#PortableDir}\WINDOWS_INSTALL_AND_RELEASE.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#PortableDir}\REAL_APP_WORKFLOW.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#PortableDir}\build-metadata.txt"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\Orchestrator"; Filename: "{app}\orchestrator.exe"
Name: "{autodesktop}\Orchestrator"; Filename: "{app}\orchestrator.exe"; Tasks: desktopicon

[Run]
Filename: "{app}\orchestrator.exe"; Parameters: "version"; Description: "Run orchestrator version"; Flags: nowait postinstall skipifsilent unchecked

[Registry]
Root: HKCU; Subkey: "Environment"; ValueType: expandsz; ValueName: "Path"; ValueData: "{code:AppendPath|{app}}"; Tasks: addtopath; Check: NeedsAddPath(ExpandConstant('{app}'))

[Code]
function NeedsAddPath(PathValue: string): Boolean;
var
  ExistingPath: string;
begin
  if not RegQueryStringValue(HKCU, 'Environment', 'Path', ExistingPath) then
    ExistingPath := '';

  Result := Pos(';' + Uppercase(PathValue) + ';', ';' + Uppercase(ExistingPath) + ';') = 0;
end;

function AppendPath(PathValue: string): string;
var
  ExistingPath: string;
begin
  if not RegQueryStringValue(HKCU, 'Environment', 'Path', ExistingPath) then
    ExistingPath := '';

  if ExistingPath = '' then
    Result := PathValue
  else if Pos(';' + Uppercase(PathValue) + ';', ';' + Uppercase(ExistingPath) + ';') > 0 then
    Result := ExistingPath
  else
    Result := ExistingPath + ';' + PathValue;
end;
