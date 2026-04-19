# Aurion installer for Windows — one command to get started.
#
# Install CLI (default): connects to aurion.studio
#   irm https://raw.githubusercontent.com/aurion-ai/aurion/main/scripts/install.ps1 | iex
#
# Self-host: starts a local Aurion server + installs CLI + configures
#   $env:AURION_MODE="local"; irm https://raw.githubusercontent.com/aurion-ai/aurion/main/scripts/install.ps1 | iex
#

$ErrorActionPreference = "Stop"

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
$RepoUrl       = "https://github.com/aurion-ai/aurion.git"
$RepoWebUrl    = "https://github.com/aurion-ai/aurion"
$DefaultInstallDir = Join-Path $env:USERPROFILE ".aurion\server"
$InstallDir    = if ($env:AURION_INSTALL_DIR) { $env:AURION_INSTALL_DIR } else { $DefaultInstallDir }

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
function Write-Info  { param([string]$Msg) Write-Host "==> $Msg" -ForegroundColor Cyan }
function Write-Ok    { param([string]$Msg) Write-Host "[OK] $Msg" -ForegroundColor Green }
function Write-Warn  { param([string]$Msg) Write-Warning $Msg }
function Write-Fail  { param([string]$Msg) Write-Host "[ERROR] $Msg" -ForegroundColor Red; exit 1 }

function Test-CommandExists {
    param([string]$Name)
    $null -ne (Get-Command $Name -ErrorAction SilentlyContinue)
}

function Get-LatestVersion {
    try {
        $release = Invoke-RestMethod -Uri "https://api.github.com/repos/aurion-ai/aurion/releases/latest" -ErrorAction Stop
        return $release.tag_name
    } catch {
        return $null
    }
}

# ---------------------------------------------------------------------------
# CLI Installation
# ---------------------------------------------------------------------------
function Install-CliBinary {
    Write-Info "Installing Aurion CLI from GitHub Releases..."

    if (-not [Environment]::Is64BitOperatingSystem) {
        Write-Fail "Aurion requires a 64-bit Windows installation."
    }
    $arch = "amd64"

    $latest = Get-LatestVersion
    if (-not $latest) {
        Write-Fail "Could not determine latest release. Check your network connection."
    }

    $url = "https://github.com/aurion-ai/aurion/releases/download/$latest/aurion_windows_$arch.zip"
    $tmpDir = Join-Path ([System.IO.Path]::GetTempPath()) "aurion-install"

    if (Test-Path $tmpDir) { Remove-Item $tmpDir -Recurse -Force }
    New-Item -ItemType Directory -Path $tmpDir | Out-Null

    Write-Info "Downloading $url ..."
    try {
        Invoke-WebRequest -Uri $url -OutFile (Join-Path $tmpDir "aurion.zip") -UseBasicParsing
    } catch {
        Remove-Item $tmpDir -Recurse -Force
        Write-Fail "Failed to download CLI binary: $_"
    }

    # Verify SHA256 checksum
    $checksumUrl = "https://github.com/aurion-ai/aurion/releases/download/$latest/checksums.txt"
    try {
        $checksums = Invoke-WebRequest -Uri $checksumUrl -UseBasicParsing -ErrorAction Stop
        $zipFile = Join-Path $tmpDir "aurion.zip"
        $actualHash = (Get-FileHash -Path $zipFile -Algorithm SHA256).Hash.ToLower()
        $expectedLine = ($checksums.Content -split "`n") | Where-Object { $_ -match "aurion_windows_$arch\.zip" } | Select-Object -First 1
        if ($expectedLine) {
            $expectedHash = ($expectedLine -split "\s+")[0].ToLower()
            if ($actualHash -ne $expectedHash) {
                Remove-Item $tmpDir -Recurse -Force
                Write-Fail "Checksum verification failed. Expected: $expectedHash, Got: $actualHash"
            }
            Write-Ok "Checksum verified"
        } else {
            Write-Warn "Could not find checksum entry for windows_$arch — skipping verification."
        }
    } catch {
        Write-Warn "Could not download checksums.txt — skipping verification."
    }

    Expand-Archive -Path (Join-Path $tmpDir "aurion.zip") -DestinationPath $tmpDir -Force

    $binDir = Join-Path $env:USERPROFILE ".aurion\bin"
    if (-not (Test-Path $binDir)) {
        New-Item -ItemType Directory -Path $binDir -Force | Out-Null
    }

    $exeSrc = Join-Path $tmpDir "aurion.exe"
    if (-not (Test-Path $exeSrc)) {
        $exeSrc = Get-ChildItem -Path $tmpDir -Filter "aurion.exe" -Recurse | Select-Object -First 1 -ExpandProperty FullName
    }
    if (-not $exeSrc -or -not (Test-Path $exeSrc)) {
        Remove-Item $tmpDir -Recurse -Force
        Write-Fail "aurion.exe not found in downloaded archive."
    }

    Copy-Item $exeSrc (Join-Path $binDir "aurion.exe") -Force
    Remove-Item $tmpDir -Recurse -Force

    Add-ToUserPath $binDir
    Write-Ok "Aurion CLI installed to $binDir\aurion.exe"
}

