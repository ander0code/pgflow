# ──────────────────────────────────────────────────────────────────────────────
# pgflow — Windows uninstaller (PowerShell)
# Removes the binary installed by install.ps1.
#
# Uso:
#   .\uninstall.ps1
#   $env:PGFLOW_INSTALL_DIR = "C:\Tools"; .\uninstall.ps1
# ──────────────────────────────────────────────────────────────────────────────
[CmdletBinding()]
param(
    [string]$InstallDir = $env:PGFLOW_INSTALL_DIR
)

$ErrorActionPreference = "Stop"

if (-not $InstallDir) {
    $InstallDir = Join-Path $env:LOCALAPPDATA "pgflow"
}

$target = Join-Path $InstallDir "pgflow.exe"
if (Test-Path $target) {
    Remove-Item -Path $target -Force
    Write-Host ("🗑  removed " + $target) -ForegroundColor Yellow
} else {
    Write-Host ("(nothing to remove at " + $target + ")")
}
