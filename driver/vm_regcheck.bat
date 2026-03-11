@echo off
set OUT=C:\Users\User\Desktop\driver\regcheck.txt
echo === AirTune Driver Diagnostics === > %OUT%
echo Time: %date% %time% >> %OUT%
echo. >> %OUT%
reg query "HKLM\SOFTWARE\AirTune" >> %OUT% 2>&1
echo. >> %OUT%
echo === Device Status === >> %OUT%
pnputil /enum-devices /class MEDIA >> %OUT% 2>&1
echo. >> %OUT%
echo === Service Status === >> %OUT%
sc query AirTuneAudio >> %OUT% 2>&1
echo Done! Output at %OUT%
