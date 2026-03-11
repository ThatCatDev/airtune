@echo off
:: AirTune Virtual Audio Driver — Uninstall Script
:: Must be run as Administrator.

echo Uninstalling AirTune Virtual Audio Driver...

:: Remove the device instance
devcon remove Root\AirTuneAudio 2>nul

:: Delete the driver from the driver store
pnputil /delete-driver AirTuneAudio.inf /uninstall /force
if %ERRORLEVEL% NEQ 0 (
    echo WARNING: Could not remove driver package. It may have already been removed.
)

echo.
echo Uninstall complete.
pause
