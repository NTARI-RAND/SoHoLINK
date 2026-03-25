; SoHoLINK Windows Installer
; NSIS Script — produces a single-click installer for end users
;
; The installer copies SoHoLINK.exe (the zero-CLI launcher) and optionally
; the headless CLI binary. On finish, it launches SoHoLINK which silently
; initializes the node and opens the dashboard in the user's browser.

!include "MUI2.nsh"

; General Configuration
Name "SoHoLINK"
OutFile "..\..\dist\SoHoLINK-Setup.exe"
InstallDir "$PROGRAMFILES\SoHoLINK"
InstallDirRegKey HKLM "Software\SoHoLINK" "Install_Dir"
RequestExecutionLevel admin

; Version Information
VIProductVersion "1.0.0.0"
VIAddVersionKey "ProductName" "SoHoLINK"
VIAddVersionKey "CompanyName" "Network Theory Applied Research Institute"
VIAddVersionKey "FileDescription" "SoHoLINK — Federated Compute Marketplace"
VIAddVersionKey "FileVersion" "1.0.0.0"
VIAddVersionKey "ProductVersion" "1.0.0.0"
VIAddVersionKey "LegalCopyright" "© 2026 NTARI"

; Interface Settings
!define MUI_ABORTWARNING
; Uncomment these when logo assets are ready:
; !define MUI_ICON "logo.ico"
; !define MUI_UNICON "logo.ico"
; !define MUI_WELCOMEFINISHPAGE_BITMAP "wizard-banner.bmp"
; !define MUI_HEADERIMAGE
; !define MUI_HEADERIMAGE_BITMAP "header.bmp"
; !define MUI_HEADERIMAGE_RIGHT

; Welcome page
!define MUI_WELCOMEPAGE_TITLE "Welcome to SoHoLINK"
!define MUI_WELCOMEPAGE_TEXT "Turn your idle hardware into income.$\r$\n$\r$\nSoHoLINK connects your computer to a federated marketplace where others rent your spare CPU, GPU, storage, and bandwidth — all while you stay in control.$\r$\n$\r$\nClick Next to install."

; Pages
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_LICENSE "..\..\LICENSE"
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES

; Finish page — launches the app (which opens the browser)
!define MUI_FINISHPAGE_TITLE "You're All Set"
!define MUI_FINISHPAGE_TEXT "SoHoLINK is installed and ready.$\r$\n$\r$\nClick Finish to open SoHoLINK in your browser."
!define MUI_FINISHPAGE_RUN "$INSTDIR\SoHoLINK.exe"
!define MUI_FINISHPAGE_RUN_TEXT "Launch SoHoLINK"
!define MUI_FINISHPAGE_RUN_CHECKED
!insertmacro MUI_PAGE_FINISH

; Uninstaller pages
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

; Languages
!insertmacro MUI_LANGUAGE "English"

; ---------------------------------------------------------------------------
; Installer
; ---------------------------------------------------------------------------
Section "SoHoLINK" SecCore
    SectionIn RO

    SetOutPath $INSTDIR

    ; Main launcher (no console window — double-click to run)
    File "..\..\bin\SoHoLINK.exe"

    ; Optional: headless CLI for advanced users
    File /nonfatal "..\..\bin\fedaaa-gui.exe"

    ; Start menu
    CreateDirectory "$SMPROGRAMS\SoHoLINK"
    CreateShortcut "$SMPROGRAMS\SoHoLINK\SoHoLINK.lnk" "$INSTDIR\SoHoLINK.exe"
    CreateShortcut "$SMPROGRAMS\SoHoLINK\Uninstall SoHoLINK.lnk" "$INSTDIR\uninstall.exe"

    ; Desktop shortcut
    CreateShortcut "$DESKTOP\SoHoLINK.lnk" "$INSTDIR\SoHoLINK.exe"

    ; Auto-start on login (current user, not admin — user can disable in Settings)
    WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "SoHoLINK" '"$INSTDIR\SoHoLINK.exe"'

    ; Add/Remove Programs registry
    WriteRegStr HKLM "Software\SoHoLINK" "Install_Dir" "$INSTDIR"
    WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\SoHoLINK" "DisplayName" "SoHoLINK"
    WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\SoHoLINK" "UninstallString" '"$INSTDIR\uninstall.exe"'
    WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\SoHoLINK" "DisplayIcon" "$INSTDIR\SoHoLINK.exe"
    WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\SoHoLINK" "Publisher" "Network Theory Applied Research Institute"
    WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\SoHoLINK" "DisplayVersion" "1.0.0"
    WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\SoHoLINK" "NoModify" 1
    WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\SoHoLINK" "NoRepair" 1
    WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\SoHoLINK" "EstimatedSize" 30000 ; ~30 MB

    WriteUninstaller "$INSTDIR\uninstall.exe"
SectionEnd

; ---------------------------------------------------------------------------
; Uninstaller
; ---------------------------------------------------------------------------
Section "Uninstall"
    ; Stop running instance
    nsExec::ExecToLog 'taskkill /F /IM SoHoLINK.exe'

    ; Remove auto-start
    DeleteRegValue HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "SoHoLINK"

    ; Remove files
    Delete "$INSTDIR\SoHoLINK.exe"
    Delete "$INSTDIR\fedaaa-gui.exe"
    Delete "$INSTDIR\uninstall.exe"

    ; Remove shortcuts
    Delete "$SMPROGRAMS\SoHoLINK\*.lnk"
    RMDir "$SMPROGRAMS\SoHoLINK"
    Delete "$DESKTOP\SoHoLINK.lnk"

    ; Remove registry
    DeleteRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\SoHoLINK"
    DeleteRegKey HKLM "Software\SoHoLINK"

    ; Remove install dir (but NOT user data in AppData)
    RMDir "$INSTDIR"

    ; Notify user that data is preserved
    MessageBox MB_OK "SoHoLINK has been uninstalled.$\r$\n$\r$\nYour node data is preserved in:$\r$\n$LOCALAPPDATA\SoHoLINK$\r$\n$\r$\nDelete that folder manually if you want a clean removal."
SectionEnd
