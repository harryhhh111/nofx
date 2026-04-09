@echo off
echo ============================================
echo  Kronos Service Setup (RTX 4060 / CUDA 12)
echo ============================================
echo.

set PYTHON=C:\Users\Administrator\AppData\Local\Programs\Python\Python312\python.exe
set VENV_DIR=%~dp0venv

:: 1. Create virtual environment
echo [1/5] Creating virtual environment...
if not exist "%VENV_DIR%" (
    "%PYTHON%" -m venv "%VENV_DIR%"
    echo     Created: %VENV_DIR%
) else (
    echo     Already exists, skipping.
)

:: Activate
call "%VENV_DIR%\Scripts\activate.bat"

:: 2. Install PyTorch with CUDA 12.1 (for RTX 4060)
echo.
echo [2/5] Installing PyTorch with CUDA 12.1...
pip install torch torchvision torchaudio --index-url https://download.pytorch.org/whl/cu121

:: 3. Install service dependencies
echo.
echo [3/5] Installing service dependencies...
pip install -r "%~dp0requirements.txt"

:: 4. Clone Kronos repo
echo.
echo [4/5] Cloning Kronos repository...
if not exist "%~dp0Kronos" (
    git clone https://github.com/shiyu-coder/Kronos.git "%~dp0Kronos"
    echo     Cloned to %~dp0Kronos
) else (
    echo     Already exists, skipping.
)

:: 5. Install Kronos dependencies
echo.
echo [5/5] Installing Kronos dependencies...
if exist "%~dp0Kronos\requirements.txt" (
    pip install -r "%~dp0Kronos\requirements.txt"
)

echo.
echo ============================================
echo  Setup complete!
echo  To start the service, run: start.bat
echo ============================================
pause
