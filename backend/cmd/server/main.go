package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/animeshv063/Aegis-OTA/agent/pkg/manifest"
)

type DeviceInfo struct {
	DeviceID       string    `json:"device_id"`
	HardwareModel  string    `json:"hardware_model"`
	CurrentSlot    string    `json:"current_slot"`
	CurrentVersion string    `json:"current_version"`
	BootCount      int       `json:"boot_count"`
	BootLimit      int       `json:"boot_limit"`
	UpgradePending bool      `json:"upgrade_pending"`
	HealthStatus   string    `json:"health_status"`
	LastSeen       time.Time `json:"last_seen"`
	StoragePool    string    `json:"storage_pool"`
}

type SimulationLog struct {
	Timestamp string `json:"timestamp"`
	Source    string `json:"source"`
	Level     string `json:"level"`
	Message   string `json:"message"`
}

type ServerState struct {
	mu         sync.RWMutex
	devices    map[string]*DeviceInfo
	manifest   *manifest.Manifest
	imageBytes []byte
	pubKeyHex  string
	privKeyHex string
	logs       []SimulationLog
}

func newServerState() *ServerState {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		log.Fatalf("failed to generate ed25519 keys: %v", err)
	}

	pubHex := hex.EncodeToString(pub)
	privHex := hex.EncodeToString(priv)

	dummyPayload := []byte("AEGIS-NAS-OS-IMMUTABLE-IMAGE-V1.2.0-BLOCK-STREAM")
	m := &manifest.Manifest{
		Version:        "1.2.0",
		ImageName:      "aegis-nas-os-v1.2.0-arm64.rootfs.img",
		TargetArch:     "arm64",
		SHA256:         "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		SizeBytes:      int64(len(dummyPayload)),
		BuildTimestamp: time.Now().UTC(),
		Metadata: map[string]string{
			"builder":       "Yocto Scarthgap 5.0",
			"kernel":        "6.6.30-aegis-nas",
			"storage_stack": "zfs-2.2.3",
		},
	}
	_ = m.Sign(privHex)

	initialDevices := map[string]*DeviceInfo{
		"nas-alpha-01": {
			DeviceID:       "nas-alpha-01",
			HardwareModel:  "Rockchip RK3588 Quad-SATA",
			CurrentSlot:    "A",
			CurrentVersion: "1.1.0",
			BootCount:      0,
			BootLimit:      3,
			UpgradePending: false,
			HealthStatus:   "HEALTHY",
			LastSeen:       time.Now(),
			StoragePool:    "ZFS RAIDZ2 (4x 8TB)",
		},
		"nas-bravo-02": {
			DeviceID:       "nas-bravo-02",
			HardwareModel:  "Intel Atom x6413E 8-Bay NAS",
			CurrentSlot:    "A",
			CurrentVersion: "1.1.0",
			BootCount:      0,
			BootLimit:      3,
			UpgradePending: false,
			HealthStatus:   "HEALTHY",
			LastSeen:       time.Now(),
			StoragePool:    "ZFS Mirror (2x 16TB)",
		},
		"nas-charlie-03": {
			DeviceID:       "nas-charlie-03",
			HardwareModel:  "Ampere Altra Edge Storage",
			CurrentSlot:    "B",
			CurrentVersion: "1.2.0",
			BootCount:      0,
			BootLimit:      3,
			UpgradePending: false,
			HealthStatus:   "HEALTHY",
			LastSeen:       time.Now(),
			StoragePool:    "NVMe-oF Pool",
		},
	}

	initialLogs := []SimulationLog{
		{Timestamp: time.Now().Add(-5 * time.Minute).Format("15:04:05"), Source: "U-BOOT", Level: "INFO", Message: "Dual A/B bootloader initialized. Slot A verified."},
		{Timestamp: time.Now().Add(-4 * time.Minute).Format("15:04:05"), Source: "AGENT", Level: "INFO", Message: "Aegis edge daemon listening for signed release manifests."},
		{Timestamp: time.Now().Add(-3 * time.Minute).Format("15:04:05"), Source: "WATCHDOG", Level: "INFO", Message: "Hardware watchdog armed. Post-boot self-healing online."},
	}

	return &ServerState{
		devices:    initialDevices,
		manifest:   m,
		imageBytes: dummyPayload,
		pubKeyHex:  pubHex,
		privKeyHex: privHex,
		logs:       initialLogs,
	}
}

