package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

// ── Types ────────────────────────────────────────────────────────────────────

type Manifest struct {
	Version        string            `json:"version"`
	ImageName      string            `json:"image"`
	SHA256         string            `json:"sha256"`
	TargetArch     string            `json:"target_arch"`
	Signature      string            `json:"signature"`
	BuildTimestamp time.Time         `json:"build_timestamp"`
	Metadata       map[string]string `json:"metadata"`
}

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

type SimLog struct {
	Timestamp string `json:"timestamp"`
	Source    string `json:"source"`
	Level     string `json:"level"`
	Message   string `json:"message"`
}

type State struct {
	mu       sync.RWMutex
	devices  map[string]*DeviceInfo
	manifest *Manifest
	pubHex   string
	logs     []SimLog
}

// ── Init ─────────────────────────────────────────────────────────────────────

func newState() *State {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		log.Fatalf("keygen: %v", err)
	}
	payload := []byte("AEGIS-NAS-OS-IMMUTABLE-IMAGE-V1.2.0")
	sig := ed25519.Sign(priv, payload)

	m := &Manifest{
		Version:        "1.2.0",
		ImageName:      "aegis-nas-os-v1.2.0-arm64.rootfs.img",
		SHA256:         "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		TargetArch:     "arm64",
		Signature:      hex.EncodeToString(sig),
		BuildTimestamp: time.Now().UTC(),
		Metadata: map[string]string{
			"builder":       "Yocto Scarthgap 5.0",
			"kernel":        "6.6.30-aegis-nas",
			"storage_stack": "zfs-2.2.3",
		},
	}

	devices := map[string]*DeviceInfo{
		"nas-alpha-01": {
			DeviceID: "nas-alpha-01", HardwareModel: "Rockchip RK3588 Quad-SATA",
			CurrentSlot: "A", CurrentVersion: "1.1.0", BootCount: 0, BootLimit: 3,
			HealthStatus: "HEALTHY", LastSeen: time.Now(),
			StoragePool: "ZFS RAIDZ2 (4x 8TB)", Uptime: "12d 4h 33m",
		},
		"nas-bravo-02": {
			DeviceID: "nas-bravo-02", HardwareModel: "Intel Atom x6413E 8-Bay NAS",
			CurrentSlot: "A", CurrentVersion: "1.1.0", BootCount: 0, BootLimit: 3,
			HealthStatus: "HEALTHY", LastSeen: time.Now(),
			StoragePool: "ZFS Mirror (2x 16TB)", Uptime: "7d 2h 11m",
		},
		"nas-charlie-03": {
			DeviceID: "nas-charlie-03", HardwareModel: "Ampere Altra Edge Storage",
			CurrentSlot: "A", CurrentVersion: "1.1.0", BootCount: 0, BootLimit: 3,
			HealthStatus: "HEALTHY", LastSeen: time.Now(),
			StoragePool: "NVMe-oF Pool", Uptime: "3d 9h 55m",
		},
	}

	logs := []SimLog{
		{time.Now().Add(-5 * time.Minute).Format("15:04:05"), "U-BOOT", "INFO", "Dual A/B bootloader initialized. Slot A verified."},
		{time.Now().Add(-4 * time.Minute).Format("15:04:05"), "AGENT", "INFO", "Aegis edge daemon listening for signed release manifests."},
		{time.Now().Add(-3 * time.Minute).Format("15:04:05"), "WATCHDOG", "INFO", "Hardware watchdog armed. Post-boot self-healing online."},
	}

	return &State{devices: devices, manifest: m, pubHex: hex.EncodeToString(pub), logs: logs}
}

func (s *State) addLog(src, level, msg string) {
	s.logs = append(s.logs, SimLog{time.Now().Format("15:04:05"), src, level, msg})
	if len(s.logs) > 60 {
		s.logs = s.logs[len(s.logs)-60:]
	}
}

