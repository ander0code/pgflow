# ──────────────────────────────────────────────────────────────────────────────
# pgflow — Windows installer (PowerShell)
# Builds the Go binary and drops it on your PATH.
#
# Uso desde PowerShell:
#   .\install.ps1                         # instala en $env:LOCALAPPDATA\pgflow
#   $env:PGFLOW_INSTALL_DIR = "C:\Tools"; .\install.ps1
# ──────────────────────────────────────────────────────────────────────────────
[CmdletBinding()]
param(
    [string]$InstallDir = $env:PGFLOW_INSTALL_DIR
)

$ErrorActionPreference = "Stop"

if (-not $InstallDir) {
    $InstallDir = Join-Path $env:LOCALAPPDATA "pgflow"
}

$ToolName = "pgflow"
$RepoDir = $PSScriptRoot

Write-Host ""
Write-Host "🐘  Installing pgflow (Windows)..."
Write-Host ""

# ── Go check ──────────────────────────────────────────────────────────────────
$go = Get-Command go -ErrorAction SilentlyContinue
if (-not $go) {
    Write-Host "❌  Go is not installed." -ForegroundColor Red
    Write-Host "    Install it: https://go.dev/dl/"
    exit 1
}
Write-Host ("✅  Go found: " + (go version)) -ForegroundColor Green

# ── Runtime deps (warn only) ──────────────────────────────────────────────────
$missing = @()
foreach ($dep in @("psql", "pg_dump", "pg_restore", "ssh")) {
    if (-not (Get-Command $dep -ErrorAction SilentlyContinue)) {
        $missing += $dep
    }
}
if ($missing.Count -gt 0) {
    Write-Host ("⚠️   Missing runtime tools: " + ($missing -join ", ")) -ForegroundColor Yellow
    Write-Host "    Install PostgreSQL client: https://www.postgresql.org/download/windows/"
    Write-Host "    Install OpenSSH client (Win10 1809+):"
    Write-Host "      Add-WindowsCapability -Online -Name OpenSSH.Client~~~~0.0.1.0"
}

# ── Build ─────────────────────────────────────────────────────────────────────
Write-Host "🔨  Building binary..." -ForegroundColor Cyan
$binPath = Join-Path $RepoDir ($ToolName + ".exe")
Push-Location $RepoDir
try {
    go build -ldflags="-s -w" -o $binPath .
} finally {
    Pop-Location
}
Write-Host ("✅  Built → " + $binPath) -ForegroundColor Green

# ── Install ───────────────────────────────────────────────────────────────────
if (-not (Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
}
Copy-Item -Path $binPath -Destination (Join-Path $InstallDir ($ToolName + ".exe")) -Force
Write-Host ("✅  Installed → " + (Join-Path $InstallDir ($ToolName + ".exe"))) -ForegroundColor Green

# ── PATH hint ─────────────────────────────────────────────────────────────────
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$InstallDir*") {
    Write-Host ""
    Write-Host "⚠️  $InstallDir is not in your PATH." -ForegroundColor Yellow
    Write-Host "    Run this in PowerShell to add it (current user only):"
    Write-Host ""
    Write-Host "      [Environment]::SetEnvironmentVariable('Path',`"$InstallDir;`" + [Environment]::GetEnvironmentVariable('Path','User'),'User')"
    Write-Host ""
    Write-Host "    Then open a NEW PowerShell window."
}

Write-Host ""
Write-Host "🚀  Done! Run it:" -ForegroundColor Green
Write-Host ""
Write-Host "    pgflow"
Write-Host "    pgflow --list"
Write-Host "    pgflow --list --json"
Write-Host ""
