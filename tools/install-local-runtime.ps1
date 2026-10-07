$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$runtimeDir = Join-Path $projectRoot 'artifacts/local-runtime'
$archivePath = Join-Path $env:TEMP 'touchdict-llama-b11429-win-cpu-x64.zip'
$downloadURL = 'https://github.com/ggml-org/llama.cpp/releases/download/b11429/llama-b11429-bin-win-cpu-x64.zip'
$expectedHash = '1283323272b04cd07905816a597a0da810918102de958f4ff6f7bbaa70ed2efe'
if (!(Test-Path -LiteralPath $archivePath) -or (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash -ne $expectedHash) {
    Invoke-WebRequest -Uri $downloadURL -OutFile $archivePath -TimeoutSec 180
}
if ((Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash -ne $expectedHash) {
    throw 'llama.cpp download checksum mismatch.'
}
New-Item -ItemType Directory -Path $runtimeDir -Force | Out-Null
Expand-Archive -LiteralPath $archivePath -DestinationPath $runtimeDir -Force
Invoke-WebRequest -Uri 'https://raw.githubusercontent.com/ggml-org/llama.cpp/b11429/LICENSE' -OutFile (Join-Path $runtimeDir 'LICENSE-llama.cpp') -TimeoutSec 30
if (!(Test-Path -LiteralPath (Join-Path $runtimeDir 'llama-server.exe'))) {
    throw 'Downloaded package does not contain llama-server.exe at its root.'
}
Get-Item (Join-Path $runtimeDir 'llama-server.exe') | Select-Object FullName, Length
