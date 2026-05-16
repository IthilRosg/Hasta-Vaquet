@echo off
set ADB=C:\Users\Admin\AppData\Local\Android\Sdk\platform-tools\adb.exe

echo ============================================
echo  Hasta-Vaquet Log Capture (full logcat)
echo  Saving to: %~dp0hastavaquet_live.txt
echo  Press Ctrl+C after testing
echo ============================================

%ADB% logcat -c
%ADB% logcat -v time > "%~dp0hastavaquet_live.txt"
