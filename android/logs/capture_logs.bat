@echo off
set ADB=C:\Users\Admin\AppData\Local\Android\Sdk\platform-tools\adb.exe

echo ============================================
echo  Hasta-Vaquet Log Capture
echo  Saving to: %~dp0live_logs.txt
echo.
echo  1. Run this script
echo  2. Launch app on phone
echo  3. Press Connect
echo  4. Come back here and press Ctrl+C
echo ============================================

%ADB% logcat -c
%ADB% logcat -v threadtime > "%~dp0live_logs.txt"
