@echo off
echo Starting Kronos Prediction Service...
echo.

set VENV_DIR=%~dp0venv
call "%VENV_DIR%\Scripts\activate.bat"

:: Optional: switch to Kronos-base for better accuracy (requires more VRAM ~500MB)
:: set KRONOS_MODEL=NeoQuasar/Kronos-base

cd /d "%~dp0"
python server.py

pause
