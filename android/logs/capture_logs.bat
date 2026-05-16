@echo off
set ADB=C:\Users\Admin\AppData\Local\Android\Sdk\platform-tools\adb.exe

echo ============================================
echo  Hasta-Vaquet Log Capture
echo  Saving to: %~dp0live_logs.txt
echo ============================================

%ADB% logcat -c
%ADB% logcat -v time GoLog:V APP:V VPN:V MAIN:V AndroidRuntime:E *:S > "%~dp0live_logs.txt"
