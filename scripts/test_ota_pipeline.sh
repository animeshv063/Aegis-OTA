#!/usr/bin/env bash
# End-to-End Aegis OTA Integration Test
set -e

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
ROOT_DIR="$(dirname "$DIR")"
TMP_DIR="/tmp/aegis_e2e"

echo "=== Aegis OTA End-to-End Test Suite ==="
mkdir -p "${TMP_DIR}"

export AEGIS_MOCK_UBOOT=1
export AEGIS_UBOOT_ENV_FILE="${TMP_DIR}/uboot.env"
export AEGIS_ACTIVE_SLOT="a"
export AEGIS_SLOT_A_DEV="${TMP_DIR}/slot_a.bin"
export AEGIS_SLOT_B_DEV="${TMP_DIR}/slot_b.bin"
export AEGIS_DATA_PATH="${TMP_DIR}/data"

mkdir -p "${AEGIS_DATA_PATH}"
touch "${AEGIS_SLOT_A_DEV}" "${AEGIS_SLOT_B_DEV}"

echo "[Step 1] Build or prepare agent binary..."
cd "${ROOT_DIR}/agent"
if command -v go >/dev/null 2>&1; then
    go build -o "${TMP_DIR}/aegis-agent" ./cmd/aegis-agent
else
    echo "Go not installed; skipping binary compilation test."
    exit 0
fi

echo "[Step 2] Check agent status..."
"${TMP_DIR}/aegis-agent" status

echo "[Step 3] Run post-boot healthcheck..."
"${TMP_DIR}/aegis-agent" healthcheck

echo "[Step 4] Test emergency rollback..."
"${TMP_DIR}/aegis-agent" rollback

echo "[SUCCESS] All local integration tests completed successfully."
