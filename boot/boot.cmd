# U-Boot boot script for Aegis OTA Dual-Slot Fail-Safe Booting
# Compile with: mkimage -C none -A arm64 -T script -d boot.cmd boot.scr

echo "=== Aegis OTA Bootloader Initializing ==="

# Check if upgrade testing is active
if test "${upgrade_available}" = "1"; then
    echo "[AEGIS] Upgrade pending validation. Current boot attempt: ${bootcount} / ${bootlimit}"
    
    # Arm hardware watchdog (60 second timeout for bootloader -> kernel -> healthcheck transition)
    watchdog 60
    
    # Increment bootcount
    setexpr bootcount ${bootcount} + 1
    saveenv
    
    # Check if threshold reached
    if test ${bootcount} -gt ${bootlimit}; then
        echo "[CRITICAL] Boot limit exceeded! Triggering emergency rollback."
        run altbootcmd
    fi
fi

# Boot selected slot
if test "${active_slot}" = "b"; then
    echo "[AEGIS] Booting Slot B..."
    run bootcmd_slot_b
else
    echo "[AEGIS] Booting Slot A..."
    run bootcmd_slot_a
fi

# If execution reaches here, boot failed
echo "[FATAL] Boot execution failed! Triggering altbootcmd fallback."
run altbootcmd
