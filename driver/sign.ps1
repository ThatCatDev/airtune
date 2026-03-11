$tp = '695B1E7BD9F8433270E6260D9E90B06038806872'
$signtool = 'C:\Program Files (x86)\Windows Kits\10\bin\10.0.26100.0\x64\signtool.exe'
$catFile = 'C:\Users\bobet\GolandProjects\airtune\driver\AirTuneAudio\x64\Debug\AirTuneAudio\airtuneaudio.cat'
$sysFile = 'C:\Users\bobet\GolandProjects\airtune\driver\AirTuneAudio\x64\Debug\AirTuneAudio.sys'

& $signtool sign /fd SHA256 /s My /sha1 $tp /t http://timestamp.digicert.com $catFile
& $signtool sign /fd SHA256 /s My /sha1 $tp /t http://timestamp.digicert.com $sysFile
