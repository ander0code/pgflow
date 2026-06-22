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

# ── descargar binario prebuilt + checksums.txt, verificar SHA256 ──────────────
$ReleaseBase = "https://github.com/$Repo/releases/latest/download"
$ChecksumsUrl = "$ReleaseBase/checksums.txt"
$TmpBin = Join-Path $env:TEMP "pgflow-install.exe"
$TmpChecksums = Join-Path $env:TEMP "pgflow-checksums.txt"

$downloaded = $false
$downloadError = $null
try {
    Invoke-WebRequest -Uri $url -OutFile $TmpBin -UseBasicParsing
    try {
        Invoke-WebRequest -Uri $ChecksumsUrl -OutFile $TmpChecksums -UseBasicParsing
    } catch { $ChecksumsUrl = $null } # release may exist without checksums yet
    $line = if ($ChecksumsUrl) { Select-String -Path $TmpChecksums -Pattern "  $asset$" | Select-Object -First 1 } else { $null }
    if ($line) {
        $expected = ($line -split '\s+')[0].ToLower()
        $actual = (Get-FileHash -Algorithm SHA256 -Path $TmpBin).Hash.ToLower()
        if ($actual -eq $expected) {
            Write-Host "✅  SHA256 verificado" -ForegroundColor Green
            Move-Item -Path $TmpBin -Destination $dest -Force
            $downloaded = $true
            Write-Host ("✅  Descargado e instalado → " + $dest) -ForegroundColor Green
        } else {
            Write-Host "❌  SHA256 no coincide (esperado $expected, obtuve $actual)." -ForegroundColor Red
            Write-Host "    La descarga puede estar comprometida. Mira: $ReleaseBase"
            Remove-Item -Path $TmpBin -ErrorAction SilentlyContinue
            exit 1
        }
    } elseif ($ChecksumsUrl) {
        Write-Host "⚠️  checksums.txt no incluye $asset — instalo sin verificación." -ForegroundColor Yellow
        Move-Item -Path $TmpBin -Destination $dest -Force
        $downloaded = $true
        Write-Host ("✅  Descargado e instalado → " + $dest) -ForegroundColor Green
    } else {
        Write-Host "⚠️  No hay checksums.txt en el release — instalo sin verificación." -ForegroundColor Yellow
        Move-Item -Path $TmpBin -Destination $dest -Force
        $downloaded = $true
        Write-Host ("✅  Descargado e instalado → " + $dest) -ForegroundColor Green
    }
} catch {
    $downloadError = $_
    Write-Host ("⚠️   No se pudo descargar (" + $url + "): " + $_.Exception.Message) -ForegroundColor Yellow
    Remove-Item -Path $TmpBin -ErrorAction SilentlyContinue
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
        Write-Host "    Para verificar el binario, clona el repo y corre:"
        Write-Host "      Get-FileHash dist\pgflow-windows-amd64.exe -Algorithm SHA256"
        Write-Host "    y compara con https://github.com/$Repo/releases/latest/download/checksums.txt"
        Write-Host "    O instala Go y reintenta el instalador."
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
