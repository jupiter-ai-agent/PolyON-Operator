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

// SubdomainConfig holds subdomain prefix overrides
type SubdomainConfig struct {
	Console string `json:"console"` // default: "console"
	Auth    string `json:"auth"`    // default: "auth"
	Mail    string `json:"mail"`    // default: "mail"
	Portal  string `json:"portal"`  // default: "portal"
}

// SetupConfig holds the wizard form data
type SetupConfig struct {
	Namespace          string          `json:"namespace"`
	Domain             string          `json:"domain"`
	AdminPassword      string          `json:"adminPassword"`
	ConsoleAdminPassword string        `json:"consoleAdminPassword"`
	OrgName            string          `json:"orgName"`
	Phase              string          `json:"phase"` // infra, services
	Subdomains         SubdomainConfig `json:"subdomains"`
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
	Name      string `json:"name"`
	Status    string `json:"status"` // pending, running, done, error
	StartedAt int64  `json:"startedAt,omitempty"`
	DoneAt    int64  `json:"doneAt,omitempty"`
	Error     string `json:"error,omitempty"`
}

// LogEntry represents a single log message
type LogEntry struct {
	Time    int64  `json:"time"`
	Level   string `json:"level"` // info, error, success
	Message string `json:"message"`
}

var (
	progress SetupProgress
	config   SetupConfig
	mu       sync.Mutex

	logBuffer []LogEntry
	logMu     sync.Mutex
)

const maxLogLines = 100

func appendLog(level, message string) {
	logMu.Lock()
	defer logMu.Unlock()
	entry := LogEntry{
		Time:    time.Now().UnixMilli(),
		Level:   level,
		Message: message,
	}
	logBuffer = append(logBuffer, entry)
	if len(logBuffer) > maxLogLines {
		logBuffer = logBuffer[len(logBuffer)-maxLogLines:]
	}
	log.Printf("[%s] %s", level, message)
}

