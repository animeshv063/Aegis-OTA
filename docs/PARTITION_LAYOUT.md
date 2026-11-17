# Aegis Storage Partition Layout Specification

Target architecture for eMMC, NVMe, or raw NAND/NOR storage media:

| Partition | Label | Filesystem | Mount Point | Properties | Size |
|---|---|---|---|---|---|
| `/dev/mmcblk0p1` | `boot` | FAT32 / ext4 | `/boot` | Read-Only | 64 MB |
| `/dev/mmcblk0p2` | `rootfs_a` | squashfs / ext4 | `/` (when Slot A active) | Read-Only (Immutable) | 4 GB |
| `/dev/mmcblk0p3` | `rootfs_b` | squashfs / ext4 | `/` (when Slot B active) | Read-Only (Immutable) | 4 GB |
| `/dev/mmcblk0p4` | `data` | ext4 / f2fs / zfs | `/data` | Read-Write (Persistent) | Remainder |

## Notes on Immutability

1. `/etc`, `/var`, and `/home` directories in the rootfs use overlayfs or symbolic links to `/data/config` and `/data/var` so user configurations survive across system updates.
2. The root filesystem (`rootfs_a` / `rootfs_b`) can be formatted as `squashfs` with `dm-verity` for hardware-verified cryptographic integrity on block reads.
