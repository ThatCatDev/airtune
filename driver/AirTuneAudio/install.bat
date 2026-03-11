@echo off
:: AirTune Virtual Audio Driver — Install Script
:: Must be run as Administrator.

echo Installing AirTune Virtual Audio Driver...

:: Add the driver package to the driver store
pnputil /add-driver "%~dp0AirTuneAudio\AirTuneAudio.inf" /install
if %ERRORLEVEL% NEQ 0 (
    echo ERROR: Failed to add driver. Make sure you are running as Administrator.
    echo If test signing is required, run: bcdedit /set testsigning on
    pause
    exit /b 1
)

:: Create a root-enumerated device instance
devcon install "%~dp0AirTuneAudio\AirTuneAudio.inf" Root\AirTuneAudio 2>nul
if %ERRORLEVEL% NEQ 0 (
    echo NOTE: devcon not found or device already exists. Check Device Manager.
)

echo.
echo Installation complete. "AirTune Virtual Speaker" should appear in Sound settings.
pause
