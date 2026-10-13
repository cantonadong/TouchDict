param([switch]$Debug)
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$artifactDir = Join-Path $projectRoot 'artifacts'
$outputPath = Join-Path $artifactDir 'TouchDict.exe'

# Use MSVC when configured, otherwise use Rust's GNU linker with LLVM for C/resources.
$toolchain = 'stable-x86_64-pc-windows-msvc'
if (!(Get-Command cl.exe -ErrorAction SilentlyContinue)) {
    $toolchain = 'stable-x86_64-pc-windows-gnu'
    $llvmRoot = Join-Path $env:TEMP 'touchdict-rust-toolchain'
    $llvmBin = Get-ChildItem -LiteralPath $llvmRoot -Directory -ErrorAction SilentlyContinue |
        Sort-Object Name -Descending |
        ForEach-Object { Join-Path $_.FullName 'bin' } |
        Where-Object { Test-Path -LiteralPath (Join-Path $_ 'x86_64-w64-mingw32-gcc.exe') } |
        Select-Object -First 1
    if (!$llvmBin -and !(Get-Command x86_64-w64-mingw32-gcc.exe -ErrorAction SilentlyContinue)) {
        throw 'Install MSVC Build Tools or LLVM/MinGW, or run tools/setup-rust-toolchain.ps1 first.'
    }
    if ($llvmBin) { $env:PATH = $llvmBin + ';' + $env:PATH }
    $env:CC = 'x86_64-w64-mingw32-gcc'
    $env:AR = 'llvm-ar'
    $rustRoot = (& rustc "+$toolchain" --print sysroot).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'Install the Rust GNU toolchain with rustup toolchain install stable-x86_64-pc-windows-gnu.' }
    $runtimeRoot = Join-Path $rustRoot 'lib/rustlib/x86_64-pc-windows-gnu'
    $lld = Get-Command ld.lld.exe -ErrorAction Stop
    $env:CARGO_TARGET_X86_64_PC_WINDOWS_GNU_LINKER = $lld.Source
    # Rust's bundled GNU ld can leave raw-dylib imports unresolved in a linked PE.
    # Keep the matching Rust CRT libraries, but use modern LLD to resolve imports.
    $env:CARGO_ENCODED_RUSTFLAGS = @(
        '-L', ('native=' + (Join-Path $runtimeRoot 'lib/self-contained')),
        '-C', 'linker-flavor=ld',
        '-C', 'link-self-contained=yes'
    ) -join [char]31
}

Push-Location $projectRoot
try {
    $cargoArguments = @("+$toolchain", 'build', '--locked')
    $profile = 'debug'
    if (!$Debug) { $cargoArguments += '--release'; $profile = 'release' }
    & cargo @cargoArguments
    if ($LASTEXITCODE -ne 0) { throw 'Rust build failed.' }
    $importReader = Get-Command llvm-readobj.exe -ErrorAction SilentlyContinue
    if ($importReader) {
        $imports = & $importReader.Source --coff-imports (Join-Path $projectRoot "target/$profile/TouchDict.exe")
        if ($LASTEXITCODE -ne 0) { throw 'Cannot inspect EXE dependencies.' }
        if ($imports -match 'Name: WebView2Loader') { throw 'EXE still requires an external WebView2Loader.dll.' }
    }
    # Only replace the user's runnable EXE once compilation has succeeded.
    Get-Process -Name 'TouchDict' -ErrorAction SilentlyContinue | Stop-Process -Force
    New-Item -ItemType Directory -Force -Path $artifactDir | Out-Null
    Copy-Item -LiteralPath (Join-Path $projectRoot "target/$profile/TouchDict.exe") -Destination $outputPath -Force
} finally {
    Pop-Location
}
Get-Item $outputPath | Select-Object FullName, Length, LastWriteTime
