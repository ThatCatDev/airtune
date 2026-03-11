@echo off
echo Importing certificate...
certutil -addstore Root "C:\Users\User\Desktop\driver\AirTuneDev.cer"
certutil -addstore TrustedPublisher "C:\Users\User\Desktop\driver\AirTuneDev.cer"
echo Installing driver...
pnputil /add-driver "C:\Users\User\Desktop\driver\AirTuneAudio.inf" /install
echo Creating device...
devcon install "C:\Users\User\Desktop\driver\AirTuneAudio.inf" Root\AirTuneAudio
echo Done!
pause
