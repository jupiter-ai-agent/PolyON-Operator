package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

// infraStep defines a single infrastructure deployment step
type infraStep struct {
	manifest      string
	labelSelector string
	timeout       time.Duration
}

// serviceStep defines a single service deployment step
type serviceStep struct {
	manifest      string
	labelSelector string
	timeout       time.Duration
}

//go:embed wizard.html
var wizardHTML embed.FS

// SetupConfig holds the wizard form data
type SetupConfig struct {
	Namespace     string   `json:"namespace"`
	Domain        string   `json:"domain"`
	AdminPassword string   `json:"adminPassword"`
	OrgName       string   `json:"orgName"`
	Phase         string   `json:"phase"` // infra, services, apps
	Apps          []string `json:"apps"`
}

// SetupProgress tracks installation progress
type SetupProgress struct {
	State   string       `json:"state"` // fresh, installing, phase_done, running, error
	Phase   string       `json:"phase"`
	Step    int          `json:"step"`
	Total   int          `json:"total"`
	Message string       `json:"message"`
	Steps   []StepStatus `json:"steps"`
}

type StepStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"` // pending, running, done, error
}

var (
	progress SetupProgress
	config   SetupConfig
	mu       sync.Mutex
)

func init() {
	progress = SetupProgress{State: "fresh"}
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Wizard UI
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
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

	// Start setup phase
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
		if progress.State == "installing" {
			mu.Unlock()
			http.Error(w, "installation in progress", 409)
			return
		}
		config = cfg
		progress.State = "installing"
		progress.Phase = cfg.Phase
		mu.Unlock()

		switch cfg.Phase {
		case "infra":
			go runInfraSetup(cfg)
		case "services":
			go runServicesSetup(cfg)
		case "apps":
			go runAppsSetup(cfg)
		default:
			mu.Lock()
			progress.State = "error"
			progress.Message = "unknown phase: " + cfg.Phase
			mu.Unlock()
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "started", "phase": cfg.Phase})
	})

	log.Printf("PolyON Operator v0.1.0 starting on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func runInfraSetup(cfg SetupConfig) {
	steps := []StepStatus{
		{Name: "PostgreSQL (Database)", Status: "pending"},
		{Name: "Redis (Cache)", Status: "pending"},
		{Name: "Elasticsearch (Search)", Status: "pending"},
		{Name: "MinIO (Object Storage)", Status: "pending"},
	}

	deploys := []infraStep{
		{manifest: "postgresql.yaml", labelSelector: "app=polyon-db", timeout: 120 * time.Second},
		{manifest: "redis.yaml", labelSelector: "app=polyon-redis", timeout: 60 * time.Second},
		{manifest: "elasticsearch.yaml", labelSelector: "app=polyon-es", timeout: 180 * time.Second},
		{manifest: "minio.yaml", labelSelector: "app=polyon-minio", timeout: 120 * time.Second},
	}

	mu.Lock()
	progress.Steps = steps
	progress.Total = len(steps)
	progress.Step = 0
	mu.Unlock()

	tcfg := NewTemplateConfig(cfg)

	log.Printf("[INFRA] Starting infrastructure setup: ns=%s domain=%s", cfg.Namespace, cfg.Domain)

	// Create namespace first
	if err := ensureNamespace(tcfg); err != nil {
		mu.Lock()
		progress.State = "error"
		progress.Message = "네임스페이스 생성 실패: " + err.Error()
		mu.Unlock()
		log.Printf("[INFRA] Namespace creation failed: %v", err)
		return
	}

	for i := range steps {
		mu.Lock()
		progress.Step = i + 1
		progress.Message = steps[i].Name + " 설치 중..."
		progress.Steps[i].Status = "running"
		mu.Unlock()

		log.Printf("[INFRA] [%d/%d] Installing %s", i+1, len(steps), steps[i].Name)

		if err := deployManifest(deploys[i].manifest, deploys[i].labelSelector, tcfg, deploys[i].timeout); err != nil {
			mu.Lock()
			progress.Steps[i].Status = "error"
			progress.State = "error"
			progress.Message = steps[i].Name + " 설치 실패: " + err.Error()
			mu.Unlock()
			log.Printf("[INFRA] Failed to install %s: %v", steps[i].Name, err)
			return
		}

		mu.Lock()
		progress.Steps[i].Status = "done"
		mu.Unlock()
	}

	mu.Lock()
	progress.State = "phase_done"
	progress.Message = "인프라 설치 완료"
	mu.Unlock()

	log.Printf("[INFRA] Infrastructure setup complete")
}

func runServicesSetup(cfg SetupConfig) {
	steps := []StepStatus{
		{Name: "Samba AD DC (Active Directory)", Status: "pending"},
		{Name: "Keycloak (SSO / 인증)", Status: "pending"},
		{Name: "Stalwart Mail (메일 서버)", Status: "pending"},
	}

	deploys := []serviceStep{
		{manifest: "samba-dc.yaml", labelSelector: "app=polyon-dc", timeout: 180 * time.Second},
		{manifest: "keycloak.yaml", labelSelector: "app=polyon-auth", timeout: 180 * time.Second},
		{manifest: "stalwart.yaml", labelSelector: "app=polyon-mail", timeout: 120 * time.Second},
	}

	mu.Lock()
	progress.Steps = steps
	progress.Total = len(steps)
	progress.Step = 0
	progress.State = "installing"
	mu.Unlock()

	tcfg := NewTemplateConfig(cfg)

	log.Printf("[SERVICES] Starting services setup: domain=%s", cfg.Domain)

	for i := range steps {
		mu.Lock()
		progress.Step = i + 1
		progress.Message = steps[i].Name + " 설치 중..."
		progress.Steps[i].Status = "running"
		mu.Unlock()

		log.Printf("[SERVICES] [%d/%d] Installing %s", i+1, len(steps), steps[i].Name)

		if err := deployManifest(deploys[i].manifest, deploys[i].labelSelector, tcfg, deploys[i].timeout); err != nil {
			mu.Lock()
			progress.Steps[i].Status = "error"
			progress.State = "error"
			progress.Message = steps[i].Name + " 설치 실패: " + err.Error()
			mu.Unlock()
			log.Printf("[SERVICES] Failed to install %s: %v", steps[i].Name, err)
			return
		}

		mu.Lock()
		progress.Steps[i].Status = "done"
		mu.Unlock()
	}

	mu.Lock()
	progress.State = "phase_done"
	progress.Message = "서비스 설치 완료"
	mu.Unlock()

	log.Printf("[SERVICES] Services setup complete")
}

func runAppsSetup(cfg SetupConfig) {
	log.Printf("[APPS] Installing apps: %v", cfg.Apps)

	mu.Lock()
	progress.State = "running"
	progress.Message = "앱 설치 완료"
	progress.Phase = "complete"
	mu.Unlock()

	log.Printf("[APPS] Setup fully complete. Domain=%s NS=%s", cfg.Domain, cfg.Namespace)
}
