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
	Uptime         string    `json:"uptime"`
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
			DeviceID: "nas-alpha-01", HardwareModel: "Rockchip RK3588 Quad-SATA",
			CurrentSlot: "A", CurrentVersion: "1.1.0", BootCount: 0, BootLimit: 3,
			UpgradePending: false, HealthStatus: "HEALTHY", LastSeen: time.Now(),
			StoragePool: "ZFS RAIDZ2 (4x 8TB)", Uptime: "12d 4h 33m",
		},
		"nas-bravo-02": {
			DeviceID: "nas-bravo-02", HardwareModel: "Intel Atom x6413E 8-Bay NAS",
			CurrentSlot: "A", CurrentVersion: "1.1.0", BootCount: 0, BootLimit: 3,
			UpgradePending: false, HealthStatus: "HEALTHY", LastSeen: time.Now(),
			StoragePool: "ZFS Mirror (2x 16TB)", Uptime: "7d 2h 11m",
		},
		"nas-charlie-03": {
			DeviceID: "nas-charlie-03", HardwareModel: "Ampere Altra Edge Storage",
			CurrentSlot: "B", CurrentVersion: "1.2.0", BootCount: 0, BootLimit: 3,
			UpgradePending: false, HealthStatus: "HEALTHY", LastSeen: time.Now(),
			StoragePool: "NVMe-oF Pool", Uptime: "3d 9h 55m",
		},
	}

	initialLogs := []SimulationLog{
		{Timestamp: time.Now().Add(-5 * time.Minute).Format("15:04:05"), Source: "U-BOOT", Level: "INFO", Message: "Dual A/B bootloader initialized. Slot A verified."},
		{Timestamp: time.Now().Add(-4 * time.Minute).Format("15:04:05"), Source: "AGENT", Level: "INFO", Message: "Aegis edge daemon listening for signed release manifests."},
		{Timestamp: time.Now().Add(-3 * time.Minute).Format("15:04:05"), Source: "WATCHDOG", Level: "INFO", Message: "Hardware watchdog armed. Post-boot self-healing online."},
	}

	return &ServerState{
		devices: initialDevices, manifest: m, imageBytes: dummyPayload,
		pubKeyHex: pubHex, privKeyHex: privHex, logs: initialLogs,
	}
}