func (s *ServerState) addLog(source, level, msg string) {
	s.logs = append(s.logs, SimulationLog{
		Timestamp: time.Now().Format("15:04:05"),
		Source:    source,
		Level:     level,
		Message:   msg,
	})
	if len(s.logs) > 50 {
		s.logs = s.logs[len(s.logs)-50:]
	}
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	state := newServerState()

	// 1. Web UI Dashboard
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		renderDashboard(w, state)
	})

	// 2. OTA Manifest API
	http.HandleFunc("/api/v1/ota/manifest", func(w http.ResponseWriter, r *http.Request) {
		state.mu.RLock()
		defer state.mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(state.manifest)
	})

	// 3. OTA Image Stream API
	http.HandleFunc("/api/v1/ota/image", func(w http.ResponseWriter, r *http.Request) {
		state.mu.RLock()
		defer state.mu.RUnlock()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename=aegis-nas-os-v1.2.0.img")
		_, _ = w.Write(state.imageBytes)
	})

	// 4. Fleet Devices List API
	http.HandleFunc("/api/v1/fleet/devices", func(w http.ResponseWriter, r *http.Request) {
		state.mu.RLock()
		defer state.mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(state.devices)
	})

	// 5. Interactive Simulation Actions API
	http.HandleFunc("/api/v1/fleet/simulate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		action := r.URL.Query().Get("action")
		nodeID := r.URL.Query().Get("node")
		if nodeID == "" {
			nodeID = "nas-alpha-01"
		}

		state.mu.Lock()
		defer state.mu.Unlock()

		dev, ok := state.devices[nodeID]
		if !ok {
			http.Error(w, "Device not found", http.StatusNotFound)
			return
		}

		switch action {
		case "flash_update":
			targetSlot := "B"
			if dev.CurrentSlot == "B" {
				targetSlot = "A"
			}
			dev.UpgradePending = true
			dev.HealthStatus = "UPGRADE_PENDING"
			dev.BootCount = 1
			state.addLog("AGENT", "INFO", fmt.Sprintf("[%s] Verified Ed25519 signature. Streamed 500MB to passive Slot %s.", nodeID, targetSlot))
			state.addLog("U-BOOT", "WARN", fmt.Sprintf("[%s] fw_setenv active_slot=%s upgrade_available=1 bootcount=1", nodeID, targetSlot))

		case "simulate_panic":
			state.addLog("KERNEL", "ERROR", fmt.Sprintf("[%s] Kernel panic during early init on new slot! Watchdog counter expiring...", nodeID))
			state.addLog("WATCHDOG", "CRITICAL", fmt.Sprintf("[%s] Hardware watchdog timeout (60s) reached! Hard power-cycling board...", nodeID))
			state.addLog("U-BOOT", "CRITICAL", fmt.Sprintf("[%s] Boot attempts exceeded bootlimit (3/3). Triggering altbootcmd!", nodeID))
			dev.BootCount = 0
			dev.UpgradePending = false
			dev.HealthStatus = "HEALTHY"
			state.addLog("U-BOOT", "SUCCESS", fmt.Sprintf("[%s] Restored fallback Slot %s automatically. Device saved from bricking.", nodeID, dev.CurrentSlot))

		case "simulate_success":
			targetSlot := "B"
			if dev.CurrentSlot == "B" {
				targetSlot = "A"
			}
			dev.CurrentSlot = targetSlot
			dev.CurrentVersion = state.manifest.Version
			dev.UpgradePending = false
			dev.BootCount = 0
			dev.HealthStatus = "HEALTHY"
			state.addLog("HEALTH", "PASS", fmt.Sprintf("[%s] Post-boot self-check passed: ZFS pool imported, 10GbE network OK.", nodeID))
			state.addLog("AGENT", "SUCCESS", fmt.Sprintf("[%s] aegis-agent commit: Slot %s permanently committed in U-Boot.", nodeID, targetSlot))

		default:
			http.Error(w, "Unknown action", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "success",
			"device": dev,
		})
	})

	// 6. Health check
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	log.Printf("==========================================================")
	log.Printf("  Aegis OTA Fleet Operations Server Running on :%s", port)
	log.Printf("  Live Web UI:       http://localhost:%s/", port)
	log.Printf("  Manifest Endpoint: http://localhost:%s/api/v1/ota/manifest", port)
	log.Printf("==========================================================")

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("server terminated: %v", err)
	}
}

