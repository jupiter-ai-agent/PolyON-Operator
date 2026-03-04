package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
)

//go:embed wizard.html
var wizardHTML embed.FS

// SetupConfig holds the wizard form data
type SetupConfig struct {
	Namespace     string `json:"namespace"`
	Domain        string `json:"domain"`
	AdminPassword string `json:"adminPassword"`
	OrgName       string `json:"orgName"`
}

// SetupProgress tracks installation progress
type SetupProgress struct {
	State   string `json:"state"` // fresh, setting_up, running, error
	Step    int    `json:"step"`
	Total   int    `json:"total"`
	Message string `json:"message"`
	Steps   []StepStatus `json:"steps"`
}

type StepStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"` // pending, running, done, error
}

var (
	progress SetupProgress
	mu       sync.Mutex
)

func init() {
	progress = SetupProgress{
		State: "fresh",
		Total: 10,
		Steps: []StepStatus{
			{Name: "Namespace 생성", Status: "pending"},
			{Name: "PostgreSQL", Status: "pending"},
			{Name: "Redis", Status: "pending"},
			{Name: "Elasticsearch", Status: "pending"},
			{Name: "MinIO (Object Storage)", Status: "pending"},
			{Name: "Samba AD DC", Status: "pending"},
			{Name: "Keycloak (SSO)", Status: "pending"},
			{Name: "Stalwart Mail", Status: "pending"},
			{Name: "PolyON Core", Status: "pending"},
			{Name: "PolyON Console", Status: "pending"},
		},
	}
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Wizard UI
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		data, _ := wizardHTML.ReadFile("wizard.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	})

	// Health
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, "ok")
	})

	// Get current state
	http.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(progress)
	})

	// Start setup
	http.HandleFunc("/api/setup", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}

		var cfg SetupConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		mu.Lock()
		if progress.State == "setting_up" {
			mu.Unlock()
			http.Error(w, "setup already in progress", 409)
			return
		}
		progress.State = "setting_up"
		mu.Unlock()

		// Run setup in background
		go runSetup(cfg)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "started"})
	})

	log.Printf("PolyON Operator starting on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func runSetup(cfg SetupConfig) {
	log.Printf("Setup started: domain=%s namespace=%s org=%s", cfg.Domain, cfg.Namespace, cfg.OrgName)

	for i := range progress.Steps {
		mu.Lock()
		progress.Step = i + 1
		progress.Message = progress.Steps[i].Name + " 설치 중..."
		progress.Steps[i].Status = "running"
		mu.Unlock()

		// TODO: 실제 Helm install 로직 구현
		// 현재는 데모용 딜레이
		log.Printf("[%d/%d] %s", i+1, progress.Total, progress.Steps[i].Name)

		mu.Lock()
		progress.Steps[i].Status = "done"
		mu.Unlock()
	}

	mu.Lock()
	progress.State = "running"
	progress.Message = "설치 완료"
	mu.Unlock()

	log.Printf("Setup completed for domain=%s", cfg.Domain)
}
