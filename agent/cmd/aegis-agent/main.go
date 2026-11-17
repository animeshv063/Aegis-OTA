package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/animeshv063/Aegis-OTA/agent/pkg/flasher"
	"github.com/animeshv063/Aegis-OTA/agent/pkg/health"
	"github.com/animeshv063/Aegis-OTA/agent/pkg/manifest"
	"github.com/animeshv063/Aegis-OTA/agent/pkg/partition"
	"github.com/animeshv063/Aegis-OTA/agent/pkg/uboot"
	"github.com/animeshv063/Aegis-OTA/agent/pkg/watchdog"
)

const banner = `
    ___              _          ____  _________ 
   /   |  ___  ____ (_)____    / __ \/_  __/   |
  / /| | / _ \/ __  / / ___/  / / / / / / / /| |
 / ___ |/  __/ /_/ / (__  )  / /_/ / / / / ___ |
/_/  |_|\___/\__, /_//____/   \____/ /_/ /_/  |_|
            /____/  Fleet OTA Update Agent
`

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "status":
		handleStatus()
	case "verify":
		handleVerify(os.Args[2:])
	case "flash":
		handleFlash(os.Args[2:])
	case "healthcheck":
		handleHealthCheck()
	case "commit":
		handleCommit()
	case "rollback":
		handleRollback()
	case "daemon":
		handleDaemon()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(banner)
	fmt.Println("Usage: aegis-agent <command> [options]")
	fmt.Println("\nCommands:")
	fmt.Println("  status       Display current slot, boot counters, and partition layout")
	fmt.Println("  verify       Verify Ed25519 cryptographic signature of a manifest")
	fmt.Println("  flash        Stream-write signed OS image directly to passive partition")
	fmt.Println("  healthcheck  Execute post-boot self-tests and commit or rollback")
	fmt.Println("  commit       Mark current slot as healthy and finalize upgrade")
	fmt.Println("  rollback     Trigger emergency bootloader rollback to fallback slot")
	fmt.Println("  daemon       Run persistent background agent with watchdog keepalive")
}

func handleStatus() {
	activeSlot, err := partition.DetectActiveSlot()
	if err != nil {
		fmt.Printf("[WARN] Active slot detection: %v\n", err)
	}
	passiveSlot, _ := partition.GetPassiveSlot(activeSlot)
	layout := partition.DefaultLayout()
	ubootMgr := uboot.NewManager()

	upgradePending, _ := ubootMgr.GetEnv("upgrade_available")
	bootCount, _ := ubootMgr.GetEnv("bootcount")
	bootLimit, _ := ubootMgr.GetEnv("bootlimit")
	fallbackSlot, _ := ubootMgr.GetEnv("fallback_slot")

	fmt.Println(banner)
	fmt.Println("--- Fleet Hardware Status ---")
	fmt.Printf("Active Root Slot:        %s\n", activeSlot)
	fmt.Printf("Passive Target Slot:     %s\n", passiveSlot)
	fmt.Printf("Slot A Device:           %s\n", layout.SlotAPath)
	fmt.Printf("Slot B Device:           %s\n", layout.SlotBPath)
	fmt.Printf("Data Partition:          %s\n", layout.DataPath)
	fmt.Println("\n--- Bootloader (U-Boot) State ---")
	fmt.Printf("Upgrade Available Flag:  %s\n", upgradePending)
	fmt.Printf("Current Boot Attempts:   %s / %s\n", bootCount, bootLimit)
	fmt.Printf("Rollback Fallback Slot:  %s\n", fallbackSlot)
}

