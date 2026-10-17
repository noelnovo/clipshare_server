!define APP_NAME "ClipShare"
!define COMPANY_NAME "ClipShare"
!define APP_VERSION "__VERSION__"

Name "${APP_NAME}"
OutFile "..\dist\clipshare-${APP_VERSION}-windows-installer.exe"
InstallDir "$LOCALAPPDATA\ClipShare"
RequestExecutionLevel admin
ShowInstDetails show
ShowUninstDetails show

!include "MUI2.nsh"

VIProductVersion "${APP_VERSION}.0"
VIAddVersionKey /LANG=1033 "ProductName" "${APP_NAME}"
VIAddVersionKey /LANG=1033 "CompanyName" "${COMPANY_NAME}"
VIAddVersionKey /LANG=1033 "FileDescription" "ClipShare clipboard sharing daemon"
VIAddVersionKey /LANG=1033 "FileVersion" "${APP_VERSION}"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

Section "Install"
  SetOutPath "$INSTDIR"
  StrCpy $R2 "0"

  ; Install the Windows release binary next to the launcher.
  File "..\dist\clipshare-${APP_VERSION}-windows-amd64.exe"
  Rename "$INSTDIR\clipshare-${APP_VERSION}-windows-amd64.exe" "$INSTDIR\clipshare.exe"

  ; Keep the user's config when the installer is run again for an upgrade.
  CreateDirectory "$APPDATA\clipshare"
  IfFileExists "$APPDATA\clipshare\config.toml" config_exists
    SetOutPath "$APPDATA\clipshare"
    File "..\config.toml"

    ; Fresh Windows installs start with mTLS enabled for Wi-Fi Direct. Keep
    ; upgrades' existing config untouched so users control their TLS rollout.
    ReadEnvStr $R0 "COMPUTERNAME"
    StrCmp $R0 "" 0 +2
      StrCpy $R0 "desktop"

    IfFileExists "$APPDATA\clipshare\certs\ca.pem" tls_ca_ready
      ExecWait '"$INSTDIR\clipshare.exe" cert init' $R1
      StrCmp $R1 0 tls_ca_ready tls_setup_failed

    tls_ca_ready:
      ExecWait '"$INSTDIR\clipshare.exe" cert issue --name "$R0" --type server' $R1
      StrCmp $R1 0 tls_server_ready tls_setup_failed

    tls_server_ready:
      ExecWait '"$INSTDIR\clipshare.exe" cert issue --name android-client --type client' $R1
      StrCmp $R1 0 tls_client_ready tls_setup_failed

    tls_client_ready:
      ExecWait '"$INSTDIR\clipshare.exe" cert qr --name android-client --type client --out "$APPDATA\clipshare\certs\android-client.png"' $R1
      StrCmp $R1 0 tls_qr_ready tls_setup_failed

    tls_qr_ready:
      ExecWait '"$INSTDIR\clipshare.exe" cert export --name android-client --type client --out "$APPDATA\clipshare\certs\android-client.p12"' $R1
      StrCmp $R1 0 tls_p12_ready tls_setup_failed

    tls_p12_ready:
      ClearErrors
      FileOpen $R4 "$APPDATA\clipshare\config.toml" "a"
      IfErrors tls_setup_failed
      FileWrite $R4 "$\r$\n[tls]$\r$\nenabled = true$\r$\nverify_hostname = false$\r$\n"
      FileClose $R4
      IfErrors tls_setup_failed
      StrCpy $R2 "1"
      Goto config_exists

    tls_setup_failed:
      Delete "$APPDATA\clipshare\config.toml"
      MessageBox MB_ICONSTOP "ClipShare could not create the first-install mTLS certificates. No daemon was started. Re-run the installer or use the manual certificate setup in the Windows guide."
      Abort
  config_exists:

  ; The launcher derives the executable path from its own location, so the
  ; installer can use any per-user directory safely.
  SetOutPath "$INSTDIR"
  File "..\windows\run-clipshare.vbs"

  ; LAN access requires administrator rights. Limit these rules to Private
  ; networks; the daemon should not be exposed on public networks.
  ExecWait '"$SYSDIR\netsh.exe" advfirewall firewall delete rule name="ClipShare WebSocket TCP 40403"'
  ExecWait '"$SYSDIR\netsh.exe" advfirewall firewall add rule name="ClipShare WebSocket TCP 40403" dir=in action=allow protocol=TCP localport=40403 profile=private'
  ; Wi-Fi Direct networks can be classified as Public. Restrict the additional
  ; rule to directly connected subnets so P2P peers can connect without
  ; exposing the listener to arbitrary remote addresses.
  ExecWait '"$SYSDIR\netsh.exe" advfirewall firewall delete rule name="ClipShare WiFi Direct TCP 40403"'
  ExecWait '"$SYSDIR\netsh.exe" advfirewall firewall add rule name="ClipShare WiFi Direct TCP 40403" dir=in action=allow protocol=TCP localport=40403 profile=any remoteip=localsubnet'
  ExecWait '"$SYSDIR\netsh.exe" advfirewall firewall delete rule name="ClipShare WiFi Direct P2P TCP 40403"'
  ExecWait '"$SYSDIR\netsh.exe" advfirewall firewall add rule name="ClipShare WiFi Direct P2P TCP 40403" dir=in action=allow protocol=TCP localport=40403 profile=any remoteip=192.168.137.0/24'
  ExecWait '"$SYSDIR\netsh.exe" advfirewall firewall delete rule name="ClipShare UDP Beacon 40404"'
  ExecWait '"$SYSDIR\netsh.exe" advfirewall firewall add rule name="ClipShare UDP Beacon 40404" dir=in action=allow protocol=UDP localport=40404 profile=private'

  ; Omitting /RU makes schtasks use the account running this installer without
  ; requiring a password. ONLOGON runs in that user's desktop session, and
  ; /RL LIMITED prevents the elevated installer token becoming the daemon's
  ; task privilege.
  ExecWait '"$SYSDIR\schtasks.exe" /Create /TN "ClipShare" /TR "$SYSDIR\wscript.exe $\"$INSTDIR\run-clipshare.vbs$\"" /SC ONLOGON /RL LIMITED /F' $0
  StrCmp $0 0 task_created
  Goto task_failed

  task_created:
  ; Start it now so the user does not need to log out and back in.
  ExecWait '"$SYSDIR\schtasks.exe" /Run /TN "ClipShare"'
  Goto install_complete

  task_failed:
    MessageBox MB_ICONEXCLAMATION "ClipShare was installed, but the automatic login task could not be created. See the Windows installation guide for the manual command."
  install_complete:
  StrCmp $R2 "1" 0 install_done
  MessageBox MB_ICONINFORMATION "Wi-Fi Direct mTLS is enabled. On Android, enable TLS and import the client credential from $APPDATA\clipshare\certs\android-client.png (QR) or android-client.p12."
  install_done:
SectionEnd

Section "Uninstall"
  ExecWait '"$SYSDIR\schtasks.exe" /End /TN "ClipShare"'
  ExecWait '"$SYSDIR\schtasks.exe" /Delete /TN "ClipShare" /F'
  ExecWait '"$SYSDIR\netsh.exe" advfirewall firewall delete rule name="ClipShare WebSocket TCP 40403"'
  ExecWait '"$SYSDIR\netsh.exe" advfirewall firewall delete rule name="ClipShare WiFi Direct TCP 40403"'
  ExecWait '"$SYSDIR\netsh.exe" advfirewall firewall delete rule name="ClipShare WiFi Direct P2P TCP 40403"'
  ExecWait '"$SYSDIR\netsh.exe" advfirewall firewall delete rule name="ClipShare UDP Beacon 40404"'
  Delete "$INSTDIR\clipshare.exe"
  Delete "$INSTDIR\run-clipshare.vbs"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"
  ; Configuration and certificates are user data and are intentionally kept.
SectionEnd

Function .onInstSuccess
  WriteUninstaller "$INSTDIR\uninstall.exe"
FunctionEnd
