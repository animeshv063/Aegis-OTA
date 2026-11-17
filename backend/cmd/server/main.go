package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/animeshv063/Aegis-OTA/agent/pkg/flasher"
	"github.com/animeshv063/Aegis-OTA/agent/pkg/health"
	"github.com/animeshv063/Aegis-OTA/agent/pkg/manifest"
	"github.com/animeshv063/Aegis-OTA/agent/pkg/partition"
	"github.com/animeshv063/Aegis-OTA/agent/pkg/uboot"
)

type DeviceInfo struct {
	DeviceID      string `json:"device_id"`
	HardwareModel string `json:"hardware_model"`
	CurrentSlot   string `json:"current_slot"`
	CurrentVersion string `json:"current_version"`
	LastSeen      time.Time `json:"last_seen"`
	Status        string `json:"status"`
}

type ServerState struct {
	mu           sync.RWMutex
	devices      map[string]*DeviceInfo
	manifest     *manifest.Manifest
	imageBytes   []byte
	pubKeyHex    string
	privKeyHex   string
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
		ImageName:      "aegis-nas-os-v1.2.0.img",
		TargetArch:     "arm64",
		SizeBytes:      int64(len(dummyPayload)),
		BuildTimestamp: time.Now().UTC(),
		Metadata: map[string]string{
			"channel": "stable",
			"storage": "zfs",
		},
	}
	_ = m.Sign(privHex)

	return &ServerState{
		devices:    make(map[string]*DeviceInfo),
		manifest:   m,
		imageBytes: dummyPayload,
		pubKeyHex:  pubHex,
		privKeyHex: privHex,
	}
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	state := newServerState()

	http.HandleFunc("/api/v1/ota/manifest", func(w http.ResponseWriter, r *http.Request) {
		state.mu.RLock()
		defer state.mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(state.manifest)
	})

	http.HandleFunc("/api/v1/ota/image", func(w http.ResponseWriter, r *http.Request) {
		state.mu.RLock()
		defer state.mu.RUnlock()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename=aegis-nas-os-v1.2.0.img")
		_, _ = w.Write(state.imageBytes)
	})

	http.HandleFunc("/api/v1/fleet/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var dev DeviceInfo
		if err := json.NewDecoder(r.Body).Decode(&dev); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		dev.LastSeen = time.Now()

		state.mu.Lock()
		state.devices[dev.DeviceID] = &dev
		state.mu.Unlock()

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"acknowledged"}`))
	})

	http.HandleFunc("/api/v1/fleet/devices", func(w http.ResponseWriter, r *http.Request) {
		state.mu.RLock()
		defer state.mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(state.devices)
	})

	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	log.Printf("Aegis OTA Backend Management Service listening on :%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("server terminated: %v", err)
	}
}