func init() {
	progress = SetupProgress{State: "fresh"}
	logBuffer = make([]LogEntry, 0, maxLogLines)
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

	// Get logs
	http.HandleFunc("/api/logs", func(w http.ResponseWriter, r *http.Request) {
		logMu.Lock()
		defer logMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(logBuffer)
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
		default:
			mu.Lock()
			progress.State = "error"
			progress.Message = "unknown phase: " + cfg.Phase
			mu.Unlock()
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "started", "phase": cfg.Phase})
	})

	log.Printf("PolyON Operator v0.4.0 starting on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func runInfraSetup(cfg SetupConfig) {
	steps := []StepStatus{
		{Name: "PostgreSQL (Database)", Status: "pending"},
		{Name: "Redis (Cache)", Status: "pending"},
		{Name: "OpenSearch (Search)", Status: "pending"},
		{Name: "RustFS (Object Storage)", Status: "pending"},
	}

	deploys := []infraStep{
		{manifest: "postgresql.yaml", labelSelector: "app=polyon-db", timeout: 120 * time.Second},
		{manifest: "redis.yaml", labelSelector: "app=polyon-redis", timeout: 60 * time.Second},
		{manifest: "opensearch.yaml", labelSelector: "app=polyon-search", timeout: 180 * time.Second},
		{manifest: "rustfs.yaml", labelSelector: "app=polyon-rustfs", timeout: 120 * time.Second},
	}

	mu.Lock()
	progress.Steps = steps
	progress.Total = len(steps)
	progress.Step = 0
	mu.Unlock()

	tcfg := NewTemplateConfig(cfg)

	appendLog("info", fmt.Sprintf("인프라 설치 시작: ns=%s domain=%s", cfg.Namespace, cfg.Domain))

	// Create namespace first
	if err := ensureNamespace(tcfg); err != nil {
		mu.Lock()
		progress.State = "error"
		progress.Message = "네임스페이스 생성 실패: " + err.Error()
		mu.Unlock()
		appendLog("error", "네임스페이스 생성 실패: "+err.Error())
		return
	}
	appendLog("success", "네임스페이스 생성 완료")

	// Create TLS secret (self-signed wildcard cert)
	if err := ensureTLSSecret(tcfg); err != nil {
		mu.Lock()
		progress.State = "error"
		progress.Message = "TLS 인증서 생성 실패: " + err.Error()
		mu.Unlock()
		appendLog("error", "TLS 인증서 생성 실패: "+err.Error())
		return
	}
	appendLog("success", fmt.Sprintf("TLS 인증서 생성 완료 (*.%s)", cfg.Domain))

	// Deploy common ConfigMap and Secret (must be before any service)
	appendLog("info", "공통 ConfigMap/Secret 생성 중...")
	if err := deployManifest("common-config.yaml", "", tcfg, 0); err != nil {
		mu.Lock()
		progress.State = "error"
		progress.Message = "공통 ConfigMap 생성 실패: " + err.Error()
		mu.Unlock()
		appendLog("error", "공통 ConfigMap 생성 실패: "+err.Error())
		return
	}
	if err := deployManifest("common-secret.yaml", "", tcfg, 0); err != nil {
		mu.Lock()
		progress.State = "error"
		progress.Message = "공통 Secret 생성 실패: " + err.Error()
		mu.Unlock()
		appendLog("error", "공통 Secret 생성 실패: "+err.Error())
		return
	}
	appendLog("success", "공통 ConfigMap/Secret 생성 완료 (서비스별 독립 비밀번호 적용)")

	for i := range steps {
		now := time.Now().UnixMilli()
		mu.Lock()
		progress.Step = i + 1
		progress.Message = steps[i].Name + " 설치 중..."
		progress.Steps[i].Status = "running"
		progress.Steps[i].StartedAt = now
		mu.Unlock()

		appendLog("info", fmt.Sprintf("[%d/%d] %s 설치 중...", i+1, len(steps), steps[i].Name))

		if err := deployManifest(deploys[i].manifest, deploys[i].labelSelector, tcfg, deploys[i].timeout); err != nil {
			mu.Lock()
			progress.Steps[i].Status = "error"
			progress.Steps[i].Error = err.Error()
			progress.State = "error"
			progress.Message = steps[i].Name + " 설치 실패: " + err.Error()
			mu.Unlock()
			appendLog("error", steps[i].Name+" 설치 실패: "+err.Error())
			return
		}

		doneAt := time.Now().UnixMilli()
		mu.Lock()
		progress.Steps[i].Status = "done"
		progress.Steps[i].DoneAt = doneAt
		mu.Unlock()
		elapsed := (doneAt - now) / 1000
		appendLog("success", fmt.Sprintf("%s 설치 완료 (%ds)", steps[i].Name, elapsed))
	}

	mu.Lock()
	progress.State = "phase_done"
	progress.Message = "인프라 설치 완료"
	mu.Unlock()

	appendLog("success", "인프라 설치 완료")
}

