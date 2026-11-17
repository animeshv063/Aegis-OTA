package partition

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// Slot identifies an A/B root partition slot.
type Slot string

const (
	SlotA Slot = "a"
	SlotB Slot = "b"
	SlotUnknown Slot = "unknown"
)

// PartitionLayout defines device paths for A/B partitions and persistent storage.
type PartitionLayout struct {
	BootDevice   string `json:"boot_device"`    // e.g. /dev/mmcblk0p1 (U-Boot & kernel)
	SlotAPath    string `json:"slot_a_path"`    // e.g. /dev/mmcblk0p2 (Rootfs Slot A)
	SlotBPath    string `json:"slot_b_path"`    // e.g. /dev/mmcblk0p3 (Rootfs Slot B)
	DataPath     string `json:"data_path"`      // e.g. /dev/mmcblk0p4 (Persistent data/NAS config)
}

// DefaultLayout returns standard embedded partition mapping.
func DefaultLayout() PartitionLayout {
	// Can be overridden via environment variables for emulation and hardware targets
	slotA := os.Getenv("AEGIS_SLOT_A_DEV")
	if slotA == "" {
		slotA = "/dev/mmcblk0p2"
	}

	slotB := os.Getenv("AEGIS_SLOT_B_DEV")
	if slotB == "" {
		slotB = "/dev/mmcblk0p3"
	}

	data := os.Getenv("AEGIS_DATA_DEV")
	if data == "" {
		data = "/dev/mmcblk0p4"
	}

	boot := os.Getenv("AEGIS_BOOT_DEV")
	if boot == "" {
		boot = "/dev/mmcblk0p1"
	}

	return PartitionLayout{
		BootDevice: boot,
		SlotAPath:  slotA,
		SlotBPath:  slotB,
		DataPath:   data,
	}
}

// DetectActiveSlot reads the Linux kernel commandline (/proc/cmdline) to identify current slot.
func DetectActiveSlot() (Slot, error) {
	// Check environment override (useful for development & unit testing)
	if envSlot := os.Getenv("AEGIS_ACTIVE_SLOT"); envSlot != "" {
		switch strings.ToLower(envSlot) {
		case "a":
			return SlotA, nil
		case "b":
			return SlotB, nil
		}
	}

	data, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		// Fallback for non-Linux or simulated test environments
		return SlotA, fmt.Errorf("unable to read /proc/cmdline, defaulting to slot a: %w", err)
	}

	cmdline := string(data)
	tokens := strings.Fields(cmdline)
	for _, token := range tokens {
		if strings.HasPrefix(token, "aegis.slot=") {
			val := strings.TrimPrefix(token, "aegis.slot=")
			if strings.EqualFold(val, "a") {
				return SlotA, nil
			} else if strings.EqualFold(val, "b") {
				return SlotB, nil
			}
		}
		if strings.HasPrefix(token, "root=") {
			layout := DefaultLayout()
			val := strings.TrimPrefix(token, "root=")
			if val == layout.SlotAPath {
				return SlotA, nil
			} else if val == layout.SlotBPath {
				return SlotB, nil
			}
		}
	}

	return SlotUnknown, errors.New("could not determine active slot from /proc/cmdline")
}

// GetPassiveSlot returns the opposite, inactive slot where new firmware should be flashed.
func GetPassiveSlot(active Slot) (Slot, error) {
	switch active {
	case SlotA:
		return SlotB, nil
	case SlotB:
		return SlotA, nil
	default:
		return SlotUnknown, fmt.Errorf("invalid active slot: %s", active)
	}
}

// GetDeviceForSlot returns the physical or virtual block device path for the given slot.
func (l *PartitionLayout) GetDeviceForSlot(s Slot) (string, error) {
	switch s {
	case SlotA:
		return l.SlotAPath, nil
	case SlotB:
		return l.SlotBPath, nil
	default:
		return "", fmt.Errorf("unknown slot: %s", s)
	}
}
