#!/usr/bin/env pwsh
#
# SentryUSB Release Builder
# Builds frontend, cross-compiles binaries, and creates Raspberry Pi images
#
# Usage:
#   .\build-release.ps1 [-Arch arm64|armhf|both] [-SkipBinaries] [-SkipImage] [-Release v1.0.0]
#

param(
    [ValidateSet('arm64', 'armhf', 'both')]
    [string]$Arch = 'arm64',
    [switch]$SkipBinaries,
    [switch]$SkipImage,
    [string]$Release = ''
)

$ErrorActionPreference = "Stop"
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path

function Write-Step {
    param([string]$Message)
    Write-Host "`n=== $Message ===" -ForegroundColor Cyan
}

function Write-Success {
    param([string]$Message)
    Write-Host "[OK] $Message" -ForegroundColor Green
}

function Write-Error-Custom {
    param([string]$Message)
    Write-Host "[FAIL] $Message" -ForegroundColor Red
}

# Check prerequisites
Write-Step "Checking prerequisites"

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Error-Custom "Go not found. Install from https://go.dev/dl/"
    exit 1
}

if (-not (Get-Command node -ErrorAction SilentlyContinue)) {
    Write-Error-Custom "Node.js not found. Install from https://nodejs.org/"
    exit 1
}

if (-not (Get-Command wsl -ErrorAction SilentlyContinue)) {
    Write-Error-Custom "WSL2 not found. Run: wsl --install"
    exit 1
}

if ($Release -ne '' -and -not (Get-Command gh -ErrorAction SilentlyContinue)) {
    Write-Error-Custom "gh CLI not found but -Release was specified. Install from https://cli.github.com/"
    exit 1
}

Write-Success "All prerequisites found"

# Build binaries
if (-not $SkipBinaries) {
    Write-Step "Building frontend"
    Push-Location "$RepoRoot\web"
    try {
        npm ci --no-audit --no-fund
        if ($LASTEXITCODE -ne 0) { throw "npm ci failed" }
        
        npm run build
        if ($LASTEXITCODE -ne 0) { throw "npm build failed" }
        
        Write-Success "Frontend built"
    }
    finally {
        Pop-Location
    }

    Write-Step "Copying frontend to server/static"
    Push-Location "$RepoRoot\server"
    try {
        if (Test-Path "static") {
            Remove-Item -Recurse -Force "static"
        }
        Copy-Item -Recurse "..\web\dist" "static"
        Write-Success "Frontend copied"
    }
    finally {
        Pop-Location
    }

    Write-Step "Building Go binaries"
    Push-Location "$RepoRoot\server"
    try {
        # ARM64
        Write-Host "  Building ARM64 binary..."
        $env:GOOS = "linux"
        $env:GOARCH = "arm64"
        go build -o bin/sentryusb-linux-arm64 .
        if ($LASTEXITCODE -ne 0) { throw "ARM64 build failed" }
        Write-Success "ARM64 binary: $(Get-Item bin/sentryusb-linux-arm64 | Select-Object -ExpandProperty Length) bytes"

        # ARMv7
        Write-Host "  Building ARMv7 binary..."
        $env:GOOS = "linux"
        $env:GOARCH = "arm"
        $env:GOARM = "7"
        go build -o bin/sentryusb-linux-armv7 .
        if ($LASTEXITCODE -ne 0) { throw "ARMv7 build failed" }
        Write-Success "ARMv7 binary: $(Get-Item bin/sentryusb-linux-armv7 | Select-Object -ExpandProperty Length) bytes"
    }
    finally {
        Pop-Location
    }

    if ($Release -ne '') {
        Write-Step "Uploading binaries to GitHub release $Release"
        gh release upload $Release `
            "$RepoRoot\server\bin\sentryusb-linux-arm64" `
            "$RepoRoot\server\bin\sentryusb-linux-armv7" `
            --clobber
        if ($LASTEXITCODE -ne 0) { throw "gh release upload failed" }
        Write-Success "Binaries uploaded to $Release"
    }
}
else {
    Write-Host "Skipping binary build (using existing binaries)" -ForegroundColor Yellow
}

# Build images
if (-not $SkipImage) {
    Write-Step "Checking WSL2 Docker"
    
    # Check if Docker is running in WSL2
    $dockerCheck = wsl -d Ubuntu -- bash -c "sudo docker info > /dev/null 2>&1; echo `$?"
    if ($dockerCheck -ne "0") {
        Write-Host "Starting Docker in WSL2..."
        wsl -d Ubuntu -- sudo service docker start
        Start-Sleep -Seconds 3
    }
    Write-Success "Docker is running"

    # Build images based on architecture selection
    $archList = @()
    if ($Arch -eq 'both') {
        $archList = @('arm64', 'armhf')
    }
    else {
        $archList = @($Arch)
    }

    foreach ($targetArch in $archList) {
        Write-Step "Building $targetArch image (this takes ~35-40 minutes)"
        
        $buildScript = "/mnt/c/Users/scott/Documents/Sentry-Six-Assets/SentryUSB/internal/build-local.sh"
        wsl -d Ubuntu -- sudo bash $buildScript $targetArch
        
        if ($LASTEXITCODE -ne 0) {
            Write-Error-Custom "$targetArch image build failed"
            exit 1
        }
        
        Write-Success "$targetArch image built"
    }

    # Copy images to deploy folder
    Write-Step "Copying images to deploy folder"
    
    if (-not (Test-Path "$RepoRoot\deploy")) {
        New-Item -ItemType Directory -Path "$RepoRoot\deploy" | Out-Null
    }

    wsl -d Ubuntu -- bash -c "sudo cp /tmp/pi-gen/deploy/image_*.zip /mnt/c/Users/scott/Documents/Sentry-Six-Assets/SentryUSB/deploy/ 2>/dev/null || true"
    
    $images = Get-ChildItem "$RepoRoot\deploy\image_*.zip" -ErrorAction SilentlyContinue
    if ($images) {
        Write-Success "Images copied to deploy/"
        foreach ($img in $images) {
            Write-Host "  - $($img.Name) ($([math]::Round($img.Length / 1MB, 1)) MB)" -ForegroundColor Gray
        }
    }
    else {
        Write-Error-Custom "No images found in deploy folder"
    }
}
else {
    Write-Host "Skipping image build" -ForegroundColor Yellow
}

Write-Step "Build complete!"
Write-Host ""
Write-Host "Next steps:" -ForegroundColor Cyan
Write-Host "  1. Test the image on actual hardware"
if ($Release -eq '') {
    Write-Host "  2. Upload binaries to a GitHub release (re-run with -Release v1.0.0):"
    Write-Host "     - server/bin/sentryusb-linux-arm64"
    Write-Host "     - server/bin/sentryusb-linux-armv7"
    Write-Host "  3. Distribute the image from deploy/ folder"
} else {
    Write-Host "  2. Binaries already uploaded to release $Release"
    Write-Host "  3. Distribute the image from deploy/ folder"
}
Write-Host ""
