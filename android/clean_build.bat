@echo off
echo ============================================
echo  NUCLEAR CLEAN — удаление ВСЕХ кешей
echo ============================================

cd /d "%~dp0"

echo 1. Killing Gradle daemon...
call gradlew --stop 2>nul

echo 2. Removing .gradle...
rmdir /s /q .gradle 2>nul

echo 3. Removing app/build...
rmdir /s /q app\build 2>nul

echo 4. Removing build...
rmdir /s /q build 2>nul

echo 5. Removing global Gradle cache for our project...
rmdir /s /q "%USERPROFILE%\.gradle\caches\modules-2\files-2.1\com.hastavaquet" 2>nul

echo.
echo ============================================
echo  DONE. Now in Android Studio:
echo  1. File - Invalidate Caches - Invalidate and Restart
echo  2. Build - Clean Project
echo  3. Build - Rebuild Project
echo  4. Run
echo ============================================
pause
