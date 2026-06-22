# ──────────────────────────────────────────────────────────────────────────────
# pgflow — Windows installer (PowerShell)
#
# Descarga el binario prebuilt desde GitHub Releases (NO necesitas Go).
# Si lo corres dentro de un clon del repo y falla la descarga, compila del código.
#
# Uso (one-liner):
#   irm https://raw.githubusercontent.com/ander0code/pgflow/main/install.ps1 | iex
#
# Uso local:
#   .\install.ps1
#   $env:PGFLOW_INSTALL_DIR = "C:\Tools"; .\install.ps1
# ──────────────────────────────────────────────────────────────────────────────
[CmdletBinding()]
param([string]$InstallDir = $env:PGFLOW_INSTALL_DIR)

$ErrorActionPreference = "Stop"
$Repo = "ander0code/pgflow"
$ToolName = "pgflow"
if (-not $InstallDir) { $InstallDir = Join-Path $env:LOCALAPPDATA "pgflow" }

Write-Host ""
Write-Host "🐘  Instalando pgflow (Windows)..."

# ── detectar arquitectura ───────────────────────────────────────────────────────
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
$asset = "$ToolName-windows-$arch.exe"
$url = "https://github.com/$Repo/releases/latest/download/$asset"

New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
$dest = Join-Path $InstallDir "$ToolName.exe"

# ── descargar binario prebuilt ───────────────────────────────────────────────────
$downloaded = $false
try {
    Invoke-WebRequest -Uri $url -OutFile $dest -UseBasicParsing
    $downloaded = $true
    Write-Host ("✅  Descargado e instalado → " + $dest) -ForegroundColor Green
} catch {
    Write-Host ("⚠️   No se pudo descargar (" + $url + ").") -ForegroundColor Yellow
}

# ── fallback: compilar del código si hay Go y estamos en un clon ──────────────────
if (-not $downloaded) {
    $go = Get-Command go -ErrorAction SilentlyContinue
    if ($go -and $PSScriptRoot -and (Test-Path (Join-Path $PSScriptRoot "go.mod"))) {
        Write-Host "🔨  Compilando desde el código (go build)..." -ForegroundColor Cyan
        Push-Location $PSScriptRoot
        try { go build -ldflags="-s -w" -o $dest . } finally { Pop-Location }
        Write-Host ("✅  Compilado e instalado → " + $dest) -ForegroundColor Green
    } else {
        Write-Host "    Revisa que exista un release: https://github.com/$Repo/releases" -ForegroundColor Yellow
        exit 1
    }
}

# ── deps de runtime (solo aviso) ──────────────────────────────────────────────────
$missing = @()
foreach ($dep in @("psql", "pg_dump", "pg_restore", "ssh")) {
    if (-not (Get-Command $dep -ErrorAction SilentlyContinue)) { $missing += $dep }
}
if ($missing.Count -gt 0) {
    Write-Host ("⚠️   Faltan herramientas en runtime: " + ($missing -join ", ")) -ForegroundColor Yellow
    Write-Host "    PostgreSQL client: https://www.postgresql.org/download/windows/"
    Write-Host "    OpenSSH client:    Add-WindowsCapability -Online -Name OpenSSH.Client~~~~0.0.1.0"
}

# ── PATH ──────────────────────────────────────────────────────────────────────────
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$InstallDir*") {
    Write-Host ""
    Write-Host "⚠️  $InstallDir no está en tu PATH (usuario). Para añadirlo:" -ForegroundColor Yellow
    Write-Host "      [Environment]::SetEnvironmentVariable('Path',`"$InstallDir;`" + [Environment]::GetEnvironmentVariable('Path','User'),'User')"
    Write-Host "    Luego abre una NUEVA ventana de PowerShell."
}

Write-Host ""
Write-Host "🚀  Listo. Ejecuta:  pgflow      (ayuda: pgflow --help)" -ForegroundColor Green
