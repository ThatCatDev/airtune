# AirTune driver signing and installation script
# Must be run as Administrator

$ErrorActionPreference = "Stop"
$driverDir = "C:\Users\bobet\GolandProjects\airtune\driver\AirTuneAudio\x64\Debug"
$sysFile = "$driverDir\AirTuneAudio.sys"
$infFile = "$driverDir\AirTuneAudio.inf"
$certFile = "C:\Users\bobet\GolandProjects\airtune\driver\AirTuneDev.cer"
$signtool = "C:\Program Files (x86)\Windows Kits\10\bin\10.0.26100.0\x64\signtool.exe"
$inf2cat = "C:\Program Files (x86)\Windows Kits\10\bin\10.0.26100.0\x86\Inf2Cat.exe"

# 1. Create self-signed code signing cert (skip if already exists)
$existing = Get-ChildItem Cert:\LocalMachine\My | Where-Object { $_.Subject -eq "CN=AirTune Dev" -and $_.HasPrivateKey }
if ($existing) {
    $cert = $existing[0]
    Write-Host "Using existing certificate: $($cert.Thumbprint)" -ForegroundColor Yellow
} else {
    Write-Host "Creating self-signed certificate..." -ForegroundColor Cyan
    $cert = New-SelfSignedCertificate -Type CodeSigningCert -Subject "CN=AirTune Dev" -CertStoreLocation Cert:\LocalMachine\My
    Write-Host "Certificate thumbprint: $($cert.Thumbprint)"
}

# 2. Export and trust it (idempotent)
Write-Host "Trusting certificate..." -ForegroundColor Cyan
Export-Certificate -Cert $cert -FilePath $certFile -Force | Out-Null
Import-Certificate -FilePath $certFile -CertStoreLocation Cert:\LocalMachine\Root | Out-Null
Import-Certificate -FilePath $certFile -CertStoreLocation Cert:\LocalMachine\TrustedPublisher | Out-Null
Write-Host "Certificate trusted (Root + TrustedPublisher)"

# 3. Generate catalog file with Inf2Cat
Write-Host "Generating catalog file..." -ForegroundColor Cyan
& $inf2cat /driver:$driverDir /os:10_X64 /verbose
if ($LASTEXITCODE -ne 0) {
    Write-Host "Inf2Cat failed (exit $LASTEXITCODE), trying stampinf first..." -ForegroundColor Yellow
    # Ensure the INF has proper CatalogFile directive
    $stampinf = "C:\Program Files (x86)\Windows Kits\10\bin\10.0.26100.0\x86\stampinf.exe"
    if (Test-Path $stampinf) {
        & $stampinf -f $infFile -d * -a amd64 -v *
    }
    & $inf2cat /driver:$driverDir /os:10_X64 /verbose
    if ($LASTEXITCODE -ne 0) { throw "Inf2Cat failed" }
}
Write-Host "Catalog file generated"

# 4. Sign the .sys and .cat files
Write-Host "Signing driver files..." -ForegroundColor Cyan
& $signtool sign /fd SHA256 /s My /sm /n "AirTune Dev" $sysFile
if ($LASTEXITCODE -ne 0) { throw "signtool failed on .sys" }

$catFile = "$driverDir\AirTuneAudio.cat"
if (Test-Path $catFile) {
    & $signtool sign /fd SHA256 /s My /sm /n "AirTune Dev" $catFile
    if ($LASTEXITCODE -ne 0) { throw "signtool failed on .cat" }
}
Write-Host "Driver files signed successfully"

# 5. Install
Write-Host "Installing driver..." -ForegroundColor Cyan
& pnputil /add-driver $infFile /install
Write-Host ""
Write-Host "Done! Check Sound settings for 'AirTune Virtual Speaker'" -ForegroundColor Green
