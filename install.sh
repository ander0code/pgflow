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
sums_url="https://github.com/${REPO}/releases/latest/download/checksums.txt"

say ""
say "🐘  Instalando pgflow (${os}/${arch})..."

mkdir -p "$INSTALL_DIR"
tmp="$(mktemp)"
sums="$(mktemp)"
trap 'rm -f "$tmp" "$sums"' EXIT

verify_sha256() {
  # verify_sha256 <file> <sha256_file> <expected_asset>
  local file="$1" sums_file="$2" expected="$3"
  # Expected line looks like: "<sha>  asset"
  local line expected_sha
  line="$(grep -F "  ${expected}" "$sums_file" 2>/dev/null || true)"
  if [ -z "$line" ]; then
    say "❌  No encuentro la firma de ${expected} en checksums.txt"
    return 1
  fi
  expected_sha="$(printf '%s' "$line" | awk '{print $1}')"
  local actual_sha
  if command -v sha256sum >/dev/null 2>&1; then
    actual_sha="$(sha256sum "$file" | awk '{print $1}')"
  elif command -v shasum >/dev/null 2>&1; then
    actual_sha="$(shasum -a 256 "$file" | awk '{print $1}')"
  else
    say "⚠️   No tengo ni sha256sum ni shasum — saltando verificación."
    return 0
  fi
  if [ "$expected_sha" != "$actual_sha" ]; then
    say "❌  SHA256 no coincide para ${expected}"
    say "    esperado: $expected_sha"
    say "    actual:   $actual_sha"
    return 1
  fi
  return 0
}

if curl -fsSL "$url" -o "$tmp" && curl -fsSL "$sums_url" -o "$sums"; then
  if verify_sha256 "$tmp" "$sums" "$asset"; then
    install -m 0755 "$tmp" "$INSTALL_DIR/$TOOL"
    say "✅  Descargado, verificado e instalado → $INSTALL_DIR/$TOOL"
  else
    say "    No instalo un binario con firma incorrecta. Mira:"
    say "    https://github.com/${REPO}/releases/latest"
    exit 1
  fi
else
  say "⚠️   No se pudo descargar el release ($url)."
  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd || true)"
  if [ -n "$script_dir" ] && [ "${script_dir}" != "." ] && [ -f "${script_dir}/go.mod" ] && command -v go >/dev/null 2>&1; then
    say "🔨  Compilando desde el código (go build)..."
    (cd "$script_dir" && go build -ldflags="-s -w -X main.version=dev" -o "$INSTALL_DIR/$TOOL" .)
    say "✅  Compilado e instalado → $INSTALL_DIR/$TOOL"
  else
    say "    Para verificar el binario, clona el repo (donde sí hay checksums.txt)"
    say "    y corre:  sha256sum -c dist/checksums.txt"
    say "    O instala Go y vuelve a correr el instalador."
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
