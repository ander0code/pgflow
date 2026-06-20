#!/usr/bin/env bash
set -euo pipefail
INSTALL_DIR="${PGFLOW_INSTALL_DIR:-$HOME/.local/bin}"
rm -f "$INSTALL_DIR/pgflow"
echo "🗑  removed $INSTALL_DIR/pgflow"
