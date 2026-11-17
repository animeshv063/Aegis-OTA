# Bootloader Configuration & Automatic Rollback

This directory contains U-Boot environment scripts and systemd units for bootloader-level recovery.

## Why Application-Layer Rollback Fails in Embedded Fleets

In traditional cloud deployments, rollback scripts run in user space. In physical edge hardware, this is fundamentally flawed:
1. **Kernel Panic**: If an upgraded kernel panics during early initialization, user space is never reached.
2. **Driver Hang**: If storage or filesystem drivers fail to mount the root volume, user space scripts cannot execute.
3. **Power Loss**: If power is cut midway through booting, software timers are lost.

## Aegis Bootloader-Level Fail-Safe Model

Aegis solves this with a 3-layer safety net:
1. **Hardware Watchdog**: Armed by U-Boot prior to jumping into the Linux kernel (`booti`). If the kernel panics or deadlocks, the hardware watchdog reboots the machine.
2. **Non-Volatile Bootcounter (`bootcount`)**: Stored in SPI flash / EEPROM / eMMC boot partition. Incremented by U-Boot before loading the kernel.
3. **Automatic Fallback (`altbootcmd`)**: If `bootcount > bootlimit` (default: 3 attempts), U-Boot automatically resets `active_slot` to `fallback_slot` without requiring network or user space intervention.
