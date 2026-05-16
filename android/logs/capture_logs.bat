@echo off
setlocal enabledelayedexpansion
set ADB=C:\Users\Admin\AppData\Local\Android\Sdk\platform-tools\adb.exe
set PACKAGE=com.hastavaquet

echo ============================================
echo  Hasta-Vaquet Log Capture (App Only)
echo  Filter: %PACKAGE%
echo  Saving to: %~dp0hastavaquet_live.txt
echo ============================================

:: Очищаем старые логи
%ADB% logcat -c

echo Starting log capture...
echo Press Ctrl+C to stop.

:: Универсальный способ: запускаем logcat и фильтруем вывод через findstr (аналог grep в Windows)
:: Это работает на всех версиях ADB и корректно выделяет логи вашего приложения.
%ADB% logcat -v time | findstr /i "%PACKAGE% HastaVaquet VPN Core" > "%~dp0hastavaquet_live.txt"
