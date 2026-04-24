Option Explicit

Dim shell, fso, scriptPath, projectRoot, command
Set shell = CreateObject("WScript.Shell")
Set fso = CreateObject("Scripting.FileSystemObject")

scriptPath = fso.GetParentFolderName(WScript.ScriptFullName)
projectRoot = fso.GetParentFolderName(scriptPath)

command = "powershell.exe -NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File """ _
  & fso.BuildPath(projectRoot, "scripts\start-v2-dogfood.ps1") & """"

' Window style 0 keeps the launcher PowerShell hidden. The Electron window still appears.
shell.Run command, 0, False
