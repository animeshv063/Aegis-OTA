#!/usr/bin/env bash
# Aegis OTA Simulation Harness
# Creates virtual loop devices to simulate embedded A/B partition hardware
set -e

WORKDIR="/tmp/aegis_sim"
echo "=== Setting up Aegis OTA Virtual Storage Harness ==="
mkdir -p "${WORKDIR}"

# Create 500MB disk image
IMAGE_FILE="${WORKDIR}/fleet_disk.raw"
if [ ! -f "${IMAGE_FILE}" ]; then
    echo "[INFO] Creating 500MB virtual storage disk..."
    dd if=/dev/zero of="${IMAGE_FILE}" bs=1M count=500 status=none
fi

# Associate loop device
LOOP_DEV=$(sudo losetup --find --show --partscan "${IMAGE_FILE}")
echo "[INFO] Disk mapped to ${LOOP_DEV}"

# Partition:
# p1: Boot (64MB)
# p2: Rootfs Slot A (200MB)
# p3: Rootfs Slot B (200MB)
# p4: Persistent Data (36MB)
echo "[INFO] Partitioning virtual disk..."
sudo parted -s "${LOOP_DEV}" \
    mklabel gpt \
    mkpart boot ext4 1MiB 65MiB \
    mkpart slot_a ext4 65MiB 265MiB \
    mkpart slot_b ext4 265MiB 465MiB \
    mkpart data ext4 465MiB 500MiB

echo "Virtual partitions ready:"
lsblk "${LOOP_DEV}"

echo "Export environment variables to test aegis-agent:"
echo "export AEGIS_BOOT_DEV=${LOOP_DEV}p1"
echo "export AEGIS_SLOT_A_DEV=${LOOP_DEV}p2"
echo "export AEGIS_SLOT_B_DEV=${LOOP_DEV}p3"
echo "export AEGIS_DATA_DEV=${LOOP_DEV}p4"
echo "export AEGIS_MOCK_UBOOT=1"