function Add-ToUserPath {
    param([string]$Dir)
    $currentPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if ($currentPath -and $currentPath.Split(";") -contains $Dir) {
        return
    }
    $newPath = if ($currentPath) { "$currentPath;$Dir" } else { $Dir }
    [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
    # Also update current session
    if ($env:Path -notlike "*$Dir*") {
        $env:Path = "$Dir;$env:Path"
    }
    Write-Info "Added $Dir to user PATH (restart your terminal for other sessions to pick it up)."
}

function Install-Cli {
    if (Test-CommandExists "aurion") {
        $currentVer = (aurion version 2>$null) -replace '.*?(v[\d.]+).*','$1'
        $latestVer = Get-LatestVersion

        $currentCmp = $currentVer -replace '^v',''
        $latestCmp = if ($latestVer) { $latestVer -replace '^v','' } else { $null }

        $isUpToDate = -not $latestCmp
        if (-not $isUpToDate) {
            try {
                $isUpToDate = [System.Version]$currentCmp -ge [System.Version]$latestCmp
            } catch {
                $isUpToDate = $currentCmp -eq $latestCmp
            }
        }

        if ($isUpToDate) {
            Write-Ok "Aurion CLI is up to date ($currentVer)"
            return
        }

        Write-Info "Aurion CLI $currentVer installed, latest is $latestVer - upgrading..."
        Install-CliBinary

        $newVer = (aurion version 2>$null) -replace '.*?(v[\d.]+).*','$1'
        Write-Ok "Aurion CLI upgraded ($currentVer -> $newVer)"
        return
    }

    Install-CliBinary

    if (-not (Test-CommandExists "aurion")) {
        Write-Fail "CLI installed but 'aurion' not found on PATH. Restart your terminal and try again."
    }
}

# ---------------------------------------------------------------------------
# Docker check
# ---------------------------------------------------------------------------
function Test-Docker {
    if (-not (Test-CommandExists "docker")) {
        Write-Fail @"
Docker is not installed. Aurion self-hosting requires Docker and Docker Compose.

Install Docker Desktop for Windows:
  https://docs.docker.com/desktop/install/windows-install/

After installing Docker, re-run this script with `$env:AURION_MODE="local"`.
"@
    }

    try {
        docker info 2>$null | Out-Null
    } catch {
        Write-Fail "Docker is installed but not running. Please start Docker Desktop and re-run this script."
    }

    Write-Ok "Docker is available"
}

# ---------------------------------------------------------------------------
# Server setup (self-host / local)
# ---------------------------------------------------------------------------
function Install-Server {
    Write-Info "Setting up Aurion server..."

    if (Test-Path (Join-Path $InstallDir ".git")) {
        Write-Info "Updating existing installation at $InstallDir..."
        Write-Warn "Any local changes in $InstallDir will be overwritten."
        Push-Location $InstallDir
        git fetch origin main --depth 1 2>$null
        git reset --hard origin/main 2>$null
        Pop-Location
    } else {
        Write-Info "Cloning Aurion repository..."
        if (-not (Test-CommandExists "git")) {
            Write-Fail "Git is not installed. Please install git and re-run."
        }
        if (Test-Path $InstallDir) {
            Write-Warn "Removing incomplete installation at $InstallDir..."
            Remove-Item $InstallDir -Recurse -Force
        }
        $parentDir = Split-Path $InstallDir -Parent
        if (-not (Test-Path $parentDir)) {
            New-Item -ItemType Directory -Path $parentDir -Force | Out-Null
        }
        git clone --depth 1 $RepoUrl $InstallDir
    }

    Write-Ok "Repository ready at $InstallDir"

    Push-Location $InstallDir

    if (-not (Test-Path ".env")) {
        Write-Info "Creating .env with random JWT_SECRET..."
        Copy-Item ".env.example" ".env"
        $jwt = -join ((1..32) | ForEach-Object { "{0:x2}" -f (Get-Random -Maximum 256) })
        (Get-Content ".env") -replace '^JWT_SECRET=.*', "JWT_SECRET=$jwt" | Set-Content ".env"
        Write-Ok "Generated .env with random JWT_SECRET"
    } else {
        Write-Ok "Using existing .env"
    }

    Write-Info "Starting Aurion services (this may take a few minutes on first run)..."
    docker compose -f docker-compose.selfhost.yml up -d --build

    Write-Info "Waiting for backend to be ready..."
    $ready = $false
    for ($i = 1; $i -le 45; $i++) {
        try {
            $null = Invoke-WebRequest -Uri "http://localhost:8080/health" -UseBasicParsing -TimeoutSec 2
            $ready = $true
            break
        } catch {
            Start-Sleep -Seconds 2
        }
    }

    if ($ready) {
        Write-Ok "Aurion server is running"
    } else {
        Write-Warn "Server is still starting. Check logs with:"
        Write-Host "  cd $InstallDir; docker compose -f docker-compose.selfhost.yml logs"
    }

    Pop-Location
}


# ---------------------------------------------------------------------------
# Main: Default mode (cloud)
# ---------------------------------------------------------------------------
function Start-DefaultInstall {
    Write-Host ""
    Write-Host "  Aurion - Installer" -ForegroundColor White
    Write-Host ""

    Install-Cli

    Write-Host ""
    Write-Host "  ============================================" -ForegroundColor Green
    Write-Host "  [OK] Aurion CLI is ready!" -ForegroundColor Green
    Write-Host "  ============================================" -ForegroundColor Green
    Write-Host ""
    Write-Host "  Next: configure your environment"
    Write-Host ""
    Write-Host "     aurion setup               " -NoNewline; Write-Host "# Connect to Aurion Cloud (aurion.studio)" -ForegroundColor DarkGray
    Write-Host "     aurion setup self-host      " -NoNewline; Write-Host "# Connect to a self-hosted server" -ForegroundColor DarkGray
    Write-Host ""
    Write-Host "  Self-hosting? Install the server first:"
    Write-Host '     $env:AURION_MODE="with-server"; irm https://raw.githubusercontent.com/aurion-ai/aurion/main/scripts/install.ps1 | iex'
    Write-Host ""
}

# ---------------------------------------------------------------------------
# Main: Local mode (self-host)
# ---------------------------------------------------------------------------
function Start-LocalInstall {
    Write-Host ""
    Write-Host "  Aurion - Self-Host Installer" -ForegroundColor White
    Write-Host "  Provisioning server infrastructure + installing CLI"
    Write-Host ""

    Test-Docker
    Install-Server
    Install-Cli

    Write-Host ""
    Write-Host "  ============================================" -ForegroundColor Green
    Write-Host "  [OK] Aurion server is running and CLI is ready!" -ForegroundColor Green
    Write-Host "  ============================================" -ForegroundColor Green
    Write-Host ""
    Write-Host "  Frontend:  http://localhost:3000"
    Write-Host "  Backend:   http://localhost:8080"
    Write-Host "  Server at: $InstallDir"
    Write-Host ""
    Write-Host "  Next: configure your CLI to connect"
    Write-Host ""
    Write-Host "     aurion setup self-host  " -NoNewline; Write-Host "# Configure + authenticate + start daemon" -ForegroundColor DarkGray
    Write-Host ""
    Write-Host "  Default verification code: 888888"
    Write-Host ""
    Write-Host "  To stop all services:"
    Write-Host '     $env:AURION_MODE="stop"; irm https://raw.githubusercontent.com/aurion-ai/aurion/main/scripts/install.ps1 | iex'
    Write-Host ""
}

# ---------------------------------------------------------------------------
# Stop: shut down a self-hosted installation
# ---------------------------------------------------------------------------
function Start-Stop {
    Write-Host ""
    Write-Info "Stopping Aurion services..."

    if (Test-Path $InstallDir) {
        Push-Location $InstallDir
        if (Test-Path "docker-compose.selfhost.yml") {
            docker compose -f docker-compose.selfhost.yml down
            Write-Ok "Docker services stopped"
        } else {
            Write-Warn "No docker-compose.selfhost.yml found at $InstallDir"
        }
        Pop-Location
    } else {
        Write-Warn "No Aurion installation found at $InstallDir"
    }

    if (Test-CommandExists "aurion") {
        try {
            aurion daemon stop 2>$null
            Write-Ok "Daemon stopped"
        } catch {}
    }

    Write-Host ""
}

# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------
$mode = if ($env:AURION_MODE) { $env:AURION_MODE.ToLower() } else { "default" }

switch ($mode) {
    "with-server" { Start-LocalInstall }
    "local"       { Start-LocalInstall }  # backwards compat alias
    "stop"        { Start-Stop }
    default       { Start-DefaultInstall }
}
