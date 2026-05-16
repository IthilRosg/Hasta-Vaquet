@echo off
setlocal enabledelayedexpansion
set ADB=C:\Users\Admin\AppData\Local\Android\Sdk\platform-tools\adb.exe

echo ============================================
echo  Hasta-Vaquet Log Capture
echo  Saving to: %~dp0live_logs.txt
echo ============================================

%ADB% logcat -c

echo Waiting for com.hastavaquet process...

:waitloop
for /f %%p in ('%ADB% shell "pidof com.hastavaquet" 2^>nul') do set PID=%%p
if "!PID!"=="" (
    timeout /t 1 >nul
    goto waitloop
)

echo Capturing PID=!PID!  Press Ctrl+C to stop.
%ADB% logcat -v time --pid=!PID! > "%~dp0live_logs.txt"
