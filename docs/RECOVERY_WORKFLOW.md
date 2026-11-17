# Failure Modes & Recovery Workflows

Aegis OTA is designed to handle fatal field events without bricking edge units.

## Scenario 1: Power Loss During Image Flashing
- **Condition**: Power is abruptly disconnected while streaming chunk 340MB of 800MB to Slot B.
- **State**: Slot A remains completely untouched and active in U-Boot environment (`active_slot=a`, `upgrade_available=0`).
- **Recovery**: When power is restored, the board boots directly into Slot A as normal. The agent detects partial data in Slot B and safely restarts the OTA process on the next cycle.

## Scenario 2: Tampered or Corrupted Image
- **Condition**: Network transmission flips bits, or download mirror is compromised with malicious binaries.
- **State**: The manifest's Ed25519 signature verification fails, or the on-the-fly computed SHA-256 does not match the manifest checksum.
- **Action**: The flasher immediately halts, logs an integrity violation error, and aborts before U-Boot environment flags are modified. The device remains stably booted in Slot A.

## Scenario 3: Kernel Panic on Upgraded Slot
- **Condition**: Image is cryptographically valid, but the new kernel crashes or panics during driver probe.
- **State**: Device fails to reach user space; software health checker cannot run.
- **Recovery**:
  1. U-Boot armed the hardware watchdog before booting into Slot B.
  2. The hardware watchdog counter expires (60 seconds) without receiving a pulse from user space.
  3. The hardware resets the board.
  4. U-Boot increments `bootcount`.
  5. Upon reaching `bootlimit` (3 attempts), U-Boot invokes `altbootcmd`, automatically resets `active_slot=a`, and boots back into the last known-good slot.

## Scenario 4: Broken Storage Pool / Network Regression
- **Condition**: The new OS boots, but the network driver fails to establish IP routing, or the NAS ZFS storage pool fails to import.
- **State**: `aegis-healthcheck.service` executes post-boot tests.
- **Action**: Health check flags failure. The agent refuses to commit the slot, invokes `aegis-agent rollback`, sets U-Boot `active_slot` back to the fallback slot, and triggers a clean reboot.
