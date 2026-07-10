#!/usr/bin/env bash
# fabric/install.sh — install the fabric CLI on macOS/Linux
#
# Usage:
#   bash install.sh           # install everything
#   bash install.sh --check   # dry-run: report what is missing

set -euo pipefail

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BOLD='\033[1m'; NC='\033[0m'
ok()   { echo -e "${GREEN}✓${NC} $*"; }
warn() { echo -e "${YELLOW}!${NC} $*"; }
fail() { echo -e "${RED}✗${NC} $*"; }
step() { echo -e "\n${BOLD}▶ $*${NC}"; }

CHECK_ONLY=false
[[ "${1:-}" == "--check" ]] && CHECK_ONLY=true

FABRIC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR="$HOME/.local/bin"

is_mac() { [[ "$(uname -s)" == "Darwin" ]]; }
has() { command -v "$1" &>/dev/null; }

require_brew() {
  if ! has brew; then
    fail "homebrew is required but not installed."
    echo "  Install it from https://brew.sh then re-run this script."
    exit 1
  fi
}

echo -e "${BOLD}fabric — installer${NC}"
echo "Fabric dir: $FABRIC_DIR"
echo "Install to: $INSTALL_DIR"
$CHECK_ONLY && echo -e "${YELLOW}(dry-run mode — nothing will be installed)${NC}"

# ── 1. Go ────────────────────────────────────────────────────────────
step "Go toolchain"

if has go; then
  GO_VER="$(go version | awk '{print $3}')"
  ok "go already installed ($GO_VER)"
else
  if $CHECK_ONLY; then
    fail "go not found"
  else
    if is_mac; then
      require_brew
      step "Installing Go via homebrew"
      brew install go
    else
      fail "Go is not installed. Install it from https://go.dev/dl/ and re-run."
      exit 1
    fi
  fi
fi

# ── 2. Claude Code CLI ───────────────────────────────────────────────
step "Claude Code CLI"

if has claude; then
  ok "claude already installed ($(claude --version 2>/dev/null | head -1))"
else
  warn "claude CLI not found — needed for the claude-code LLM backend."
  echo "  Install Claude Code: https://claude.ai/download"
  echo "  Once installed, re-run this script."
  $CHECK_ONLY || true
fi

# ── 3. Build the fabric binary ──────────────────────────────────────
step "Building fabric binary"

if $CHECK_ONLY; then
  ok "build step skipped (--check mode)"
else
  if ! has go; then
    fail "go is still not in PATH; cannot build."
    exit 1
  fi

  cd "$FABRIC_DIR"
  echo "  go mod tidy..."
  go mod tidy
  echo "  go build..."
  go build -o fabric ./cmd/fabric
  ok "binary built at $FABRIC_DIR/fabric"
fi

# ── 4. Install to PATH ───────────────────────────────────────────────
step "Installing to $INSTALL_DIR"

if $CHECK_ONLY; then
  if [[ -L "$INSTALL_DIR/fabric" ]] || [[ -f "$INSTALL_DIR/fabric" ]]; then
    ok "fabric already in $INSTALL_DIR"
  else
    fail "fabric not found in $INSTALL_DIR"
  fi
else
  mkdir -p "$INSTALL_DIR"
  ln -sf "$FABRIC_DIR/fabric" "$INSTALL_DIR/fabric"
  ok "fabric symlinked to $INSTALL_DIR/fabric"

  if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
    warn "$INSTALL_DIR is not in your PATH."
    echo ""
    echo "  Add to your shell config (~/.zshrc or ~/.bashrc):"
    echo "    export PATH=\"\$HOME/.local/bin:\$PATH\""
    echo ""
    echo "  Or run now for this session:"
    echo "    export PATH=\"\$HOME/.local/bin:\$PATH\""
  fi
fi

# ── 5. Symlink skills for Claude Code ────────────────────────────────
step "Claude Code skills"

if $CHECK_ONLY; then
  ok "skills check skipped (--check mode)"
else
  mkdir -p "$HOME/.claude/skills"
  for skill_dir in "$FABRIC_DIR/skills/"*; do
    [[ -d "$skill_dir" ]] || continue
    skill_name="$(basename "$skill_dir")"
    ln -sfn "$skill_dir" "$HOME/.claude/skills/$skill_name"
  done
  ok "skills symlinked to ~/.claude/skills/"
fi

# ── done ─────────────────────────────────────────────────────────────
echo ""
echo -e "${GREEN}${BOLD}All done.${NC}"
if ! $CHECK_ONLY; then
  echo ""
  echo "Quick start:"
  echo "  cd /path/to/your/project"
  echo "  fabric init              # creates fabric.yaml + knowledge base"
  echo "  fabric plan \"your task\"  # generate a plan"
fi
