@echo off
chcp 65001 >nul
cd /d "%~dp0"

if not exist medialinker.exe goto noexe
if not exist config.yaml goto nocfg
goto run

:noexe
echo [ERROR] medialinker.exe not found in this folder.
echo Keep run.bat in the same folder as medialinker.exe.
pause
exit /b 1

:nocfg
echo [ERROR] config.yaml not found in this folder.
echo It ships with the zip. If you lost it, re-download the package.
pause
exit /b 1

:run
echo Starting medialinker. Keep this window open.
echo Press Ctrl+C to stop.
echo.
medialinker.exe -config config.yaml
echo.
echo Program exited.
pause
