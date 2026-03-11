@echo off
set OUT=C:\Users\User\Desktop\driver\check2.txt
echo === Device Status === > %OUT%
pnputil /enum-devices /class MEDIA >> %OUT% 2>&1
echo. >> %OUT%
echo === Driver Service === >> %OUT%
sc query AirTuneAudio >> %OUT% 2>&1
echo. >> %OUT%
echo === Recent PnP errors === >> %OUT%
wevtutil qe System /q:"*[System[Provider[@Name='Microsoft-Windows-Kernel-PnP'] and (Level=2 or Level=3)]]" /c:5 /f:text /rd:true >> %OUT% 2>&1
echo Done!
