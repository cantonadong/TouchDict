$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$artifactDir = Join-Path $projectRoot 'artifacts'
$outputPath = Join-Path $artifactDir 'TouchDict.exe'

Get-Process -Name 'TouchDict' -ErrorAction SilentlyContinue | Stop-Process -Force
New-Item -ItemType Directory -Force -Path $artifactDir | Out-Null
if (!(Test-Path -LiteralPath (Join-Path $artifactDir 'local-runtime/llama-server.exe'))) {
    & (Join-Path $projectRoot 'tools/install-local-runtime.ps1')
}

Push-Location $projectRoot
try {
    gofmt -w cmd internal
    go mod tidy
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    go build -trimpath -ldflags '-H=windowsgui -s -w' -o $outputPath ./cmd/touchdict
    if ($LASTEXITCODE -ne 0) { throw 'Go build failed.' }
    if (Test-Path (Join-Path $projectRoot 'gemini_key.txt')) {
        Copy-Item -Force (Join-Path $projectRoot 'gemini_key.txt') (Join-Path $artifactDir 'gemini_key.txt')
    }
} finally {
    Pop-Location
}

Get-Item $outputPath | Select-Object FullName, Length, LastWriteTime
