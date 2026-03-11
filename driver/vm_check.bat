@echo off
echo === Device Status ===
pnputil /enum-devices /instanceid "ROOT\MEDIA\*" /problem
echo.
echo === All MEDIA devices ===
pnputil /enum-devices /class MEDIA
echo.
echo === System Event Log (last 5 audio errors) ===
wevtutil qe System /q:"*[System[Provider[@Name='Service Control Manager'] and (Level=2)]]" /c:5 /f:text /rd:true
echo.
echo === Driver Files ===
dir C:\Windows\System32\drivers\AirTune* 2>nul
pause
