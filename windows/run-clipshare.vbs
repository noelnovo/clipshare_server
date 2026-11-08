Option Explicit

Dim shell, fso, executable, command
Set shell = CreateObject("WScript.Shell")
Set fso = CreateObject("Scripting.FileSystemObject")

executable = fso.BuildPath(fso.GetParentFolderName(WScript.ScriptFullName), "clipshare.exe")
If Not fso.FileExists(executable) Then
  WScript.Quit 2
End If

command = """" & executable & """ daemon"
shell.Run command, 0, False
