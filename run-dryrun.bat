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
echo ============================================================
echo  DRY RUN: only queries TMDB and prints target paths.
echo  Nothing is written to disk. Safe to run first.
echo ============================================================
echo.
medialinker.exe -config config.yaml -once -dry-run
echo.
echo Done. If the target paths look right, run run.bat instead.
pause
