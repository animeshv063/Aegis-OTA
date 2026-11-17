# Deployment & Validation Tools

This directory contains test harnesses, scripts, and utilities for validating the Aegis OTA deployment pipeline:

- `setup_dev_env.sh`: Verifies local dependencies (Go 1.22+, Python 3, Ed25519 cryptography).
- `simulate_qemu.sh`: Sets up a virtual disk with loop devices to simulate real eMMC / NVMe dual-slot embedded storage without requiring physical hardware.
- `test_ota_pipeline.sh`: Runs end-to-end unit and integration tests for manifest verification, streaming flash, U-Boot flags, and post-boot health checking.
