#!/usr/bin/env bash
# Developer Environment Setup for Aegis OTA
set -e

echo "Setting up Aegis OTA development dependencies..."

# Check Python and Go
if ! command -v python3 >/dev/null 2>&1; then
    echo "[WARN] Python3 is not installed."
fi

if ! command -v go >/dev/null 2>&1; then
    echo "[WARN] Go is not installed. Recommended version: Go 1.22+"
fi

echo "Installing recommended Python crypto libraries..."
pip install cryptography || true

echo "Setup complete. You can build the edge agent with: cd agent && go build ./cmd/aegis-agent"
