package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
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
	AppEngine string `json:"appEngine"` // default: "apps"
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

var cachedTemplateConfig *TemplateConfig

func init() {
	progress = SetupProgress{State: "fresh"}
	logBuffer = make([]LogEntry, 0, maxLogLines)
}

// getOrCreateTemplateConfig returns a cached TemplateConfig, ensuring
// passwords are generated only once across infra and services phases.
func getOrCreateTemplateConfig(cfg SetupConfig) TemplateConfig {
	mu.Lock()
	defer mu.Unlock()
	if cachedTemplateConfig == nil {
		tc := NewTemplateConfig(cfg)
		cachedTemplateConfig = &tc
	}
	return *cachedTemplateConfig
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

	log.Printf("PolyON Operator v0.7.9 starting on :%s", port)
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

	tcfg := getOrCreateTemplateConfig(cfg)

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

// runStep은 단일 설치 스텝을 실행하고 progress를 업데이트하는 헬퍼입니다.
// fatal=true이면 실패 시 에러를 반환하고, false이면 경고만 남기고 계속 진행합니다.
func runStep(stepIdx int, name string, fatal bool, fn func() error) error {
	now := time.Now().UnixMilli()
	mu.Lock()
	progress.Step = stepIdx + 1
	progress.Message = name + " 진행 중..."
	progress.Steps[stepIdx].Status = "running"
	progress.Steps[stepIdx].StartedAt = now
	mu.Unlock()

	appendLog("info", name+" 진행 중...")
	err := fn()
	doneAt := time.Now().UnixMilli()
	elapsed := (doneAt - now) / 1000

	if err != nil {
		if fatal {
			mu.Lock()
			progress.Steps[stepIdx].Status = "error"
			progress.Steps[stepIdx].Error = err.Error()
			progress.State = "error"
			progress.Message = name + " 실패: " + err.Error()
			mu.Unlock()
			appendLog("error", fmt.Sprintf("%s 실패: %v", name, err))
		} else {
			mu.Lock()
			progress.Steps[stepIdx].Status = "done"
			progress.Steps[stepIdx].DoneAt = doneAt
			mu.Unlock()
			appendLog("warn", fmt.Sprintf("%s 경고 (비치명적): %v", name, err))
		}
		return err
	}

	mu.Lock()
	progress.Steps[stepIdx].Status = "done"
	progress.Steps[stepIdx].DoneAt = doneAt
	mu.Unlock()
	appendLog("success", fmt.Sprintf("%s 완료 (%ds)", name, elapsed))
	return nil
}

// ensureDB creates a PostgreSQL database if it doesn't exist.
func ensureDB(ns, dbName string) {
	out, _ := kubectlExec(ns, "app=polyon-db",
		[]string{"psql", "-U", "polyon", "-d", "postgres", "-tAc",
			fmt.Sprintf("SELECT 1 FROM pg_database WHERE datname='%s'", dbName)})
	if strings.TrimSpace(out) == "1" {
		appendLog("info", fmt.Sprintf("DB %s 이미 존재", dbName))
		return
	}
	kubectlExec(ns, "app=polyon-db",
		[]string{"psql", "-U", "polyon", "-d", "postgres", "-c",
			fmt.Sprintf("CREATE DATABASE %s OWNER polyon", dbName)})
	appendLog("success", fmt.Sprintf("DB %s 생성 완료", dbName))
}

func runServicesSetup(cfg SetupConfig) {
	// 설치 순서 (의존성 기반):
	// [코어]       DC → KC → Mail
	// [KC 프로비저닝] Realm/Client/LDAP/GroupMapper  ← KC 확정 직후
	// [앱 서비스]   OPA → Gitea → AI Gateway         ← KC 프로비저닝 후 (향후 OIDC 연동 대비)
	// [플랫폼 UI]   Core → Console → Portal → AppEngine
	// [라우팅]      Ingress
	steps := []StepStatus{
		{Name: "Samba AD DC (Active Directory)", Status: "pending"},       // 0
		{Name: "Keycloak (SSO / 인증)", Status: "pending"},                // 1
		{Name: "Stalwart Mail (메일 서버)", Status: "pending"},             // 2
		{Name: "Keycloak 프로비저닝 (Realm, Client, LDAP)", Status: "pending"}, // 3
		{Name: "OPA (정책 엔진)", Status: "pending"},                       // 4
		{Name: "Gitea (Git 저장소)", Status: "pending"},                    // 5
		{Name: "AI Gateway (LiteLLM)", Status: "pending"},                 // 6
		{Name: "Core 백엔드", Status: "pending"},                           // 7
		{Name: "Console", Status: "pending"},                               // 8
		{Name: "Portal", Status: "pending"},                                // 9
		{Name: "AppEngine (ERP 엔진)", Status: "pending"},                  // 10
		{Name: "Ingress 설정", Status: "pending"},                          // 11
	}

	mu.Lock()
	progress.Steps = steps
	progress.Total = len(steps)
	progress.Step = 0
	progress.State = "installing"
	mu.Unlock()

	tcfg := getOrCreateTemplateConfig(cfg)
	appendLog("info", fmt.Sprintf("서비스 설치 시작: domain=%s", cfg.Domain))

	// ── 0: Samba AD DC ──────────────────────────────────────────
	if err := runStep(0, "Samba AD DC (Active Directory)", true, func() error {
		return deployManifest("samba-dc.yaml", "app=polyon-dc", tcfg, 300*time.Second)
	}); err != nil {
		return
	}

	// ── 1: Keycloak ─────────────────────────────────────────────
	if err := runStep(1, "Keycloak (SSO / 인증)", true, func() error {
		return deployManifest("keycloak.yaml", "app=polyon-auth", tcfg, 300*time.Second)
	}); err != nil {
		return
	}

	// ── 2: Stalwart Mail ────────────────────────────────────────
	if err := runStep(2, "Stalwart Mail (메일 서버)", true, func() error {
		if err := deployManifest("stalwart-config.yaml", "", tcfg, 0); err != nil {
			return fmt.Errorf("Stalwart ConfigMap: %w", err)
		}
		return deployManifest("stalwart.yaml", "app=polyon-mail", tcfg, 120*time.Second)
	}); err != nil {
		return
	}

	// ── 3: Keycloak 프로비저닝 ───────────────────────────────────
	// KC가 완전히 기동된 직후 프로비저닝 (이후 앱들이 KC OIDC를 사용할 수 있도록)
	if err := runStep(3, "Keycloak 프로비저닝 (Realm, Client, LDAP)", true, func() error {
		return runProvisioning(cfg, tcfg)
	}); err != nil {
		return
	}

	// ── 4: OPA ──────────────────────────────────────────────────
	if err := runStep(4, "OPA (정책 엔진)", true, func() error {
		return deployManifest("opa.yaml", "app=polyon-opa", tcfg, 60*time.Second)
	}); err != nil {
		return
	}

	// ── 5: Gitea ────────────────────────────────────────────────
	if err := runStep(5, "Gitea (Git 저장소)", true, func() error {
		ensureDB(tcfg.Namespace, "polyon_gitea")
		if err := deployManifest("gitea.yaml", "app=polyon-gitea", tcfg, 120*time.Second); err != nil {
			return err
		}
		// Gitea 관리자 계정 생성 (비치명적)
		_, err := kubectlExec(tcfg.Namespace, "app=polyon-gitea",
			[]string{"su", "git", "-c",
				fmt.Sprintf("gitea admin user create --admin --username polyon-admin --password '%s' --email admin@%s --config /data/gitea/conf/app.ini",
					tcfg.GiteaAdminPassword, tcfg.Domain)})
		if err != nil {
			if strings.Contains(fmt.Sprint(err), "already exists") {
				appendLog("info", "Gitea 관리자 계정 이미 존재")
			} else {
				appendLog("warn", "Gitea 관리자 계정 생성 실패 (비치명적): "+err.Error())
			}
		} else {
			appendLog("success", "Gitea 관리자 계정 생성 완료 (polyon-admin)")
		}
		return nil
	}); err != nil {
		return
	}

	// ── 6: AI Gateway (LiteLLM) ─────────────────────────────────
	if err := runStep(6, "AI Gateway (LiteLLM)", true, func() error {
		ensureDB(tcfg.Namespace, "polyon_ai")
		return deployManifest("litellm.yaml", "app=polyon-ai", tcfg, 300*time.Second)
	}); err != nil {
		return
	}

	// ── 7: Core 백엔드 ──────────────────────────────────────────
	if err := runStep(7, "Core 백엔드", true, func() error {
		return deployManifest("core.yaml", "app=polyon-core", tcfg, 120*time.Second)
	}); err != nil {
		return
	}

	// ── 8: Console ──────────────────────────────────────────────
	if err := runStep(8, "Console", true, func() error {
		return deployManifest("console.yaml", "app=polyon-console", tcfg, 120*time.Second)
	}); err != nil {
		return
	}

	// ── 9: Portal ───────────────────────────────────────────────
	if err := runStep(9, "Portal", true, func() error {
		return deployManifest("portal.yaml", "app=polyon-portal", tcfg, 120*time.Second)
	}); err != nil {
		return
	}

	// ── 10: AppEngine ───────────────────────────────────────────
	if err := runStep(10, "AppEngine (ERP 엔진)", true, func() error {
		if err := provisionAppEngineDB(tcfg); err != nil {
			appendLog("warn", "AppEngine DB 생성 실패 (비치명적): "+err.Error())
		}
		return deployManifest("appengine.yaml", "app=polyon-appengine", tcfg, 180*time.Second)
	}); err != nil {
		return
	}

	// ── 11: Ingress ─────────────────────────────────────────────
	if err := runStep(11, "Ingress 설정", true, func() error {
		return deployManifest("ingress.yaml", "", tcfg, 0)
	}); err != nil {
		return
	}

	mu.Lock()
	progress.State = "phase_done"
	progress.Message = "서비스 설치 완료"
	mu.Unlock()
	appendLog("success", "서비스 설치 완료 — 모든 구성 요소가 배포되었습니다")
}