// ── Main ─────────────────────────────────────────────────────────────────────

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	st := newState()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		st.mu.RLock()
		defer st.mu.RUnlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, buildHTML(st))
	})

	http.HandleFunc("/api/v1/ota/manifest", func(w http.ResponseWriter, r *http.Request) {
		st.mu.RLock()
		defer st.mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(st.manifest)
	})

	http.HandleFunc("/api/v1/fleet/devices", func(w http.ResponseWriter, r *http.Request) {
		st.mu.RLock()
		defer st.mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(st.devices)
	})

	http.HandleFunc("/api/v1/fleet/simulate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		action := r.URL.Query().Get("action")
		nodeID := r.URL.Query().Get("node")

		st.mu.Lock()
		defer st.mu.Unlock()

		targets := []string{nodeID}
		if nodeID == "all" {
			targets = []string{"nas-alpha-01", "nas-bravo-02", "nas-charlie-03"}
			st.addLog("CONTROLLER", "INFO", "[BATCH] Initiating fleet-wide OTA deployment to all 3 nodes...")
		}

		for _, id := range targets {
			dev, ok := st.devices[id]
			if !ok {
				continue
			}
			dev.LastSeen = time.Now()

			passive := "B"
			if dev.CurrentSlot == "B" {
				passive = "A"
			}

			switch action {
			case "flash_update":
				dev.UpgradePending = true
				dev.HealthStatus = "UPGRADE_PENDING"
				dev.BootCount = 0
				dev.CurrentVersion = st.manifest.Version
				st.addLog("AGENT", "INFO", fmt.Sprintf("[%s] Ed25519 sig verified OK. Streaming 500MB image (v%s) to passive Slot %s...", id, st.manifest.Version, passive))
				st.addLog("U-BOOT", "WARN", fmt.Sprintf("[%s] fw_setenv active_slot=%s upgrade_available=1 bootcount=0", id, passive))

			case "simulate_panic":
				dev.BootCount++
				if dev.BootCount < dev.BootLimit {
					dev.HealthStatus = fmt.Sprintf("CRASH (ATTEMPT %d/%d)", dev.BootCount, dev.BootLimit)
					st.addLog("KERNEL", "ERROR", fmt.Sprintf("[%s] Kernel panic — VFS: unable to mount rootfs! (Attempt %d/%d)", id, dev.BootCount, dev.BootLimit))
					st.addLog("WATCHDOG", "WARN", fmt.Sprintf("[%s] Watchdog timer expired! Hard power-cycle rebooted board. Bootcount = %d/%d", id, dev.BootCount, dev.BootLimit))
				} else {
					st.addLog("KERNEL", "ERROR", fmt.Sprintf("[%s] Kernel panic — fatal crash on Attempt 3/3!", id))
					st.addLog("WATCHDOG", "CRITICAL", fmt.Sprintf("[%s] Hardware watchdog triggered board power-cycle!", id))
					st.addLog("U-BOOT", "CRITICAL", fmt.Sprintf("[%s] BOOTCOUNT (3/3) EXCEEDED BOOTLIMIT! Triggering ALTBOOTCMD auto-rollback!", id))
					dev.BootCount = 0
					dev.UpgradePending = false
					dev.HealthStatus = "REVERTED_TO_STABLE"
					dev.CurrentVersion = "1.1.0"
					dev.CurrentSlot = "A"
					st.addLog("U-BOOT", "SUCCESS", fmt.Sprintf("[%s] Auto-Rollback complete! Back to Slot A (v1.1.0). Zero-brick self-healing verified.", id))
				}

			case "simulate_success":
				dev.CurrentSlot = passive
				dev.CurrentVersion = st.manifest.Version
				dev.UpgradePending = false
				dev.BootCount = 0
				dev.HealthStatus = "HEALTHY"
				st.addLog("HEALTH", "PASS", fmt.Sprintf("[%s] ZFS pool imported, 10GbE link up, all daemons active.", id))
				st.addLog("AGENT", "SUCCESS", fmt.Sprintf("[%s] Slot %s permanently committed in U-Boot env.", id, passive))
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "logs": st.logs})
	})

	http.HandleFunc("/api/v1/fleet/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		st.mu.Lock()
		for _, d := range st.devices {
			d.LastSeen = time.Now()
		}
		st.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = fmt.Fprint(w, "ok")
	})

	log.Printf("Aegis OTA server :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

// ── HTML Builder ──────────────────────────────────────────────────────────────

func buildHTML(st *State) string {
	// Build node cards HTML
	nodeOrder := []string{"nas-alpha-01", "nas-bravo-02", "nas-charlie-03"}
	nodeCardsHTML := ""
	for _, id := range nodeOrder {
		dev, ok := st.devices[id]
		if !ok {
			continue
		}
		slotClass := "slot-a"
		if dev.CurrentSlot == "B" {
			slotClass = "slot-b"
		}
		statusColor := "#10b981"
		if dev.HealthStatus != "HEALTHY" {
			statusColor = "#f59e0b"
		}
		nodeCardsHTML += fmt.Sprintf(`
<div class="node-card" id="card-%s">
  <div class="node-top">
    <div class="node-id">%s</div>
    <div class="slot-pill %s" id="slot-%s">BOOT SLOT %s</div>
  </div>
  <div class="node-meta">
    <div>Hardware: <span>%s</span></div>
    <div>Firmware: <span id="ver-%s">v%s</span></div>
    <div>Storage: <span>%s</span></div>
    <div>Bootcount: <span id="bc-%s">%d/%d</span></div>
    <div>Uptime: <span>%s</span></div>
    <div>Status: <span id="status-%s" style="color:%s;font-weight:700">%s</span></div>
  </div>
  <div class="node-actions">
    <button class="nbtn btn-flash" onclick="runAction('%s','flash_update')">&#9654; Stream to Passive Slot</button>
    <button class="nbtn btn-panic" onclick="runAction('%s','simulate_panic')">&#9888; Panic &amp; Rollback</button>
    <button class="nbtn btn-commit" onclick="runAction('%s','simulate_success')">&#10003; Health &amp; Commit</button>
  </div>
  <div class="prog-wrap" id="prog-%s">
    <div class="prog-label" id="prog-lbl-%s">Streaming... 0%%</div>
    <div class="prog-bg"><div class="prog-fill" id="prog-fill-%s"></div></div>
  </div>
</div>`,
			id, id, slotClass, id, dev.CurrentSlot,
			dev.HardwareModel,
			id, dev.CurrentVersion,
			dev.StoragePool,
			id, dev.BootCount, dev.BootLimit,
			dev.Uptime,
			id, statusColor, dev.HealthStatus,
			id, id, id,
			id, id, id,
		)
	}

	// Build initial console logs HTML
	logsHTML := ""
	for _, l := range st.logs {
		logsHTML += fmt.Sprintf(`<div class="log-entry"><span class="log-ts">[%s]</span> <span class="src-%s">[%s]</span>: <span class="lvl-%s">%s</span></div>`,
			l.Timestamp, l.Source, l.Source, l.Level, l.Message)
	}

	manifestJSON, _ := json.MarshalIndent(st.manifest, "", "  ")

	return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1.0">
<title>Aegis OTA | Fleet Operations Dashboard</title>
<link href="https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;600;700&family=Plus+Jakarta+Sans:wght@400;600;700;800&display=swap" rel="stylesheet">
<style>
:root{--bg:#090d16;--card:#111726;--border:#1e293b;--primary:#38bdf8;--success:#10b981;--warn:#f59e0b;--danger:#ef4444;--text:#f8fafc;--muted:#94a3b8}
*{box-sizing:border-box;margin:0;padding:0}
body{background:var(--bg);color:var(--text);font-family:'Plus Jakarta Sans',sans-serif;min-height:100vh;padding:1.5rem}
.wrap{max-width:1400px;margin:0 auto}
header{display:flex;justify-content:space-between;align-items:center;padding-bottom:1.5rem;border-bottom:1px solid var(--border);margin-bottom:1.5rem}
.brand h1{font-size:1.4rem;font-weight:800}
.brand p{font-size:.8rem;color:var(--muted);margin-top:.2rem}
.badge-live{display:inline-flex;align-items:center;gap:.5rem;padding:.4rem .9rem;border-radius:9999px;background:rgba(16,185,129,.1);border:1px solid rgba(16,185,129,.3);color:var(--success);font-size:.8rem;font-weight:600}
.dot{width:8px;height:8px;border-radius:50%;background:var(--success);animation:blink 1.4s infinite}
@keyframes blink{0%,100%{opacity:.4}50%{opacity:1}}
.stats{display:grid;grid-template-columns:repeat(4,1fr);gap:1rem;margin-bottom:1.5rem}
.stat{background:var(--card);border:1px solid var(--border);border-radius:14px;padding:1.1rem 1.4rem}
.stat-lbl{font-size:.72rem;text-transform:uppercase;letter-spacing:.5px;color:var(--muted);margin-bottom:.4rem}
.stat-val{font-size:1.6rem;font-weight:800;font-family:'JetBrains Mono',monospace}
.layout{display:grid;grid-template-columns:1fr 360px;gap:1.5rem}
@media(max-width:1050px){.layout{grid-template-columns:1fr}.stats{grid-template-columns:repeat(2,1fr)}}
.panel{background:var(--card);border:1px solid var(--border);border-radius:14px;padding:1.4rem;margin-bottom:1rem}
.panel-title{font-size:1rem;font-weight:700;margin-bottom:1.1rem}
/* Batch panel */
.batch-box{background:#0a0f1c;border:1px solid rgba(245,158,11,.25);border-radius:10px;padding:1rem;margin-bottom:1.2rem}
.batch-title{font-size:.85rem;font-weight:700;color:var(--warn);margin-bottom:.8rem}
.batch-btns{display:grid;grid-template-columns:repeat(3,1fr);gap:.5rem}
/* Node cards */
.node-card{background:#0a0f1c;border:1px solid #1a2234;border-radius:10px;padding:1.1rem;margin-bottom:.9rem;transition:border-color .3s,box-shadow .3s}
.node-card.panic-anim{border-color:var(--danger)!important;box-shadow:0 0 18px rgba(239,68,68,.35)!important;animation:panicPulse .55s infinite}
.node-card.pending-anim{border-color:var(--warn)!important}
@keyframes panicPulse{0%,100%{opacity:1}50%{opacity:.55}}
.node-top{display:flex;justify-content:space-between;align-items:center;margin-bottom:.8rem}
.node-id{font-family:'JetBrains Mono',monospace;font-weight:700;font-size:1rem}
.slot-pill{font-size:.7rem;font-weight:700;padding:.2rem .6rem;border-radius:6px;font-family:'JetBrains Mono',monospace}
.slot-a{background:rgba(56,189,248,.12);color:#38bdf8;border:1px solid rgba(56,189,248,.35)}
.slot-b{background:rgba(168,85,247,.12);color:#c084fc;border:1px solid rgba(168,85,247,.35)}
.node-meta{display:grid;grid-template-columns:repeat(3,1fr);gap:.4rem;font-size:.77rem;color:var(--muted);margin-bottom:1rem}
.node-meta span{color:var(--text);font-weight:600}
.node-actions{display:grid;grid-template-columns:repeat(3,1fr);gap:.5rem}
.nbtn{padding:.45rem .3rem;border-radius:7px;border:1px solid transparent;font-size:.72rem;font-weight:700;cursor:pointer;font-family:'Plus Jakarta Sans',sans-serif;text-align:center;transition:all .15s}
.btn-flash{background:rgba(56,189,248,.12);border-color:rgba(56,189,248,.35);color:#7dd3fc}
.btn-flash:hover{background:rgba(56,189,248,.28)}
.btn-panic{background:rgba(239,68,68,.1);border-color:rgba(239,68,68,.35);color:#fca5a5}
.btn-panic:hover{background:rgba(239,68,68,.25)}
.btn-commit{background:rgba(16,185,129,.1);border-color:rgba(16,185,129,.35);color:#86efac}
.btn-commit:hover{background:rgba(16,185,129,.25)}
/* Progress bar */
.prog-wrap{margin-top:.8rem;display:none}
.prog-label{font-size:.72rem;color:var(--muted);margin-bottom:.3rem;font-family:'JetBrains Mono',monospace}
.prog-bg{background:#0d1526;border-radius:999px;height:8px;overflow:hidden}
.prog-fill{height:8px;border-radius:999px;background:linear-gradient(90deg,#0284c7,#38bdf8);width:0%;transition:width .25s}
/* Console */
.console{background:#06090e;border:1px solid #1a2234;border-radius:10px;padding:.9rem;font-family:'JetBrains Mono',monospace;font-size:.74rem;height:300px;overflow-y:auto}
.log-entry{margin-bottom:.35rem;line-height:1.5}
.log-ts{color:#475569}
.src-U-BOOT{color:#38bdf8;font-weight:700}
.src-AGENT{color:#a78bfa;font-weight:700}
.src-WATCHDOG{color:#fb923c;font-weight:700}
.src-KERNEL{color:#f87171;font-weight:700}
.src-HEALTH{color:#4ade80;font-weight:700}
.src-CONTROLLER{color:#facc15;font-weight:700}
.lvl-CRITICAL,.lvl-ERROR{color:#f87171;font-weight:700}
.lvl-SUCCESS,.lvl-PASS{color:#4ade80;font-weight:700}
.lvl-WARN{color:#fbbf24}
.lvl-INFO{color:#cbd5e1}
.manifest-box{background:#06090e;border:1px solid #1a2234;border-radius:10px;padding:.9rem;font-family:'JetBrains Mono',monospace;font-size:.7rem;color:#94a3b8;overflow-x:auto;margin-top:1rem;white-space:pre-wrap;word-break:break-all}
footer{text-align:center;margin-top:2rem;font-size:.77rem;color:#475569;border-top:1px solid var(--border);padding-top:1.2rem}
</style>
</head>
<body>
<div class="wrap">

<header>
  <div class="brand">
    <h1>&#x26E8; Aegis OTA Fleet Operations</h1>
    <p>Zero-Brick Fail-Safe Deployment Engine for NAS Storage Clusters</p>
  </div>
  <div class="badge-live"><span class="dot"></span>FLEET ACTIVE &bull; WATCHDOG ARMED &bull; <span id="clk">--:--:--</span></div>
</header>

<div class="stats">
  <div class="stat"><div class="stat-lbl">Total Connected Nodes</div><div class="stat-val">3 <span style="font-size:.9rem;color:var(--muted)">Units</span></div></div>
  <div class="stat"><div class="stat-lbl">Cryptographic Protection</div><div class="stat-val" style="color:var(--primary)">Ed25519</div></div>
  <div class="stat"><div class="stat-lbl">Active Partitions</div><div class="stat-val">A / B <span style="font-size:.9rem;color:var(--success)">Immutable</span></div></div>
  <div class="stat"><div class="stat-lbl">Bootloader Rollback</div><div class="stat-val" style="color:var(--success)">Auto-Armed</div></div>
</div>

<div class="layout">
<div>

  <!-- Batch OTA -->
  <div class="panel">
    <div class="panel-title">&#9889; Batch Fleet OTA &mdash; All 3 Nodes Simultaneously</div>
    <div class="batch-box">
      <div class="batch-title">&#9888; Fires all three NAS units at once</div>
      <div class="batch-btns">
        <button class="nbtn btn-flash" onclick="runAction('all','flash_update')">&#9654; Batch Stream to Passive Slot</button>
        <button class="nbtn btn-panic" onclick="runAction('all','simulate_panic')">&#9888; Batch Panic &amp; Rollback</button>
        <button class="nbtn btn-commit" onclick="runAction('all','simulate_success')">&#10003; Batch Health &amp; Commit</button>
      </div>
    </div>
  </div>

  <!-- Per-node cards -->
  <div class="panel">
    <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:1.1rem">
      <div class="panel-title" style="margin:0">Active NAS Fleet Units</div>
      <span style="font-size:.7rem;color:var(--muted);font-family:'JetBrains Mono',monospace">Live Telemetry &bull; <span id="hb">--:--:--</span></span>
    </div>
` + nodeCardsHTML + `
    <div style="margin-top:1.4rem">
      <div class="panel-title">Active Signed Manifest (v` + st.manifest.Version + `)</div>
      <div class="manifest-box">` + string(manifestJSON) + `</div>
    </div>
  </div>

</div>

<!-- Console -->
<div>
  <div class="panel">
    <div class="panel-title">U-Boot &amp; Agent Console</div>
    <div class="console" id="con">
` + logsHTML + `
    </div>
  </div>
</div>
</div>

<footer>Aegis OTA &bull; Designed for Physical Edge &amp; NAS Hardware Fleets &bull; Animesh Verma &amp; Team</footer>
</div>

<script>
// Live clock
function tick(){
  var t=new Date().toTimeString().slice(0,8);
  document.getElementById('clk').textContent=t;
  document.getElementById('hb').textContent=t;
}
tick(); setInterval(tick,1000);

// Animated progress bar
function animProg(id, cb){
  var w=document.getElementById('prog-'+id);
  var f=document.getElementById('prog-fill-'+id);
  var l=document.getElementById('prog-lbl-'+id);
  if(!w){cb();return;}
  w.style.display='block';
  var steps=[2,5,9,14,20,27,35,43,51,59,66,73,79,84,89,93,96,98,100];
  var i=0;
  var iv=setInterval(function(){
    if(i>=steps.length){
      clearInterval(iv);
      l.textContent='Flash complete. SHA-256 verified \u2713';
      setTimeout(function(){w.style.display='none';f.style.width='0%';cb();},1200);
      return;
    }
    f.style.width=steps[i]+'%';
    l.textContent='Streaming image to passive partition... '+steps[i]+'%';
    i++;
  },160);
}

// Panic countdown
function animPanic(id, cb){
  var card=document.getElementById('card-'+id);
  var st=document.getElementById('status-'+id);
  if(card) card.classList.add('panic-anim');
  if(st){st.style.color='#f87171';}
  var n=5;
  var iv=setInterval(function(){
    if(st) st.textContent='WATCHDOG COUNTDOWN: '+n+'s';
    n--;
    if(n<0){
      clearInterval(iv);
      if(card) card.classList.remove('panic-anim');
      if(st){st.style.color='#10b981';}
      cb();
    }
  },600);
}

async function runAction(nodeId, action){
  var ids=nodeId==='all'?['nas-alpha-01','nas-bravo-02','nas-charlie-03']:[nodeId];

  if(action==='flash_update'){
    ids.forEach(function(id){
      var c=document.getElementById('card-'+id);
      if(c) c.classList.add('pending-anim');
    });
    var done=0;
    ids.forEach(function(id){
      animProg(id, async function(){
        done++;
        if(done===ids.length){
          await doPost(nodeId,action);
          ids.forEach(function(i){var c=document.getElementById('card-'+i);if(c)c.classList.remove('pending-anim');});
        }
      });
    });
    return;
  }

  if(action==='simulate_panic'){
    var done2=0;
    ids.forEach(function(id){
      animPanic(id, async function(){
        done2++;
        if(done2===ids.length){
          await doPost(nodeId,action);
        }
      });
    });
    return;
  }

  await doPost(nodeId, action);
}

async function doPost(nodeId, action){
  try{
    var r=await fetch('/api/v1/fleet/simulate?node='+nodeId+'&action='+action,{method:'POST'});
    if(r.ok){
      var d=await r.json();
      renderLogs(d.logs);
      await refreshNodes();
    }
  }catch(e){console.error(e);}
}

async function refreshNodes(){
  try{
    var r=await fetch('/api/v1/fleet/devices');
    var devs=await r.json();
    Object.keys(devs).forEach(function(id){
      var dev=devs[id];
      var slotEl=document.getElementById('slot-'+id);
      var verEl=document.getElementById('ver-'+id);
      var stEl=document.getElementById('status-'+id);
      var bcEl=document.getElementById('bc-'+id);
      if(slotEl){
        slotEl.textContent='BOOT SLOT '+dev.current_slot;
        slotEl.className='slot-pill '+(dev.current_slot==='A'?'slot-a':'slot-b');
      }
      if(verEl) verEl.textContent='v'+dev.current_version;
      if(stEl && stEl.textContent.indexOf('WATCHDOG')===-1){
        stEl.textContent=dev.health_status;
        if(dev.health_status==='HEALTHY' || dev.health_status==='REVERTED_TO_STABLE'){
          stEl.style.color='#10b981';
        } else if(dev.health_status==='UPGRADE_PENDING'){
          stEl.style.color='#f59e0b';
        } else {
          stEl.style.color='#ef4444';
        }
      }
      if(bcEl) bcEl.textContent=dev.boot_count+'/'+dev.boot_limit;
    });
  }catch(e){console.error(e);}
}

function renderLogs(logs){
  if(!logs||!logs.length) return;
  var box=document.getElementById('con');
  var srcC={'U-BOOT':'#38bdf8','AGENT':'#a78bfa','WATCHDOG':'#fb923c','KERNEL':'#f87171','HEALTH':'#4ade80','CONTROLLER':'#facc15'};
  var lvlC={'CRITICAL':'#f87171','ERROR':'#f87171','SUCCESS':'#4ade80','PASS':'#4ade80','WARN':'#fbbf24','INFO':'#cbd5e1'};
  box.innerHTML='';
  logs.forEach(function(log){
    var d=document.createElement('div');
    d.className='log-entry';
    var sc=srcC[log.source]||'#94a3b8';
    var lc=lvlC[log.level]||'#cbd5e1';
    var bold=['CRITICAL','ERROR','SUCCESS','PASS'].indexOf(log.level)>-1?700:400;
    d.innerHTML='<span class="log-ts">['+log.timestamp+']</span> <span style="color:'+sc+';font-weight:700">['+log.source+']</span>: <span style="color:'+lc+';font-weight:'+bold+'">'+log.message+'</span>';
    box.appendChild(d);
  });
  box.scrollTop=box.scrollHeight;
}

// Auto-heartbeat every 10s
setInterval(async function(){
  await fetch('/api/v1/fleet/heartbeat');
  await refreshNodes();
},10000);

document.getElementById('con').scrollTop=99999;
</script>
</body>
</html>`
}
