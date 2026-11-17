# Aegis OTA: System Architecture & Threat Model

Aegis OTA is an enterprise-grade, fault-tolerant Over-The-Air deployment framework engineered for physical hardware fleets and edge NAS nodes running specialized storage software in hostile or volatile operating environments.

```
       +-------------------------------------------------------+
       |                  CI / Build Server                    |
       |  1. Build Immutable Rootfs (Yocto / Buildroot)        |
       |  2. Compute SHA-256 Checksum                          |
       |  3. Sign Manifest with Ed25519 Private Key            |
       +-------------------------------------------------------+
                                   |
                         (HTTPS / S3 / CDN)
                                   v
+---------------------------------------------------------------------+
|                      Edge Node (Aegis Agent)                        |
|                                                                     |
|  [Step 1] Verify Manifest Signature with Embedded Public Key        |
|  [Step 2] Stream directly to Inactive Block Device (Slot B)         |
|  [Step 3] Flush kernel storage buffers (fsync)                      |
|  [Step 4] Validate on-the-fly computed SHA-256                      |
|  [Step 5] Configure U-Boot env: active_slot=b, upgrade_available=1  |
|  [Step 6] Armed Hardware Watchdog -> System Reboot                  |
+---------------------------------------------------------------------+
                                   |
                                (Reboot)
                                   v
+---------------------------------------------------------------------+
|                     Bootloader (U-Boot Logic)                       |
|                                                                     |
|            +--------------------------------------------+           |
|            | Increment bootcount. Is bootcount > limit? |           |
|            +--------------------------------------------+           |
|                 /                                   \               |
|            (YES)                                     (NO)           |
|              v                                         v            |
|     +------------------+                     +------------------+   |
|     | Rollback to      |                     | Boot into Slot B |   |
|     | Fallback Slot A  |                     +------------------+   |
|     +------------------+                               |            |
|                                                        v            |
|                                              +-------------------+  |
|                                              | Post-Boot Health  |  |
|                                              | Checks (systemd)  |  |
|                                              +-------------------+  |
|                                                  /           \      |
|                                              (PASS)         (FAIL)  |
|                                                v               v    |
|                                         +------------+   +---------+|
|                                         | Commit Slot|   | Rollback||
|                                         | in U-Boot  |   | & Reboot||
|                                         +------------+   +---------+|
+---------------------------------------------------------------------+
```

## Core Tenets

### 1. Zero-Trust Payload Verification
- Cryptographic verification is completely detached from network transport (TLS).
- Even in scenarios with compromised CDN edge nodes or DNS hijacking, unsigned or tampered OS payloads are immediately rejected before writing.
- Signatures utilize **Ed25519** (Edwards-curve Digital Signature Algorithm), offering high performance and high resistance to side-channel attacks.

### 2. O(1) Memory Footprint Streaming
- Edge devices often feature limited RAM (512MB - 2GB) that cannot hold multi-gigabyte OS root filesystems.
- The `flasher` module streams chunks directly to the raw block device (`/dev/mmcblk0pX`) while hashing on the fly.
- RAM usage remains constant (<32MB) regardless of image size.

### 3. Dual-Bank A/B Partitioning
- Physical storage is divided into non-overlapping root partitions: `slot_a` and `slot_b`.
- The active partition is mounted strictly read-only (`ro`).
- Persistent application state and NAS configuration live on a dedicated `data` partition (`/dev/mmcblk0p4`).

### 4. Bootloader-Level Fail-Safe
- Software watchdog daemons running in user space cannot save a machine suffering from an early kernel panic or defective device tree blob.
- Rollback logic is placed in U-Boot with hardware watchdog timers. If the new slot fails to boot 3 times, the hardware rolls back to the known-good partition without operator intervention.
