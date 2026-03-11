@echo off
REM Build airtune_sync.dll (64-bit)
REM Requires: Visual Studio 2022 Build Tools with C++ workload

if exist "C:\Program Files (x86)\Microsoft Visual Studio\2022\BuildTools\VC\Auxiliary\Build\vcvarsall.bat" (
    call "C:\Program Files (x86)\Microsoft Visual Studio\2022\BuildTools\VC\Auxiliary\Build\vcvarsall.bat" x64
) else if exist "C:\Program Files\Microsoft Visual Studio\2022\Community\VC\Auxiliary\Build\vcvarsall.bat" (
    call "C:\Program Files\Microsoft Visual Studio\2022\Community\VC\Auxiliary\Build\vcvarsall.bat" x64
) else (
    echo ERROR: Visual Studio 2022 not found
    exit /b 1
)

cl.exe /LD /EHsc /O2 /DUNICODE /D_UNICODE /DNDEBUG ^
    /I. ^
    airtune_sync.cpp ^
    /Fe:airtune_sync.dll ^
    /link ole32.lib uuid.lib user32.lib kernel32.lib advapi32.lib
