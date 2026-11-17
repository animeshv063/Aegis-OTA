package watchdog

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"
)

// HardwareWatchdog handles periodic keepalive pulses to /dev/watchdog or systemd.
type Watchdog struct {
	devPath      string
	deviceFile   *os.File
	systemdSock  string
	interval     time.Duration
}

// NewWatchdog discovers available watchdog devices.
func NewWatchdog() *Watchdog {
	devPath := "/dev/watchdog"
	if custom := os.Getenv("AEGIS_WATCHDOG_DEV"); custom != "" {
		devPath = custom
	}

	systemdSock := os.Getenv("NOTIFY_SOCKET")

	// Determine interval from WATCHDOG_USEC if set by systemd
	interval := 15 * time.Second
	if usecStr := os.Getenv("WATCHDOG_USEC"); usecStr != "" {
		if usec, err := strconv.ParseInt(usecStr, 10, 64); err == nil && usec > 0 {
			// Kick at half the timeout period to avoid race conditions
			interval = time.Duration(usec/2) * time.Microsecond
		}
	}

	return &Watchdog{
		devPath:     devPath,
		systemdSock: systemdSock,
		interval:    interval,
	}
}

// OpenDevice opens the raw hardware watchdog file if present.
func (w *Watchdog) OpenDevice() error {
	f, err := os.OpenFile(w.devPath, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("unable to open hardware watchdog %s: %w", w.devPath, err)
	}
	w.deviceFile = f
	return nil
}

// Kick resets the hardware counter by writing a single character to /dev/watchdog
// or sending WATCHDOG=1 to systemd NOTIFY_SOCKET.
func (w *Watchdog) Kick() error {
	// 1. Kick raw hardware watchdog device if open
	if w.deviceFile != nil {
		if _, err := w.deviceFile.Write([]byte("1")); err != nil {
			return fmt.Errorf("hardware watchdog kick failed: %w", err)
		}
	}

	// 2. Notify systemd watchdog socket if configured
	if w.systemdSock != "" {
		if err := sendSystemdNotification("WATCHDOG=1", w.systemdSock); err != nil {
			return fmt.Errorf("systemd watchdog notify failed: %w", err)
		}
	}

	return nil
}

// Close disarms the watchdog cleanly by writing the magic 'V' character before exit.
func (w *Watchdog) Close() error {
	if w.deviceFile != nil {
		// Linux watchdog convention: write 'V' before closing to prevent immediate reboot
		_, _ = w.deviceFile.Write([]byte("V"))
		err := w.deviceFile.Close()
		w.deviceFile = nil
		return err
	}
	return nil
}

// StartAutoKick starts a background goroutine that pulses the watchdog until ctx is cancelled.
func (w *Watchdog) StartAutoKick(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				_ = w.Close()
				return
			case <-ticker.C:
				_ = w.Kick()
			}
		}
	}()
}

func sendSystemdNotification(state, sockPath string) error {
	addr := &net.UnixAddr{
		Name: sockPath,
		Net:  "unixgram",
	}
	conn, err := net.DialUnix("unixgram", nil, addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = conn.Write([]byte(state))
	return err
}
