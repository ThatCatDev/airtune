#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif

[Setup]
AppName=AirTune
AppVersion={#AppVersion}
AppPublisher=AirTune
DefaultDirName={autopf}\AirTune
DefaultGroupName=AirTune
UninstallDisplayIcon={app}\airtune.exe
OutputBaseFilename=airtune-setup-v{#AppVersion}
OutputDir=..
Compression=lzma2/ultra
SolidCompression=yes
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible

[Files]
Source: "..\bundle\airtune.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\bundle\*.dll"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\bundle\lib\*"; DestDir: "{app}\lib"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "..\bundle\share\*"; DestDir: "{app}\share"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{group}\AirTune"; Filename: "{app}\airtune.exe"
Name: "{group}\Uninstall AirTune"; Filename: "{uninstallexe}"
Name: "{autodesktop}\AirTune"; Filename: "{app}\airtune.exe"; Tasks: desktopicon

[Tasks]
Name: "desktopicon"; Description: "Create a &desktop shortcut"; GroupDescription: "Additional shortcuts:"
