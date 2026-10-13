$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$toolRoot = Join-Path $env:TEMP 'touchdict-rust-toolchain'
$version = '20261006'
$archiveName = "llvm-mingw-$version-ucrt-x86_64.zip"
$bin = Join-Path $toolRoot "llvm-mingw-$version-ucrt-x86_64/bin"
if (!(Test-Path -LiteralPath (Join-Path $bin 'clang.exe'))) {
    New-Item -ItemType Directory -Force -Path $toolRoot | Out-Null
    $archive = Join-Path $toolRoot $archiveName
    Invoke-WebRequest "https://github.com/mstorsjo/llvm-mingw/releases/download/$version/$archiveName" -OutFile $archive -UseBasicParsing
    Expand-Archive -LiteralPath $archive -DestinationPath $toolRoot -Force
}
rustup toolchain install stable-x86_64-pc-windows-gnu
if ($LASTEXITCODE -ne 0) { throw 'Rust GNU toolchain installation failed.' }
Write-Output "Rust Windows toolchain ready: $bin"
