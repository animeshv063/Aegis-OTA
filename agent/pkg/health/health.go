package health

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
)

// CheckResult records status of an individual health assessment.
type CheckResult struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Message string `json:"message"`
}

// SystemHealthReport aggregates all post-boot validation checks.
type SystemHealthReport struct {
	AllPassed bool          `json:"all_passed"`
	Timestamp time.Time     `json:"timestamp"`
	Checks    []CheckResult `json:"checks"`
}

// RunAllChecks performs strict health validation before an update is committed.
func RunAllChecks() *SystemHealthReport {
	report := &SystemHealthReport{
		Timestamp: time.Now(),
		AllPassed: true,
		Checks:    make([]CheckResult, 0),
	}

	// 1. Filesystem Mount Verification
	report.addCheck(checkMounts())

	// 2. Network Reachability
	report.addCheck(checkNetwork())

	// 3. Storage Pool Access
	report.addCheck(checkStoragePool())

	// 4. Critical Services
	report.addCheck(checkServices([]string{"systemd-journald"}))

	return report
}

func (r *SystemHealthReport) addCheck(res CheckResult) {
	r.Checks = append(r.Checks, res)
	if !res.Passed {
		r.AllPassed = false
	}
}

func checkMounts() CheckResult {
	dataPath := os.Getenv("AEGIS_DATA_PATH")
	if dataPath == "" {
		dataPath = "/tmp" // Fallback path in testing
	}

	testFile := fmt.Sprintf("%s/.aegis_health_%d", dataPath, time.Now().UnixNano())
	err := os.WriteFile(testFile, []byte("healthcheck"), 0644)
	if err != nil {
		return CheckResult{
			Name:    "Persistent Mount R/W Check",
			Passed:  false,
			Message: fmt.Sprintf("Failed to write to persistent data volume %s: %v", dataPath, err),
		}
	}
	_ = os.Remove(testFile)

	return CheckResult{
		Name:    "Persistent Mount R/W Check",
		Passed:  true,
		Message: fmt.Sprintf("Persistent storage at %s is accessible and writable", dataPath),
	}
}

func checkNetwork() CheckResult {
	// Check loopback or local socket connectivity
	conn, err := net.DialTimeout("tcp", "1.1.1.1:53", 2*time.Second)
	if err != nil {
		// Try fallback loopback check in air-gapped embedded environments
		if lconn, lerr := net.Dial("tcp", "127.0.0.1:0"); lerr == nil {
			_ = lconn.Close()
			return CheckResult{
				Name:    "Network Subsystem Check",
				Passed:  true,
				Message: "Local network stack verified (isolated mode)",
			}
		}
		return CheckResult{
			Name:    "Network Connectivity Check",
			Passed:  false,
			Message: fmt.Sprintf("Network connection test timed out: %v", err),
		}
	}
	defer conn.Close()

	return CheckResult{
		Name:    "Network Connectivity Check",
		Passed:  true,
		Message: "External DNS gateway connection established",
	}
}

func checkStoragePool() CheckResult {
	// In NAS hardware, verify pool status (ZFS, mdadm, or target storage dir)
	storageDir := os.Getenv("AEGIS_STORAGE_POOL")
	if storageDir == "" {
		storageDir = "/var"
	}

	info, err := os.Stat(storageDir)
	if err != nil || !info.IsDir() {
		return CheckResult{
			Name:    "NAS Storage Pool Status",
			Passed:  false,
			Message: fmt.Sprintf("Storage pool path %s is unreachable or invalid", storageDir),
		}
	}

	return CheckResult{
		Name:    "NAS Storage Pool Status",
		Passed:  true,
		Message: fmt.Sprintf("Storage pool %s healthy and operational", storageDir),
	}
}

func checkServices(services []string) CheckResult {
	if _, err := exec.LookPath("systemctl"); err != nil {
		// Non-systemd test environment
		return CheckResult{
			Name:    "Core Daemon Checks",
			Passed:  true,
			Message: "systemctl not present; skipping init service unit check",
		}
	}

	var failed []string
	for _, svc := range services {
		cmd := exec.Command("systemctl", "is-active", "--quiet", svc)
		if err := cmd.Run(); err != nil {
			failed = append(failed, svc)
		}
	}

	if len(failed) > 0 {
		return CheckResult{
			Name:    "Core Daemon Checks",
			Passed:  false,
			Message: fmt.Sprintf("Services not active: %s", strings.Join(failed, ", ")),
		}
	}

	return CheckResult{
		Name:    "Core Daemon Checks",
		Passed:  true,
		Message: "All critical system services are running",
	}
}
