@echo off
set JAVA_HOME=C:\Users\Admin\AppData\Local\jdk-17.0.2
set PATH=%JAVA_HOME%\bin;%PATH%
C:\Users\Admin\AppData\Local\Android\Sdk\cmdline-tools\latest\bin\sdkmanager.bat --sdk_root=C:\Users\Admin\AppData\Local\Android\Sdk "platforms;android-35"
echo EXIT_CODE=%ERRORLEVEL%