func runServicesSetup(cfg SetupConfig) {
	steps := []StepStatus{
		{Name: "Samba AD DC (Active Directory)", Status: "pending"},
		{Name: "Keycloak (SSO / 인증)", Status: "pending"},
		{Name: "Stalwart Mail (메일 서버)", Status: "pending"},
		{Name: "Keycloak 프로비저닝 (Realm, Client, LDAP)", Status: "pending"},
		{Name: "Console 배포", Status: "pending"},
		{Name: "Ingress 설정", Status: "pending"},
	}

	deploys := []serviceStep{
		{manifest: "samba-dc.yaml", labelSelector: "app=polyon-dc", timeout: 180 * time.Second},
		{manifest: "keycloak.yaml", labelSelector: "app=polyon-auth", timeout: 180 * time.Second},
		{manifest: "stalwart-config.yaml", labelSelector: "", timeout: 0},
		{manifest: "stalwart.yaml", labelSelector: "app=polyon-mail", timeout: 120 * time.Second},
	}

	mu.Lock()
	progress.Steps = steps
	progress.Total = len(steps)
	progress.Step = 0
	progress.State = "installing"
	mu.Unlock()

	tcfg := NewTemplateConfig(cfg)

	appendLog("info", fmt.Sprintf("서비스 설치 시작: domain=%s", cfg.Domain))

	// Deploy core services (samba, keycloak, stalwart-config + stalwart)
	serviceNames := []string{
		"Samba AD DC (Active Directory)",
		"Keycloak (SSO / 인증)",
		"Stalwart Mail (메일 서버)",
	}
	for i, deploy := range deploys {
		stepIdx := i
		if i == 2 {
			appendLog("info", "Stalwart 설정(ConfigMap) 배포 중...")
			if err := deployManifest(deploy.manifest, deploy.labelSelector, tcfg, deploy.timeout); err != nil {
				mu.Lock()
				progress.Steps[2].Status = "error"
				progress.Steps[2].Error = err.Error()
				progress.State = "error"
				progress.Message = "Stalwart 설정 실패: " + err.Error()
				mu.Unlock()
				appendLog("error", "Stalwart 설정 실패: "+err.Error())
				return
			}
			continue
		}
		if i == 3 {
			stepIdx = 2
		}

		now := time.Now().UnixMilli()
		mu.Lock()
		progress.Step = stepIdx + 1
		progress.Message = serviceNames[stepIdx] + " 설치 중..."
		progress.Steps[stepIdx].Status = "running"
		progress.Steps[stepIdx].StartedAt = now
		mu.Unlock()

		appendLog("info", fmt.Sprintf("[%d/3] %s 설치 중...", stepIdx+1, serviceNames[stepIdx]))

		if err := deployManifest(deploy.manifest, deploy.labelSelector, tcfg, deploy.timeout); err != nil {
			mu.Lock()
			progress.Steps[stepIdx].Status = "error"
			progress.Steps[stepIdx].Error = err.Error()
			progress.State = "error"
			progress.Message = serviceNames[stepIdx] + " 설치 실패: " + err.Error()
			mu.Unlock()
			appendLog("error", serviceNames[stepIdx]+" 설치 실패: "+err.Error())
			return
		}

		doneAt := time.Now().UnixMilli()
		mu.Lock()
		progress.Steps[stepIdx].Status = "done"
		progress.Steps[stepIdx].DoneAt = doneAt
		mu.Unlock()
		elapsed := (doneAt - now) / 1000
		appendLog("success", fmt.Sprintf("%s 설치 완료 (%ds)", serviceNames[stepIdx], elapsed))
	}

	// Provisioning phase (must run before OAuth2 Proxy — realm must exist for OIDC discovery)
	provNow := time.Now().UnixMilli()
	mu.Lock()
	progress.Step = 4
	progress.Message = "Keycloak 프로비저닝 (Realm, Client, LDAP) 진행 중..."
	progress.Steps[3].Status = "running"
	progress.Steps[3].StartedAt = provNow
	mu.Unlock()

	appendLog("info", "Keycloak 프로비저닝 시작...")
	if err := runProvisioning(cfg, tcfg); err != nil {
		mu.Lock()
		progress.Steps[3].Status = "error"
		progress.Steps[3].Error = err.Error()
		progress.State = "error"
		progress.Message = "프로비저닝 실패: " + err.Error()
		mu.Unlock()
		appendLog("error", "프로비저닝 실패: "+err.Error())
		return
	}

	provDone := time.Now().UnixMilli()
	provElapsed := (provDone - provNow) / 1000
	mu.Lock()
	progress.Steps[3].Status = "done"
	progress.Steps[3].DoneAt = provDone
	mu.Unlock()
	appendLog("success", fmt.Sprintf("프로비저닝 완료 (%ds)", provElapsed))

	// Mark remaining steps done (Console & Ingress are deployed via manifest)
	{
		doneAt := time.Now().UnixMilli()
		mu.Lock()
		progress.Steps[4].Status = "done"
		progress.Steps[4].DoneAt = doneAt
		progress.Steps[5].Status = "done"
		progress.Steps[5].DoneAt = doneAt
		progress.State = "phase_done"
		progress.Message = "서비스 설치 완료"
		mu.Unlock()
	}

	appendLog("success", "서비스 설치 완료 — 모든 구성 요소가 배포되었습니다")
}
