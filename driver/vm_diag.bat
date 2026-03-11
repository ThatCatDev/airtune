@echo off
set OUT=C:\Users\User\Desktop\driver\diag.txt
echo === Driver load errors === > %OUT%
wevtutil qe System /q:"*[System[Provider[@Name='Microsoft-Windows-Kernel-PnP'] and (Level=2 or Level=3)]]" /c:10 /f:text /rd:true >> %OUT% 2>&1
echo. >> %OUT%
echo === AirTuneAudio service errors === >> %OUT%
wevtutil qe System /q:"*[System[(EventID=7000 or EventID=7001 or EventID=7009 or EventID=7026 or EventID=7034)]]" /c:10 /f:text /rd:true >> %OUT% 2>&1
echo. >> %OUT%
echo === Bugcheck / WHEA === >> %OUT%
wevtutil qe System /q:"*[System[Provider[@Name='Microsoft-Windows-WER-SystemErrorReporting']]]" /c:5 /f:text /rd:true >> %OUT% 2>&1
echo. >> %OUT%
echo Done! Output at %OUT%
