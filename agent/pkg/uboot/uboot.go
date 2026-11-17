package uboot

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

var (
	mockMu   sync.Mutex
	mockFile = os.Getenv("AEGIS_UBOOT_ENV_FILE")
)

func init() {
	if mockFile == "" {
		mockFile = "/tmp/aegis_uboot.env"
	}
}

// UBootManager provides interfaces to read/write bootloader environment flags.
type UBootManager struct {
	useMock bool
}

// NewManager creates a manager, auto-detecting fw_printenv availability.
func NewManager() *UBootManager {
	_, err := exec.LookPath("fw_printenv")
	useMock := err != nil || os.Getenv("AEGIS_MOCK_UBOOT") == "1"
	return &UBootManager{useMock: useMock}
}

// GetEnv retrieves a variable from U-Boot environment.
func (m *UBootManager) GetEnv(key string) (string, error) {
	if m.useMock {
		return m.getMockEnv(key)
	}

	cmd := exec.Command("fw_printenv", "-n", key)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("fw_printenv failed for key %s: %w", key, err)
	}

	return strings.TrimSpace(string(out)), nil
}

// SetEnv writes a variable to U-Boot non-volatile storage.
func (m *UBootManager) SetEnv(key, value string) error {
	if m.useMock {
		return m.setMockEnv(key, value)
	}

	cmd := exec.Command("fw_setenv", key, value)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("fw_setenv failed for %s=%s: %w", key, value, err)
	}

	return nil
}

// PrepareUpgradeSwitch configures U-Boot to boot into targetSlot on next reboot with watchdog armed.
func (m *UBootManager) PrepareUpgradeSwitch(targetSlot string, fallbackSlot string, maxAttempts int) error {
	if err := m.SetEnv("upgrade_available", "1"); err != nil {
		return err
	}
	if err := m.SetEnv("active_slot", targetSlot); err != nil {
		return err
	}
	if err := m.SetEnv("fallback_slot", fallbackSlot); err != nil {
		return err
	}
	if err := m.SetEnv("bootcount", "0"); err != nil {
		return err
	}
	if err := m.SetEnv("bootlimit", strconv.Itoa(maxAttempts)); err != nil {
		return err
	}
	return nil
}

// CommitUpdate marks the current slot as successfully validated, disabling the rollback watchdog counter.
func (m *UBootManager) CommitUpdate(currentSlot string) error {
	if err := m.SetEnv("upgrade_available", "0"); err != nil {
		return err
	}
	if err := m.SetEnv("bootcount", "0"); err != nil {
		return err
	}
	if err := m.SetEnv("fallback_slot", currentSlot); err != nil {
		return err
	}
	return nil
}

// TriggerRollback immediately switches the active boot slot back to the known-good fallback.
func (m *UBootManager) TriggerRollback() (string, error) {
	fallback, err := m.GetEnv("fallback_slot")
	if err != nil || fallback == "" {
		fallback = "a" // Safe default fallback
	}

	if err := m.SetEnv("active_slot", fallback); err != nil {
		return "", err
	}
	if err := m.SetEnv("upgrade_available", "0"); err != nil {
		return "", err
	}
	if err := m.SetEnv("bootcount", "0"); err != nil {
		return "", err
	}

	return fallback, nil
}

func (m *UBootManager) getMockEnv(key string) (string, error) {
	mockMu.Lock()
	defer mockMu.Unlock()

	data, err := os.ReadFile(mockFile)
	if err != nil {
		// Provide default values
		switch key {
		case "active_slot":
			return "a", nil
		case "fallback_slot":
			return "a", nil
		case "upgrade_available":
			return "0", nil
		case "bootcount":
			return "0", nil
		case "bootlimit":
			return "3", nil
		default:
			return "", fmt.Errorf("key %s not found in mock env", key)
		}
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, key+"=") {
			return strings.TrimPrefix(line, key+"="), nil
		}
	}

	return "", fmt.Errorf("key %s not found in mock environment", key)
}

func (m *UBootManager) setMockEnv(key, value string) error {
	mockMu.Lock()
	defer mockMu.Unlock()

	envMap := make(map[string]string)
	if data, err := os.ReadFile(mockFile); err == nil {
		scanner := bufio.NewScanner(bytes.NewReader(data))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if idx := strings.Index(line, "="); idx != -1 {
				k := line[:idx]
				v := line[idx+1:]
				envMap[k] = v
			}
		}
	}

	envMap[key] = value

	if err := os.MkdirAll(filepath.Dir(mockFile), 0755); err != nil {
		return err
	}

	var sb strings.Builder
	for k, v := range envMap {
		sb.WriteString(fmt.Sprintf("%s=%s\n", k, v))
	}

	return os.WriteFile(mockFile, []byte(sb.String()), 0644)
}