func (s *ServerState) addLog(source, level, msg string) {
	s.logs = append(s.logs, SimulationLog{
		Timestamp: time.Now().Format("15:04:05"), Source: source, Level: level, Message: msg,
	})
	if len(s.logs) > 60 {
		s.logs = s.logs[len(s.logs)-60:]
	}
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	state := newServerState()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		renderDashboard(w, state)
	})

	http.HandleFunc("/api/v1/ota/manifest", func(w http.ResponseWriter, r *http.Request) {
		state.mu.RLock()
		defer state.mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(state.manifest)
	})

	http.HandleFunc("/api/v1/fleet/devices", func(w http.ResponseWriter, r *http.Request) {
		state.mu.RLock()
		defer state.mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(state.devices)
	})

	http.HandleFunc("/api/v1/fleet/simulate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		action := r.URL.Query().Get("action")
		nodeID := r.URL.Query().Get("node")
		batchAll := nodeID == "all"

		state.mu.Lock()
		defer state.mu.Unlock()

		targets := []string{nodeID}
		if batchAll {
			targets = []string{"nas-alpha-01", "nas-bravo-02", "nas-charlie-03"}
			state.addLog("CONTROLLER", "INFO", "[BATCH] Initiating fleet-wide OTA deployment to all 3 nodes...")
		}

		for _, id := range targets {
			dev, ok := state.devices[id]
			if !ok {
				continue
			}
			dev.LastSeen = time.Now()

			targetSlot := "B"
			if dev.CurrentSlot == "B" {
				targetSlot = "A"
			}

			switch action {
			case "flash_update":
				dev.UpgradePending = true
				dev.HealthStatus = "UPGRADE_PENDING"
				dev.BootCount = 1
				state.addLog("AGENT", "INFO", fmt.Sprintf("[%s] Verified Ed25519 signature. Streaming 500MB image to passive Slot %s...", id, targetSlot))
				state.addLog("U-BOOT", "WARN", fmt.Sprintf("[%s] fw_setenv active_slot=%s upgrade_available=1 bootcount=1", id, targetSlot))

			case "simulate_panic":
				state.addLog("KERNEL", "ERROR", fmt.Sprintf("[%s] Kernel panic — not syncing: VFS: unable to mount root fs on new slot!", id))
				state.addLog("WATCHDOG", "CRITICAL", fmt.Sprintf("[%s] Watchdog timeout expired (60s)! Hard power-cycling board...", id))
				state.addLog("U-BOOT", "CRITICAL", fmt.Sprintf("[%s] bootcount %d exceeded bootlimit %d. Triggering altbootcmd!", id, dev.BootLimit, dev.BootLimit))
				dev.BootCount = 0
				dev.UpgradePending = false
				dev.HealthStatus = "HEALTHY"
				state.addLog("U-BOOT", "SUCCESS", fmt.Sprintf("[%s] Restored fallback Slot %s. Device self-healed. No brick occurred.", id, dev.CurrentSlot))

			case "simulate_success":
				dev.CurrentSlot = targetSlot
				dev.CurrentVersion = state.manifest.Version
				dev.UpgradePending = false
				dev.BootCount = 0
				dev.HealthStatus = "HEALTHY"
				state.addLog("HEALTH", "PASS", fmt.Sprintf("[%s] ZFS pool imported, 10GbE link up, all daemons active.", id))
				state.addLog("AGENT", "SUCCESS", fmt.Sprintf("[%s] aegis-agent commit: Slot %s permanently committed in U-Boot env.", id, targetSlot))
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "logs": state.logs})
	})

	http.HandleFunc("/api/v1/fleet/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		state.mu.Lock()
		for _, dev := range state.devices {
			dev.LastSeen = time.Now()
		}
		state.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	log.Printf("Aegis OTA Fleet Server on :%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func renderDashboard(w http.ResponseWriter, state *ServerState) {
	state.mu.RLock()
	defer state.mu.RUnlock()

	tmplStr := `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1.0">
<title>Aegis OTA | Fleet Operations Dashboard</title>
<link href="https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;500;700&family=Plus+Jakarta+Sans:wght@400;600;700;800&display=swap" rel="stylesheet">
<style>
:root{--bg:#090d16;--card:#111726;--border:#1e293b;--primary:#38bdf8;--success:#10b981;--warning:#f59e0b;--danger:#ef4444;--text:#f8fafc;--muted:#94a3b8}
*{box-sizing:border-box;margin:0;padding:0}
body{background:var(--bg);color:var(--text);font-family:'Plus Jakarta Sans',sans-serif;min-height:100vh;padding:1.5rem}
.container{max-width:1400px;margin:0 auto}
header{display:flex;justify-content:space-between;align-items:center;padding-bottom:1.5rem;border-bottom:1px solid var(--border);margin-bottom:1.5rem}
.brand{display:flex;align-items:center;gap:1rem}
.logo{width:44px;height:44px;background:linear-gradient(135deg,#0284c7,#38bdf8);border-radius:12px;display:flex;align-items:center;justify-content:center;font-size:1.3rem;font-weight:800;box-shadow:0 0 20px rgba(56,189,248,0.4)}
.brand h1{font-size:1.4rem;font-weight:800}
.brand p{font-size:.8rem;color:var(--muted)}
.badge-live{display:inline-flex;align-items:center;gap:.5rem;padding:.4rem .9rem;border-radius:9999px;background:rgba(16,185,129,.1);border:1px solid rgba(16,185,129,.3);color:var(--success);font-size:.8rem;font-weight:600}
.pulse{width:8px;height:8px;border-radius:50%;background:var(--success);animation:pulse 1.5s infinite}
@keyframes pulse{0%,100%{transform:scale(.9);opacity:.7}50%{transform:scale(1.2);opacity:1}}
.stats{display:grid;grid-template-columns:repeat(4,1fr);gap:1rem;margin-bottom:1.5rem}
@media(max-width:900px){.stats{grid-template-columns:repeat(2,1fr)}}
.stat{background:var(--card);border:1px solid var(--border);border-radius:14px;padding:1.1rem 1.4rem}
.stat-label{font-size:.75rem;text-transform:uppercase;letter-spacing:.5px;color:var(--muted);margin-bottom:.4rem}
.stat-val{font-size:1.6rem;font-weight:800;font-family:'JetBrains Mono',monospace}
.layout{display:grid;grid-template-columns:1fr 340px;gap:1.5rem}
@media(max-width:1050px){.layout{grid-template-columns:1fr}}
.panel{background:var(--card);border:1px solid var(--border);border-radius:14px;padding:1.4rem}
.panel-hdr{display:flex;justify-content:space-between;align-items:center;margin-bottom:1.2rem}
.panel-title{font-size:1rem;font-weight:700}
.node-card{background:#0a0f1c;border:1px solid #1a2234;border-radius:10px;padding:1.1rem;margin-bottom:.9rem;transition:border-color .3s,box-shadow .3s}
.node-card.panic{border-color:var(--danger)!important;box-shadow:0 0 16px rgba(239,68,68,.3)!important;animation:panicPulse .6s infinite}
@keyframes panicPulse{0%,100%{opacity:1}50%{opacity:.6}}
.node-card.pending{border-color:var(--warning)!important}
.node-top{display:flex;justify-content:space-between;align-items:center;margin-bottom:.8rem}
.node-id{font-family:'JetBrains Mono',monospace;font-weight:700;font-size:1rem}
.slot-pill{font-size:.7rem;font-weight:700;padding:.2rem .55rem;border-radius:6px;font-family:'JetBrains Mono',monospace}
.slot-a{background:rgba(56,189,248,.12);color:var(--primary);border:1px solid rgba(56,189,248,.35)}
.slot-b{background:rgba(168,85,247,.12);color:#c084fc;border:1px solid rgba(168,85,247,.35)}
.node-meta{display:grid;grid-template-columns:repeat(3,1fr);gap:.4rem;font-size:.77rem;color:var(--muted);margin-bottom:1rem}
.node-meta span{color:var(--text);font-weight:600}
.node-actions{display:grid;grid-template-columns:repeat(3,1fr);gap:.5rem}
.node-btn{padding:.45rem .3rem;border-radius:7px;border:1px solid transparent;font-size:.72rem;font-weight:700;cursor:pointer;font-family:'Plus Jakarta Sans',sans-serif;text-align:center;transition:all .15s}
.btn-flash{background:rgba(56,189,248,.12);border-color:rgba(56,189,248,.35);color:#7dd3fc}
.btn-flash:hover{background:rgba(56,189,248,.25)}
.btn-panic{background:rgba(239,68,68,.1);border-color:rgba(239,68,68,.35);color:#fca5a5}
.btn-panic:hover{background:rgba(239,68,68,.22)}
.btn-commit{background:rgba(16,185,129,.1);border-color:rgba(16,185,129,.35);color:#86efac}
.btn-commit:hover{background:rgba(16,185,129,.22)}
.progress-wrap{margin-top:.8rem;display:none}
.progress-label{font-size:.72rem;color:var(--muted);margin-bottom:.3rem;font-family:'JetBrains Mono',monospace}
.progress-bar-bg{background:#0d1526;border-radius:999px;height:8px;overflow:hidden}
.progress-bar-fill{height:8px;border-radius:999px;background:linear-gradient(90deg,#0284c7,#38bdf8);width:0%;transition:width .3s ease}
.batch-panel{background:#0a0f1c;border:1px solid var(--border);border-radius:10px;padding:1rem;margin-bottom:1.2rem}
.batch-title{font-size:.85rem;font-weight:700;color:var(--warning);margin-bottom:.8rem;display:flex;align-items:center;gap:.5rem}
.batch-actions{display:grid;grid-template-columns:repeat(3,1fr);gap:.5rem}
.console{background:#06090e;border:1px solid #1a2234;border-radius:10px;padding:.9rem;font-family:'JetBrains Mono',monospace;font-size:.75rem;height:260px;overflow-y:auto}
.log-entry{margin-bottom:.35rem;line-height:1.5}
.log-ts{color:#475569}
.log-src-UBOOT{color:#38bdf8}
.log-src-AGENT{color:#a78bfa}
.log-src-WATCHDOG{color:#fb923c}
.log-src-KERNEL{color:#f87171}
.log-src-HEALTH{color:#4ade80}
.log-src-CONTROLLER{color:#facc15}
.log-src-DEFAULT{color:#94a3b8}
.level-CRITICAL,.level-ERROR{color:#f87171;font-weight:700}
.level-SUCCESS,.level-PASS{color:#4ade80;font-weight:700}
.level-WARN{color:#fbbf24}
.level-INFO{color:#cbd5e1}
.manifest-box{background:#06090e;border:1px solid #1a2234;border-radius:10px;padding:.9rem;font-family:'JetBrains Mono',monospace;font-size:.72rem;color:#94a3b8;overflow-x:auto;margin-top:1rem;word-break:break-all}
footer{text-align:center;margin-top:2.5rem;font-size:.77rem;color:#475569;border-top:1px solid var(--border);padding-top:1.2rem}
.heartbeat-ts{font-size:.7rem;color:var(--muted);font-family:'JetBrains Mono',monospace}
</style>
</head>
<body>
<div class="container">
<header>
  <div class="brand">
    <div class="logo">&#x26E8;</div>
    <div>
      <h1>Aegis OTA Fleet Operations</h1>
      <p>Zero-Brick Fail-Safe Deployment Engine for NAS Storage Clusters</p>
    </div>
  </div>
  <div class="badge-live"><span class="pulse"></span>FLEET ACTIVE &bull; WATCHDOG ARMED &bull; <span id="liveTime"></span></div>
</header>

<div class="stats">
  <div class="stat"><div class="stat-label">Connected Nodes</div><div class="stat-val">{{len .Devices}} <span style="font-size:.9rem;color:var(--muted)">Units</span></div></div>
  <div class="stat"><div class="stat-label">Cryptographic Signing</div><div class="stat-val" style="color:var(--primary)">Ed25519</div></div>
  <div class="stat"><div class="stat-label">Active Partitions</div><div class="stat-val">A / B <span style="font-size:.9rem;color:var(--success)">Immutable</span></div></div>
  <div class="stat"><div class="stat-label">Bootloader Rollback</div><div class="stat-val" style="color:var(--success)">Auto-Armed</div></div>
</div>

<div class="layout">
<div>
  <!-- Batch OTA Panel -->
  <div class="panel" style="margin-bottom:1rem">
    <div class="batch-panel">
      <div class="batch-title">&#9889; Batch Fleet OTA Control — All 3 Nodes Simultaneously</div>
      <div class="batch-actions">
        <button class="node-btn btn-flash" onclick="runAction('all','flash_update')">&#9654; Batch Stream to Passive Slot</button>
        <button class="node-btn btn-panic" onclick="runAction('all','simulate_panic')">&#9888; Batch Panic &amp; Rollback</button>
        <button class="node-btn btn-commit" onclick="runAction('all','simulate_success')">&#10003; Batch Health &amp; Commit</button>
      </div>
    </div>
  </div>

  <!-- Per-Node Cards -->
  <div class="panel">
    <div class="panel-hdr">
      <div class="panel-title">Active NAS Fleet Units</div>
      <span class="heartbeat-ts">Live Telemetry &bull; Heartbeat: <span id="hbTime">--:--:--</span></span>
    </div>
    {{range .Devices}}
    <div class="node-card" id="card-{{.DeviceID}}">
      <div class="node-top">
        <div class="node-id">{{.DeviceID}}</div>
        <div class="slot-pill {{if eq .CurrentSlot "A"}}slot-a{{else}}slot-b{{end}}" id="slot-{{.DeviceID}}">BOOT SLOT {{.CurrentSlot}}</div>
      </div>
      <div class="node-meta">
        <div>HW: <span>{{.HardwareModel}}</span></div>
        <div>Firmware: <span id="ver-{{.DeviceID}}">v{{.CurrentVersion}}</span></div>
        <div>Storage: <span>{{.StoragePool}}</span></div>
        <div>Bootcount: <span id="bc-{{.DeviceID}}">{{.BootCount}}/{{.BootLimit}}</span></div>
        <div>Uptime: <span id="up-{{.DeviceID}}">{{.Uptime}}</span></div>
        <div>Status: <span id="status-{{.DeviceID}}" style="color:var(--success)">{{.HealthStatus}}</span></div>
      </div>
      <div class="node-actions">
        <button class="node-btn btn-flash" onclick="runAction('{{.DeviceID}}','flash_update')">&#9654; Stream to Passive Slot</button>
        <button class="node-btn btn-panic" onclick="runAction('{{.DeviceID}}','simulate_panic')">&#9888; Panic &amp; Rollback</button>
        <button class="node-btn btn-commit" onclick="runAction('{{.DeviceID}}','simulate_success')">&#10003; Health Check &amp; Commit</button>
      </div>
      <div class="progress-wrap" id="prog-{{.DeviceID}}">
        <div class="progress-label" id="prog-label-{{.DeviceID}}">Streaming image to passive partition... 0%</div>
        <div class="progress-bar-bg"><div class="progress-bar-fill" id="prog-fill-{{.DeviceID}}"></div></div>
      </div>
    </div>
    {{end}}

    <div class="panel-hdr" style="margin-top:1.5rem">
      <div class="panel-title">Active Signed Manifest (v{{.Manifest.Version}})</div>
    </div>
    <div class="manifest-box">{"version":"{{.Manifest.Version}}","image":"{{.Manifest.ImageName}}","sha256":"{{.Manifest.SHA256}}","arch":"{{.Manifest.TargetArch}}","signature":"{{.Manifest.Signature}}"}</div>
  </div>
</div>

<!-- Right Side: Console -->
<div>
  <div class="panel">
    <div class="panel-hdr"><div class="panel-title">U-Boot &amp; Agent Console</div></div>
    <div class="console" id="consoleBox">
      {{range .Logs}}
      <div class="log-entry">
        <span class="log-ts">[{{.Timestamp}}]</span>
        <span class="log-src-{{.Source}}{{if not (eq .Source "U-BOOT")}}{{if not (eq .Source "AGENT")}}{{if not (eq .Source "WATCHDOG")}}{{if not (eq .Source "KERNEL")}}{{if not (eq .Source "HEALTH")}}{{if not (eq .Source "CONTROLLER")}}-DEFAULT{{end}}{{end}}{{end}}{{end}}{{end}}{{end}}">[{{.Source}}]</span>:
        <span class="level-{{.Level}}">{{.Message}}</span>
      </div>
      {{end}}
    </div>
  </div>
</div>
</div>

<footer>Aegis OTA &bull; Designed for Physical Edge &amp; NAS Hardware Fleets &bull; Animesh Verma &amp; Team</footer>
</div>

<script>
// Live clock
function updateClock(){
  const t=new Date();
  const s=t.toTimeString().slice(0,8);
  document.getElementById('liveTime').textContent=s;
  document.getElementById('hbTime').textContent=s;
}
updateClock();
setInterval(updateClock,1000);

// Animated progress bar for a node
function animateProgress(nodeId, cb){
  const wrap=document.getElementById('prog-'+nodeId);
  const fill=document.getElementById('prog-fill-'+nodeId);
  const label=document.getElementById('prog-label-'+nodeId);
  if(!wrap)return cb();
  wrap.style.display='block';
  let pct=0;
  const steps=[2,5,8,12,15,20,25,32,38,45,50,55,60,66,72,78,83,88,92,95,98,100];
  let i=0;
  const interval=setInterval(()=>{
    if(i>=steps.length){
      clearInterval(interval);
      label.textContent='Flash complete. Verifying SHA-256 checksum... ✓';
      setTimeout(()=>{wrap.style.display='none';fill.style.width='0%';cb();},1200);
      return;
    }
    pct=steps[i++];
    fill.style.width=pct+'%';
    label.textContent='Streaming image to passive partition... '+pct+'%';
  },150);
}

// Panic countdown
function animatePanic(nodeId, cb){
  const card=document.getElementById('card-'+nodeId);
  if(card) card.classList.add('panic');
  let count=5;
  const statusEl=document.getElementById('status-'+nodeId);
  if(statusEl) statusEl.style.color='#f87171';
  const interval=setInterval(()=>{
    if(statusEl) statusEl.textContent='WATCHDOG COUNTDOWN: '+count+'s';
    count--;
    if(count<0){
      clearInterval(interval);
      if(card) card.classList.remove('panic');
      cb();
    }
  },600);
}

async function runAction(nodeId, action){
  // Flash update: show progress bar first
  if(action==='flash_update'){
    const nodeIds = nodeId==='all' ? ['nas-alpha-01','nas-bravo-02','nas-charlie-03'] : [nodeId];
    nodeIds.forEach(id=>{
      const card=document.getElementById('card-'+id);
      if(card) card.classList.add('pending');
    });
    // Animate all in parallel
    let done=0;
    nodeIds.forEach(id=>{
      animateProgress(id, async()=>{
        done++;
        if(done===nodeIds.length){
          await doFetch(nodeId, action);
          nodeIds.forEach(i=>{const c=document.getElementById('card-'+i);if(c)c.classList.remove('pending');});
        }
      });
    });
    return;
  }

  // Panic: show watchdog countdown
  if(action==='simulate_panic'){
    const nodeIds = nodeId==='all' ? ['nas-alpha-01','nas-bravo-02','nas-charlie-03'] : [nodeId];
    let done=0;
    nodeIds.forEach(id=>{
      animatePanic(id, async()=>{
        done++;
        if(done===nodeIds.length){
          await doFetch(nodeId, action);
        }
      });
    });
    return;
  }

  await doFetch(nodeId, action);
}

async function doFetch(nodeId, action){
  try{
    const res=await fetch('/api/v1/fleet/simulate?node='+nodeId+'&action='+action,{method:'POST'});
    if(res.ok){
      const data=await res.json();
      updateConsole(data.logs);
      await refreshDevices();
    }
  }catch(e){console.error(e);}
}

async function refreshDevices(){
  try{
    const res=await fetch('/api/v1/fleet/devices');
    const devs=await res.json();
    for(const[id,dev] of Object.entries(devs)){
      const slotEl=document.getElementById('slot-'+id);
      const verEl=document.getElementById('ver-'+id);
      const statusEl=document.getElementById('status-'+id);
      const bcEl=document.getElementById('bc-'+id);
      if(slotEl){
        slotEl.textContent='BOOT SLOT '+dev.current_slot;
        slotEl.className='slot-pill '+(dev.current_slot==='A'?'slot-a':'slot-b');
      }
      if(verEl) verEl.textContent='v'+dev.current_version;
      if(statusEl){
        statusEl.textContent=dev.health_status;
        statusEl.style.color=dev.health_status==='HEALTHY'?'var(--success)':dev.health_status==='UPGRADE_PENDING'?'var(--warning)':'var(--danger)';
      }
      if(bcEl) bcEl.textContent=dev.boot_count+'/'+dev.boot_limit;
    }
  }catch(e){console.error(e);}
}

function updateConsole(logs){
  if(!logs||!logs.length) return;
  const box=document.getElementById('consoleBox');
  box.innerHTML='';
  const srcColors={
    'U-BOOT':'#38bdf8','AGENT':'#a78bfa','WATCHDOG':'#fb923c',
    'KERNEL':'#f87171','HEALTH':'#4ade80','CONTROLLER':'#facc15'
  };
  const lvlColors={
    'CRITICAL':'#f87171','ERROR':'#f87171','SUCCESS':'#4ade80',
    'PASS':'#4ade80','WARN':'#fbbf24','INFO':'#cbd5e1'
  };
  logs.forEach(log=>{
    const div=document.createElement('div');
    div.className='log-entry';
    const srcColor=srcColors[log.source]||'#94a3b8';
    const lvlColor=lvlColors[log.level]||'#cbd5e1';
    const isBold=log.level==='CRITICAL'||log.level==='ERROR'||log.level==='SUCCESS'||log.level==='PASS';
    div.innerHTML=
      '<span class="log-ts">['+log.timestamp+']</span> '+
      '<span style="color:'+srcColor+';font-weight:700">['+log.source+']</span>: '+
      '<span style="color:'+lvlColor+';font-weight:'+(isBold?700:400)+'">'+log.message+'</span>';
    box.appendChild(div);
  });
  box.scrollTop=box.scrollHeight;
}

// Auto-heartbeat ping every 10s
setInterval(async()=>{
  await fetch('/api/v1/fleet/heartbeat',{method:'GET'});
  await refreshDevices();
},10000);

// Initial scroll
const cb=document.getElementById('consoleBox');
if(cb) cb.scrollTop=cb.scrollHeight;
</script>
</body>
</html>`

	tmpl, err := template.New("dash").Funcs(template.FuncMap{
		"not": func(b bool) bool { return !b },
	}).Parse(tmplStr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := struct {
		Devices  map[string]*DeviceInfo
		Manifest *manifest.Manifest
		Logs     []SimulationLog
	}{Devices: state.devices, Manifest: state.manifest, Logs: state.logs}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tmpl.Execute(w, data)
}
