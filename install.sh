#!/usr/bin/env bash
# ──────────────────────────────────────────────────────────────────────────────
# pgflow — installer (macOS / Linux)
#
# Descarga el binario prebuilt desde GitHub Releases (NO necesitas Go).
# Si lo corres dentro de un clon del repo y falla la descarga, compila del código.
#
#   curl -fsSL https://raw.githubusercontent.com/ander0code/pgflow/main/install.sh | bash
#
# Variables:
#   PGFLOW_INSTALL_DIR   destino (default: ~/.local/bin)
# ──────────────────────────────────────────────────────────────────────────────
set -euo pipefail

REPO="ander0code/pgflow"
TOOL="pgflow"
INSTALL_DIR="${PGFLOW_INSTALL_DIR:-$HOME/.local/bin}"

say() { printf '%s\n' "$*"; }

# ── detectar plataforma ─────────────────────────────────────────────────────────
os="$(uname -s)"; arch="$(uname -m)"
case "$os" in
  Darwin) os="darwin" ;;
  Linux)  os="linux" ;;
  *) say "❌  SO no soportado por este script: $os"
     say "    (¿Windows? usa install.ps1 desde PowerShell)"; exit 1 ;;
esac
case "$arch" in
  x86_64|amd64)  arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) say "❌  arquitectura no soportada: $arch"; exit 1 ;;
esac

asset="${TOOL}-${os}-${arch}"
url="https://github.com/${REPO}/releases/latest/download/${asset}"

say ""
say "🐘  Instalando pgflow (${os}/${arch})..."

mkdir -p "$INSTALL_DIR"
tmp="$(mktemp)"

if curl -fsSL "$url" -o "$tmp"; then
  install -m 0755 "$tmp" "$INSTALL_DIR/$TOOL"
  rm -f "$tmp"
  say "✅  Descargado e instalado → $INSTALL_DIR/$TOOL"
else
  rm -f "$tmp"
  say "⚠️   No se pudo descargar ($url)."
  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd || true)"
  if [ -n "$script_dir" ] && [ -f "${script_dir}/go.mod" ] && command -v go >/dev/null 2>&1; then
    say "🔨  Compilando desde el código (go build)..."
    (cd "$script_dir" && go build -ldflags="-s -w" -o "$INSTALL_DIR/$TOOL" .)
    say "✅  Compilado e instalado → $INSTALL_DIR/$TOOL"
  else
    say "    Revisa que exista un release: https://github.com/${REPO}/releases"
    say "    o clona el repo y corre 'make install' (necesita Go)."
    exit 1
  fi
fi

# ── deps de runtime (solo aviso) ────────────────────────────────────────────────
missing=()
for dep in psql pg_dump pg_restore ssh; do
  command -v "$dep" >/dev/null 2>&1 || missing+=("$dep")
done
if [ ${#missing[@]} -gt 0 ]; then
  say ""
  say "⚠️   Faltan herramientas en runtime: ${missing[*]}"
  say "    macOS:  brew install postgresql"
  say "    Debian: sudo apt install postgresql-client openssh-client"
fi

# ── PATH ────────────────────────────────────────────────────────────────────────
case ":$PATH:" in
  *":$INSTALL_DIR:"*) : ;;
  *)
    rc="$HOME/.$(basename "${SHELL:-bash}")rc"
    say ""
    say "⚠️   $INSTALL_DIR no está en tu PATH. Añade a $rc:"
    say '       export PATH="$HOME/.local/bin:$PATH"'
    say "    y luego:  source $rc"
    ;;
esac

say ""
say "🚀  Listo. Ejecuta:  pgflow      (ayuda: pgflow --help)"