func handleVerify(args []string) {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	manifestPath := fs.String("manifest", "", "Path to manifest.json file")
	pubKey := fs.String("pubkey", "", "Ed25519 public key in hexadecimal format")
	_ = fs.Parse(args)

	if *manifestPath == "" || *pubKey == "" {
		fmt.Println("Error: --manifest and --pubkey are required")
		fs.Usage()
		os.Exit(1)
	}

	m, err := manifest.Load(*manifestPath)
	if err != nil {
		fmt.Printf("[FAIL] Could not load manifest: %v\n", err)
		os.Exit(1)
	}

	if err := m.VerifySignature(*pubKey); err != nil {
		fmt.Printf("[FAIL] Signature Verification: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("[PASS] Manifest cryptographic signature is VALID.")
	fmt.Printf("  Version:    %s\n", m.Version)
	fmt.Printf("  Target Arch:%s\n", m.TargetArch)
	fmt.Printf("  Image SHA:  %s\n", m.SHA256)
}

func handleFlash(args []string) {
	fs := flag.NewFlagSet("flash", flag.ExitOnError)
	manifestPath := fs.String("manifest", "", "Path to signed manifest.json")
	pubKey := fs.String("pubkey", "", "Ed25519 public key hex")
	imageURL := fs.String("url", "", "HTTP stream URL for OS image artifact")
	_ = fs.Parse(args)

	if *manifestPath == "" || *pubKey == "" || *imageURL == "" {
		fmt.Println("Error: --manifest, --pubkey, and --url are required")
		fs.Usage()
		os.Exit(1)
	}

	// 1. Verify Manifest Signature First
	m, err := manifest.Load(*manifestPath)
	if err != nil {
		fmt.Printf("[ERROR] Failed to load manifest: %v\n", err)
		os.Exit(1)
	}
	if err := m.VerifySignature(*pubKey); err != nil {
		fmt.Printf("[ERROR] Refusing to flash untrusted image: %v\n", err)
		os.Exit(1)
	}

	// 2. Identify Passive Target Slot
	activeSlot, err := partition.DetectActiveSlot()
	if err != nil {
		fmt.Printf("[ERROR] Cannot determine active slot: %v\n", err)
		os.Exit(1)
	}
	passiveSlot, err := partition.GetPassiveSlot(activeSlot)
	if err != nil {
		fmt.Printf("[ERROR] Cannot determine passive slot: %v\n", err)
		os.Exit(1)
	}

	layout := partition.DefaultLayout()
	targetDevice, err := layout.GetDeviceForSlot(passiveSlot)
	if err != nil {
		fmt.Printf("[ERROR] Failed to resolve target device: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("[INFO] Current Active Slot: %s | Target Passive Slot: %s (%s)\n", activeSlot, passiveSlot, targetDevice)
	fmt.Printf("[INFO] Streaming OS Image (SHA256: %s) to raw block device...\n", m.SHA256)

	// 3. Stream to block device
	opts := flasher.StreamDirectOptions{
		ImageURL:          *imageURL,
		TargetPath:        targetDevice,
		ExpectedSHA256:    m.SHA256,
		ExpectedSizeBytes: m.SizeBytes,
		OnProgress: func(writtenBytes, totalBytes int64, percent float64) {
			fmt.Printf("\rProgress: [%.1f%%] %d/%d bytes flashed", percent, writtenBytes, totalBytes)
		},
	}

	hash, err := flasher.StreamToBlockDevice(opts)
	fmt.Println()
	if err != nil {
		fmt.Printf("[ERROR] Flashing aborted: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[PASS] Flashed and verified SHA256: %s\n", hash)

	// 4. Configure U-Boot for Slot Switch on Next Reboot
	ubootMgr := uboot.NewManager()
	if err := ubootMgr.PrepareUpgradeSwitch(string(passiveSlot), string(activeSlot), 3); err != nil {
		fmt.Printf("[ERROR] Failed to set U-Boot upgrade flags: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("[SUCCESS] Firmware successfully written and verified.")
	fmt.Println("[ACTION] Reboot the node to complete migration to the new slot.")
}

func handleHealthCheck() {
	fmt.Println("[INFO] Running Aegis Post-Boot Self-Healing Health Checks...")
	report := health.RunAllChecks()

	for _, check := range report.Checks {
		status := "[PASS]"
		if !check.Passed {
			status = "[FAIL]"
		}
		fmt.Printf("  %s %-30s : %s\n", status, check.Name, check.Message)
	}

	ubootMgr := uboot.NewManager()
	upgradePending, _ := ubootMgr.GetEnv("upgrade_available")

	if report.AllPassed {
		fmt.Println("[PASS] All system and storage health metrics verified.")
		if upgradePending == "1" {
			activeSlot, _ := partition.DetectActiveSlot()
			_ = ubootMgr.CommitUpdate(string(activeSlot))
			fmt.Println("[COMMIT] New OS image validated and permanently committed in U-Boot.")
		}
	} else {
		fmt.Println("[CRITICAL] Health verification failed.")
		if upgradePending == "1" {
			fmt.Println("[ROLLBACK] Initiating automated bootloader rollback...")
			fallback, err := ubootMgr.TriggerRollback()
			if err != nil {
				fmt.Printf("[ERROR] Rollback flag trigger failed: %v\n", err)
			} else {
				fmt.Printf("[ROLLBACK] Reverting active slot to %s and rebooting system...\n", fallback)
			}
			os.Exit(1)
		}
	}
}

func handleCommit() {
	activeSlot, _ := partition.DetectActiveSlot()
	ubootMgr := uboot.NewManager()
	if err := ubootMgr.CommitUpdate(string(activeSlot)); err != nil {
		fmt.Printf("[ERROR] Failed to commit slot: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[PASS] Active slot %s committed successfully.\n", activeSlot)
}

func handleRollback() {
	ubootMgr := uboot.NewManager()
	fallback, err := ubootMgr.TriggerRollback()
	if err != nil {
		fmt.Printf("[ERROR] Rollback failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[ALERT] Emergency rollback triggered! Next boot will activate slot %s.\n", fallback)
}

func handleDaemon() {
	fmt.Println(banner)
	fmt.Println("[DAEMON] Starting Aegis OTA background supervisor...")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Hardware and Systemd Watchdog Loop
	wd := watchdog.NewWatchdog()
	if err := wd.OpenDevice(); err != nil {
		fmt.Printf("[WARN] Hardware watchdog device not opened: %v (continuing with soft timers)\n", err)
	}
	wd.StartAutoKick(ctx)

	// Run initial health verification
	report := health.RunAllChecks()
	if !report.AllPassed {
		fmt.Println("[WARN] Initial health checks reported anomalies.")
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	fmt.Println("[DAEMON] Aegis agent is active and monitoring fleet health.")
	for {
		select {
		case sig := <-sigChan:
			fmt.Printf("\n[DAEMON] Received signal %v, disarming watchdog and exiting cleanly...\n", sig)
			cancel()
			return
		case <-ticker.C:
			// Periodic background sanity check
			_ = wd.Kick()
		}
	}
}
