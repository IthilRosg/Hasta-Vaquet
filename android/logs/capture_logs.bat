@echo off
echo ============================================
echo  Hasta-Vaquet Log Capture
echo  Saving to: %~dp0live_logs.txt
echo.
echo  1. Run this script
echo  2. Launch app on phone
echo  3. Press Connect
echo  4. Come back here and press Ctrl+C
echo ============================================

adb logcat -c
adb logcat -v threadtime > "%~dp0live_logs.txt"
