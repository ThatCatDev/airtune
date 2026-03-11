@echo off
"C:\Program Files (x86)\Microsoft Visual Studio\2022\BuildTools\MSBuild\Current\Bin\MSBuild.exe" "%~dp0AirTuneAudio\AirTuneAudio.sln" /p:Configuration=Debug /p:Platform=x64
echo.
echo Exit code: %ERRORLEVEL%
