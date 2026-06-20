#!/usr/bin/env bash
# ──────────────────────────────────────────────────────────────────────────────
# pgflow — installer
# Builds the Go binary and drops it on your PATH.
# ──────────────────────────────────────────────────────────────────────────────
set -euo pipefail

INSTALL_DIR="${PGFLOW_INSTALL_DIR:-$HOME/.local/bin}"
TOOL_NAME="pgflow"
REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo ""
echo "🐘  Installing pgflow..."
echo ""

# ── Go check ──────────────────────────────────────────────────────────────────
if ! command -v go &>/dev/null; then
  echo "❌  Go is not installed."
  echo "    Install it: https://go.dev/dl/"
  echo "    macOS:      brew install go"
  exit 1
fi
echo "✅  Go found: $(go version | awk '{print $3}')"

# ── Runtime deps (warn only) ────────────────────────────────────────────────────
missing=()
for dep in psql pg_dump pg_restore ssh; do
  command -v "$dep" &>/dev/null || missing+=("$dep")
done
if [ ${#missing[@]} -gt 0 ]; then
  echo "⚠️   Missing runtime tools: ${missing[*]}"
  echo "    macOS:  brew install postgresql"
  echo "    Debian: sudo apt install postgresql-client openssh-client"
fi

# ── Build ─────────────────────────────────────────────────────────────────────
echo "🔨  Building binary..."
(cd "$REPO_DIR" && go build -ldflags="-s -w" -o "$REPO_DIR/$TOOL_NAME" .)
echo "✅  Built → $REPO_DIR/$TOOL_NAME"

# ── Install ───────────────────────────────────────────────────────────────────
mkdir -p "$INSTALL_DIR"
cp "$REPO_DIR/$TOOL_NAME" "$INSTALL_DIR/$TOOL_NAME"
chmod +x "$INSTALL_DIR/$TOOL_NAME"
echo "✅  Installed → $INSTALL_DIR/$TOOL_NAME"

# ── PATH hint ─────────────────────────────────────────────────────────────────
if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
  SHELL_NAME=$(basename "${SHELL:-bash}")
  RC="$HOME/.${SHELL_NAME}rc"
  echo ""
  echo "⚠️   $INSTALL_DIR is not in your PATH."
  echo "    Add this line to $RC:"
  echo ""
  echo '    export PATH="$HOME/.local/bin:$PATH"'
  echo ""
  echo "    Then run:  source $RC"
fi

echo ""
echo "🚀  Done! Run it:"
echo ""
echo "    pgflow"
echo "    pgflow --list"
echo "    pgflow --list --json"
echo ""
