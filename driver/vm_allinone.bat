@echo off
set OUT=C:\Users\User\Desktop\driver\allinone.txt
echo === Step 1: Remove all AirTune devices === > %OUT%
"C:\Users\User\Desktop\driver\devcon.exe" remove Root\AirTuneAudio >> %OUT% 2>&1
echo. >> %OUT%

echo === Step 2: Delete ALL oem driver packages === >> %OUT%
for /f "tokens=1" %%a in ('pnputil /enum-drivers /class MEDIA ^| findstr /i "oem"') do (
    echo Deleting %%a >> %OUT%
    pnputil /delete-driver %%a /force >> %OUT% 2>&1
)
echo. >> %OUT%

echo === Step 3: Stop and delete service === >> %OUT%
sc stop AirTuneAudio >> %OUT% 2>&1
sc delete AirTuneAudio >> %OUT% 2>&1
echo. >> %OUT%

echo === Step 4: Clear diagnostics === >> %OUT%
reg delete "HKLM\SOFTWARE\AirTune" /f >> %OUT% 2>&1
echo. >> %OUT%

echo === Step 5: Install new driver === >> %OUT%
pnputil /add-driver "C:\Users\User\Desktop\driver\AirTuneAudio.inf" /install >> %OUT% 2>&1
echo. >> %OUT%

echo === Step 6: Create device === >> %OUT%
"C:\Users\User\Desktop\driver\devcon.exe" install "C:\Users\User\Desktop\driver\AirTuneAudio.inf" Root\AirTuneAudio >> %OUT% 2>&1
echo. >> %OUT%

echo === Step 7: Restart device === >> %OUT%
pnputil /restart-device "ROOT\MEDIA\0000" >> %OUT% 2>&1
echo. >> %OUT%

echo === Step 8: Device status === >> %OUT%
pnputil /enum-devices /class MEDIA >> %OUT% 2>&1
echo. >> %OUT%

echo === Step 9: Service status === >> %OUT%
sc query AirTuneAudio >> %OUT% 2>&1
echo. >> %OUT%

echo === Step 10: Driver diagnostics (registry) === >> %OUT%
reg query "HKLM\SOFTWARE\AirTune" >> %OUT% 2>&1
echo. >> %OUT%

echo === Step 11: Recent PnP errors === >> %OUT%
wevtutil qe System /q:"*[System[Provider[@Name='Microsoft-Windows-Kernel-PnP'] and (Level=2 or Level=3)]]" /c:3 /f:text /rd:true >> %OUT% 2>&1

echo Done! Output at %OUT%
