; NeoCloud per-user installer: %LOCALAPPDATA%\Programs\NeoCloud, no admin.
; Built by build.sh, which passes TAG, NUMVERSION, EXE, ICON and OUTFILE.
Unicode true
!include MUI2.nsh

!define APP "NeoCloud"
!define UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\NeoCloud"
!define RUN_KEY "Software\Microsoft\Windows\CurrentVersion\Run"
; Same value name as internal/autostart (autostart.Label).
!define RUN_VALUE "com.neolabs.neocloud-lite"

Name "${APP}"
OutFile "${OUTFILE}"
InstallDir "$LOCALAPPDATA\Programs\NeoCloud"
RequestExecutionLevel user
SetCompressor /SOLID lzma
VIProductVersion "${NUMVERSION}.0"
VIAddVersionKey "ProductName" "${APP}"
VIAddVersionKey "ProductVersion" "${TAG}"
VIAddVersionKey "FileVersion" "${NUMVERSION}.0"
VIAddVersionKey "FileDescription" "NeoCloud Setup"
VIAddVersionKey "LegalCopyright" "NeoLabs Systems"

!define MUI_ICON "${ICON}"
!define MUI_UNICON "${ICON}"
!define MUI_FINISHPAGE_RUN "$INSTDIR\NeoCloud.exe"
!define MUI_FINISHPAGE_RUN_TEXT "Launch NeoCloud"
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

; An update replaces the exe, so the running copy has to go first.
!macro StopNeoCloud
  nsExec::Exec 'taskkill /IM NeoCloud.exe'
  Sleep 1500
  nsExec::Exec 'taskkill /F /IM NeoCloud.exe'
!macroend

Section "Install"
  SetShellVarContext current
  !insertmacro StopNeoCloud
  ; A fresh install starts at login; an update keeps the user's choice.
  ReadRegStr $0 HKCU "${UNINST_KEY}" "UninstallString"
  SetOutPath "$INSTDIR"
  File "/oname=NeoCloud.exe" "${EXE}"
  WriteUninstaller "$INSTDIR\Uninstall.exe"
  CreateShortcut "$SMPROGRAMS\NeoCloud.lnk" "$INSTDIR\NeoCloud.exe"
  StrCmp $0 "" 0 +2
    WriteRegStr HKCU "${RUN_KEY}" "${RUN_VALUE}" '"$INSTDIR\NeoCloud.exe" run'
  WriteRegStr HKCU "${UNINST_KEY}" "DisplayName" "${APP}"
  WriteRegStr HKCU "${UNINST_KEY}" "DisplayVersion" "${TAG}"
  WriteRegStr HKCU "${UNINST_KEY}" "Publisher" "NeoLabs Systems"
  WriteRegStr HKCU "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\NeoCloud.exe"
  WriteRegStr HKCU "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UNINST_KEY}" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteRegDWORD HKCU "${UNINST_KEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINST_KEY}" "NoRepair" 1
SectionEnd

; Removes the app, its shortcut and the login entry. The config and sync
; state in %APPDATA%\neocloud-lite stay.
Section "Uninstall"
  SetShellVarContext current
  !insertmacro StopNeoCloud
  DeleteRegValue HKCU "${RUN_KEY}" "${RUN_VALUE}"
  Delete "$SMPROGRAMS\NeoCloud.lnk"
  Delete "$INSTDIR\NeoCloud.exe"
  Delete "$INSTDIR\Uninstall.exe"
  RMDir "$INSTDIR"
  DeleteRegKey HKCU "${UNINST_KEY}"
SectionEnd