func renderDashboard(w http.ResponseWriter, state *ServerState) {
	state.mu.RLock()
	defer state.mu.RUnlock()

	tmplStr := `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Aegis OTA | Fleet Operations & Rollback Engine</title>
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link href="https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;500;700&family=Plus+Jakarta+Sans:wght@400;600;700;800&display=swap" rel="stylesheet">
  <style>
    :root {
      --bg: #090d16;
      --card-bg: #111726;
      --card-border: #1e293b;
      --primary: #38bdf8;
      --primary-glow: rgba(56, 189, 248, 0.15);
      --success: #10b981;
      --warning: #f59e0b;
      --danger: #ef4444;
      --text: #f8fafc;
      --text-muted: #94a3b8;
    }
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      background-color: var(--bg);
      color: var(--text);
      font-family: 'Plus Jakarta Sans', sans-serif;
      min-height: 100vh;
      padding: 2rem;
    }
    .container { max-width: 1300px; margin: 0 auto; }
    header {
      display: flex;
      justify-content: space-between;
      align-items: center;
      padding-bottom: 2rem;
      border-bottom: 1px solid var(--card-border);
      margin-bottom: 2rem;
    }
    .brand { display: flex; align-items: center; gap: 1rem; }
    .brand-logo {
      width: 44px;
      height: 44px;
      background: linear-gradient(135deg, #0284c7, #38bdf8);
      border-radius: 12px;
      display: flex;
      align-items: center;
      justify-content: center;
      font-weight: 800;
      font-size: 1.3rem;
      box-shadow: 0 0 20px rgba(56,189,248,0.4);
    }
    .brand h1 { font-size: 1.5rem; font-weight: 800; letter-spacing: -0.5px; }
    .brand p { font-size: 0.85rem; color: var(--text-muted); }
    .badge-live {
      display: inline-flex;
      align-items: center;
      gap: 0.5rem;
      padding: 0.4rem 0.9rem;
      border-radius: 9999px;
      background: rgba(16, 185, 129, 0.1);
      border: 1px solid rgba(16, 185, 129, 0.3);
      color: var(--success);
      font-size: 0.8rem;
      font-weight: 600;
    }
    .pulse-dot {
      width: 8px;
      height: 8px;
      border-radius: 50%;
      background: var(--success);
      animation: pulse 1.5s infinite;
    }
    @keyframes pulse {
      0% { transform: scale(0.9); opacity: 0.7; }
      50% { transform: scale(1.2); opacity: 1; }
      100% { transform: scale(0.9); opacity: 0.7; }
    }
    .stats-grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(250px, 1fr));
      gap: 1.25rem;
      margin-bottom: 2rem;
    }
    .stat-card {
      background: var(--card-bg);
      border: 1px solid var(--card-border);
      border-radius: 14px;
      padding: 1.25rem 1.5rem;
    }
    .stat-label { font-size: 0.8rem; text-transform: uppercase; letter-spacing: 0.5px; color: var(--text-muted); margin-bottom: 0.5rem; }
    .stat-val { font-size: 1.7rem; font-weight: 800; font-family: 'JetBrains Mono', monospace; }
    
    .main-grid {
      display: grid;
      grid-template-columns: 2fr 1fr;
      gap: 1.5rem;
      margin-bottom: 2rem;
    }
    @media (max-width: 1000px) { .main-grid { grid-template-columns: 1fr; } }
    
    .panel {
      background: var(--card-bg);
      border: 1px solid var(--card-border);
      border-radius: 14px;
      padding: 1.5rem;
    }
    .panel-header {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-bottom: 1.25rem;
    }
    .panel-title { font-size: 1.1rem; font-weight: 700; }
    
    .node-card {
      background: #0d121f;
      border: 1px solid #1a2234;
      border-radius: 10px;
      padding: 1.2rem;
      margin-bottom: 1rem;
    }
    .node-top {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-bottom: 0.8rem;
    }
    .node-id { font-family: 'JetBrains Mono', monospace; font-weight: 700; font-size: 1.05rem; }
    .slot-pill {
      font-size: 0.75rem;
      font-weight: 700;
      padding: 0.25rem 0.6rem;
      border-radius: 6px;
      font-family: 'JetBrains Mono', monospace;
    }
    .slot-a { background: rgba(56, 189, 248, 0.15); color: var(--primary); border: 1px solid rgba(56, 189, 248, 0.4); }
    .slot-b { background: rgba(168, 85, 247, 0.15); color: #c084fc; border: 1px solid rgba(168, 85, 247, 0.4); }
    
    .node-meta {
      display: grid;
      grid-template-columns: repeat(3, 1fr);
      gap: 0.5rem;
      font-size: 0.8rem;
      color: var(--text-muted);
    }
    .node-meta span { color: var(--text); font-weight: 600; }
    
    .actions-panel { display: flex; flex-direction: column; gap: 0.8rem; }
    .sim-btn {
      width: 100%;
      padding: 0.9rem 1.2rem;
      border-radius: 10px;
      border: 1px solid transparent;
      font-size: 0.9rem;
      font-weight: 700;
      cursor: pointer;
      display: flex;
      align-items: center;
      justify-content: center;
      gap: 0.6rem;
      transition: all 0.2s ease;
      font-family: 'Plus Jakarta Sans', sans-serif;
    }
    .btn-update { background: var(--primary); color: #000; }
    .btn-update:hover { background: #7dd3fc; transform: translateY(-1px); }
    .btn-fail { background: rgba(239, 68, 68, 0.15); border-color: rgba(239, 68, 68, 0.4); color: #fca5a5; }
    .btn-fail:hover { background: rgba(239, 68, 68, 0.25); }
    .btn-pass { background: rgba(16, 185, 129, 0.15); border-color: rgba(16, 185, 129, 0.4); color: #86efac; }
    .btn-pass:hover { background: rgba(16, 185, 129, 0.25); }
    
    .console-panel {
      background: #06090e;
      border: 1px solid #1a2234;
      border-radius: 10px;
      padding: 1rem;
      font-family: 'JetBrains Mono', monospace;
      font-size: 0.8rem;
      height: 240px;
      overflow-y: auto;
    }
    .log-entry { margin-bottom: 0.4rem; line-height: 1.4; }
    .log-ts { color: #64748b; }
    .log-src { color: #38bdf8; font-weight: 700; }
    .log-info { color: #cbd5e1; }
    .log-warn { color: #fbbf24; }
    .log-err { color: #f87171; font-weight: 700; }
    .log-pass { color: #4ade80; font-weight: 700; }
    
    .manifest-box {
      background: #06090e;
      border: 1px solid #1a2234;
      border-radius: 10px;
      padding: 1rem;
      font-family: 'JetBrains Mono', monospace;
      font-size: 0.75rem;
      color: #94a3b8;
      overflow-x: auto;
      margin-top: 1rem;
    }
    footer {
      text-align: center;
      margin-top: 3rem;
      font-size: 0.8rem;
      color: #64748b;
      border-top: 1px solid var(--card-border);
      padding-top: 1.5rem;
    }
  </style>
</head>
<body>
  <div class="container">
    <header>
      <div class="brand">
        <div class="brand-logo">&#x26E8;</div>
        <div>
          <h1>Aegis OTA Fleet Operations</h1>
          <p>Zero-Brick Fail-Safe Deployment Engine for NAS Storage Clusters</p>
        </div>
      </div>
      <div class="badge-live">
        <span class="pulse-dot"></span>
        FLEET ACTIVE &bull; WATCHDOG ARMED
      </div>
    </header>

    <div class="stats-grid">
      <div class="stat-card">
        <div class="stat-label">Total Connected Nodes</div>
        <div class="stat-val">{{len .Devices}} <span style="font-size:1rem;color:var(--text-muted)">Units</span></div>
      </div>
      <div class="stat-card">
        <div class="stat-label">Cryptographic Protection</div>
        <div class="stat-val" style="color:var(--primary)">Ed25519</div>
      </div>
      <div class="stat-card">
        <div class="stat-label">Active Partitions</div>
        <div class="stat-val">A / B <span style="font-size:1rem;color:var(--success)">Immutable</span></div>
      </div>
      <div class="stat-card">
        <div class="stat-label">Bootloader Rollback</div>
        <div class="stat-val" style="color:var(--success)">Auto-Armed</div>
      </div>
    </div>

    <div class="main-grid">
      <div class="panel">
        <div class="panel-header">
          <div class="panel-title">Active NAS Fleet Units</div>
          <span style="font-size:0.8rem;color:var(--text-muted)">Live Telemetry</span>
        </div>
        <div id="nodesList">
          {{range .Devices}}
          <div class="node-card">
            <div class="node-top">
              <div class="node-id">{{.DeviceID}}</div>
              <div class="slot-pill {{if eq .CurrentSlot "A"}}slot-a{{else}}slot-b{{end}}">
                BOOT SLOT {{.CurrentSlot}}
              </div>
            </div>
            <div class="node-meta">
              <div>Hardware: <span>{{.HardwareModel}}</span></div>
              <div>Firmware: <span>v{{.CurrentVersion}}</span></div>
              <div>Storage: <span>{{.StoragePool}}</span></div>
              <div>U-Boot Bootcount: <span>{{.BootCount}} / {{.BootLimit}}</span></div>
              <div>Watchdog: <span>Active</span></div>
              <div>Status: <span style="{{if eq .HealthStatus "HEALTHY"}}color:var(--success){{else}}color:var(--warning){{end}}">{{.HealthStatus}}</span></div>
            </div>
          </div>
          {{end}}
        </div>

        <div class="panel-header" style="margin-top:1.5rem">
          <div class="panel-title">Active Signed Manifest (v{{.Manifest.Version}})</div>
        </div>
        <div class="manifest-box">
{
  "version": "{{.Manifest.Version}}",
  "image": "{{.Manifest.ImageName}}",
  "sha256": "{{.Manifest.SHA256}}",
  "target_arch": "{{.Manifest.TargetArch}}",
  "signature": "{{.Manifest.Signature}}"
}
        </div>
      </div>

      <div class="panel">
        <div class="panel-header">
          <div class="panel-title">Live Simulator Controls</div>
        </div>
        <div class="actions-panel">
          <button class="sim-btn btn-update" onclick="runSim('flash_update')">
            &#9654; 1. Stream Update to Slot B
          </button>
          <button class="sim-btn btn-fail" onclick="runSim('simulate_panic')">
            &#9888; 2. Trigger Panic & Rollback
          </button>
          <button class="sim-btn btn-pass" onclick="runSim('simulate_success')">
            &#10003; 3. Health Check & Commit
          </button>
        </div>

        <div class="panel-header" style="margin-top:1.5rem">
          <div class="panel-title">U-Boot & Agent Console</div>
        </div>
        <div class="console-panel" id="consoleBox">
          {{range .Logs}}
          <div class="log-entry">
            <span class="log-ts">[{{.Timestamp}}]</span>
            <span class="log-src">[{{.Source}}]</span>:
            <span class="{{if eq .Level "CRITICAL"}}log-err{{else if eq .Level "WARN"}}log-warn{{else if eq .Level "SUCCESS"}}log-pass{{else}}log-info{{end}}">{{.Message}}</span>
          </div>
          {{end}}
        </div>
      </div>
    </div>

    <footer>
      Aegis OTA &bull; Designed for Physical Edge &amp; NAS Hardware Fleets &bull; Animesh Verma &amp; Team
    </footer>
  </div>

  <script>
    async function runSim(action) {
      try {
        const res = await fetch('/api/v1/fleet/simulate?node=nas-alpha-01&action=' + action, { method: 'POST' });
        if (res.ok) {
          setTimeout(() => window.location.reload(), 400);
        }
      } catch (err) {
        alert('Simulation trigger error: ' + err);
      }
    }
    const box = document.getElementById('consoleBox');
    if (box) box.scrollTop = box.scrollHeight;
  </script>
</body>
</html>`

	tmpl, err := template.New("dash").Parse(tmplStr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := struct {
		Devices  map[string]*DeviceInfo
		Manifest *manifest.Manifest
		Logs     []SimulationLog
	}{
		Devices:  state.devices,
		Manifest: state.manifest,
		Logs:     state.logs,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tmpl.Execute(w, data)
}
