package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

// runProvisioning configures Keycloak realms, clients, LDAP federation, and deploys Ingress + Console
func runProvisioning(cfg SetupConfig, tcfg TemplateConfig) error {
	keycloakURL := fmt.Sprintf("http://polyon-auth.%s.svc.cluster.local:8080", tcfg.Namespace)

	// 1. Wait for Keycloak to be ready
	appendLog("info", "Keycloak 준비 대기 중...")
	if err := waitForKeycloak(keycloakURL, 5*time.Minute); err != nil {
		return fmt.Errorf("keycloak not ready: %w", err)
	}
	appendLog("success", "Keycloak 준비 완료")

	// 2. Get admin token (use KCAdminPassword — matches KC_BOOTSTRAP_ADMIN_PASSWORD)
	token, err := getKeycloakToken(keycloakURL, tcfg.KCAdminPassword)
	if err != nil {
		return fmt.Errorf("get admin token: %w", err)
	}
	appendLog("success", "Keycloak 관리자 토큰 획득")

	// 3. Create "admin" realm
	appendLog("info", "admin Realm 생성 중...")
	if err := createRealm(keycloakURL, token, "admin"); err != nil {
		return fmt.Errorf("create admin realm: %w", err)
	}
	appendLog("success", "admin Realm 생성 완료")

	// 4. Create "polyon" realm
	appendLog("info", "polyon Realm 생성 중...")
	if err := createRealm(keycloakURL, token, "polyon"); err != nil {
		return fmt.Errorf("create polyon realm: %w", err)
	}
	appendLog("success", "polyon Realm 생성 완료")

	// 5. Create OIDC client "polyon-console" in admin realm
	appendLog("info", "polyon-console OIDC 클라이언트 생성 중...")
	if err := createOIDCClient(keycloakURL, token, "admin", "polyon-console", tcfg.ConsoleDomain); err != nil {
		return fmt.Errorf("create polyon-console client: %w", err)
	}
	appendLog("success", "polyon-console 클라이언트 생성 완료 (admin realm)")

	// 6. Create OIDC client "polyon-portal" in polyon realm
	appendLog("info", "polyon-portal OIDC 클라이언트 생성 중...")
	if err := createOIDCClient(keycloakURL, token, "polyon", "polyon-portal", tcfg.PortalDomain); err != nil {
		return fmt.Errorf("create polyon-portal client: %w", err)
	}
	appendLog("success", "polyon-portal 클라이언트 생성 완료 (polyon realm)")

	// 6.5 Create OIDC client "polyon-erpengine" in polyon realm
	appendLog("info", "polyon-erpengine OIDC 클라이언트 생성 중...")
	if err := createOIDCClient(keycloakURL, token, "polyon", "polyon-erpengine", "erp."+tcfg.Domain); err != nil {
		return fmt.Errorf("create polyon-erpengine client: %w", err)
	}
	appendLog("success", "polyon-erpengine 클라이언트 생성 완료 (polyon realm)")

	// 7. Create local admin user in admin realm (no LDAP)
	appendLog("info", "admin realm 관리자 계정 생성 중...")
	if err := createLocalUser(keycloakURL, token, "admin", "admin", tcfg.ConsoleAdminPassword); err != nil {
		return fmt.Errorf("create admin user in admin realm: %w", err)
	}
	appendLog("success", "admin realm 관리자 계정 생성 완료 (admin)")

	// 8. Create LDAP federation in polyon realm only
	appendLog("info", "polyon realm LDAP 페더레이션 설정 중...")
	fedID, err := createLDAPFederation(keycloakURL, token, "polyon", tcfg)
	if err != nil {
		return fmt.Errorf("create LDAP federation in polyon: %w", err)
	}
	appendLog("success", fmt.Sprintf("polyon realm LDAP 페더레이션 완료 (id=%s)", fedID))

	// 8.5. Ensure all AD users have mail attribute (required for Stalwart mail)
	appendLog("info", "AD 사용자 메일 속성 자동 설정 중...")
	if err := ensureADUserMailAttrs(&tcfg); err != nil {
		appendLog("warn", "AD 메일 속성 설정 일부 실패 (비치명적): "+err.Error())
	} else {
		appendLog("success", "AD 사용자 메일 속성 설정 완료")
	}

	// 9. Trigger fullSync for polyon realm
	appendLog("info", "polyon realm LDAP 동기화 중...")
	if err := triggerLDAPSync(keycloakURL, token, "polyon", fedID); err != nil {
		return fmt.Errorf("LDAP sync in polyon: %w", err)
	}
	appendLog("success", "polyon realm LDAP 동기화 완료")

	// 9. Deploy Core backend (must be before Console — nginx needs polyon-core upstream)
	appendLog("info", "Core 백엔드 배포 중...")
	mu.Lock()
	progress.Step = 5
	progress.Message = "Core + Console 배포 중..."
	progress.Steps[4].Status = "running"
	progress.Steps[4].StartedAt = time.Now().UnixMilli()
	mu.Unlock()

	if err := deployManifest("core.yaml", "app=polyon-core", tcfg, 120*time.Second); err != nil {
		return fmt.Errorf("deploy core: %w", err)
	}
	appendLog("success", "Core 백엔드 배포 완료")

	// 10. Deploy Console (after Core — nginx upstream dependency)
	appendLog("info", "Console 배포 중...")
	if err := deployManifest("console.yaml", "app=polyon-console", tcfg, 120*time.Second); err != nil {
		return fmt.Errorf("deploy console: %w", err)
	}
	appendLog("success", "Console 배포 완료")

	// 10.5 Deploy Portal
	appendLog("info", "Portal 배포 중...")
	if err := deployManifest("portal.yaml", "app=polyon-portal", tcfg, 120*time.Second); err != nil {
		return fmt.Errorf("deploy portal: %w", err)
	}
	appendLog("success", "Portal 배포 완료")

	// 10.6 Deploy ERPEngine (Foundation module)
	// ERPEngine DB + Secret 프로비저닝
	appendLog("info", "ERPEngine DB 생성 중...")
	if err := provisionERPEngineDB(tcfg); err != nil {
		appendLog("warn", "ERPEngine DB 생성 실패 (비치명적): "+err.Error())
	} else {
		appendLog("success", "ERPEngine DB 생성 완료")
	}

	appendLog("info", "ERPEngine 배포 중...")
	if err := deployManifest("erpengine.yaml", "app=polyon-erpengine", tcfg, 180*time.Second); err != nil {
		return fmt.Errorf("deploy erpengine: %w", err)
	}
	appendLog("success", "ERPEngine 배포 완료")

	// 11. Deploy Ingress
	appendLog("info", "Ingress 배포 중...")
	mu.Lock()
	progress.Step = 6
	progress.Message = "Ingress 설정 중..."
	progress.Steps[5].Status = "running"
	progress.Steps[5].StartedAt = time.Now().UnixMilli()
	mu.Unlock()

	if err := deployManifest("ingress.yaml", "", tcfg, 0); err != nil {
		return fmt.Errorf("deploy ingress: %w", err)
	}
	appendLog("success", "Ingress 배포 완료")

	appendLog("success", "Keycloak 프로비저닝 완료")
	return nil
}

func waitForKeycloak(baseURL string, timeout time.Duration) error {
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + "/realms/master")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("timeout waiting for keycloak after %v", timeout)
}

func getKeycloakToken(baseURL, password string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	data := url.Values{
		"grant_type": {"password"},
		"client_id":  {"admin-cli"},
		"username":   {"admin"},
		"password":   {password},
	}

	resp, err := client.PostForm(baseURL+"/realms/master/protocol/openid-connect/token", data)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token request failed (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.AccessToken, nil
}

func createRealm(baseURL, token, realmName string) error {
	payload := map[string]interface{}{
		"realm":   realmName,
		"enabled": true,
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", baseURL+"/admin/realms", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// 201 = created, 409 = already exists
	if resp.StatusCode != 201 && resp.StatusCode != 409 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create realm %s failed (%d): %s", realmName, resp.StatusCode, string(respBody))
	}
	return nil
}

func createOIDCClient(baseURL, token, realm, clientID, redirectDomain string) error {
	// Build explicit redirect URIs for portal + known service subdomains
	parts := strings.SplitN(redirectDomain, ".", 2)
	baseDomain := redirectDomain
	if len(parts) > 1 {
		baseDomain = parts[1]
	}
	// Portal + all potential service subdomains (Keycloak doesn't support *.domain wildcard)
	subdomains := []string{"portal", "mail", "chat", "drive", "wiki", "git", "office", "erp", "bpm", "ai", "automation"}
	redirectUris := make([]string, 0, len(subdomains)+1)
	redirectUris = append(redirectUris, "https://"+redirectDomain+"/*")
	for _, sub := range subdomains {
		uri := "https://" + sub + "." + baseDomain + "/*"
		if uri != "https://"+redirectDomain+"/*" {
			redirectUris = append(redirectUris, uri)
		}
	}
	payload := map[string]interface{}{
		"clientId":     clientID,
		"enabled":      true,
		"publicClient": true,
		"protocol":     "openid-connect",
		"redirectUris":  redirectUris,
		"webOrigins":    []string{"+"},
		"attributes": map[string]string{
			"pkce.code.challenge.method": "S256",
		},
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", baseURL+"/admin/realms/"+realm+"/clients", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 && resp.StatusCode != 409 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create client %s failed (%d): %s", clientID, resp.StatusCode, string(respBody))
	}
	return nil
}

func createLocalUser(baseURL, token, realm, username, password string) error {
	// Create user
	payload := map[string]interface{}{
		"username":      username,
		"enabled":       true,
		"emailVerified": true,
		"firstName":     "Admin",
		"lastName":      "User",
		"email":         username + "@localhost",
		"credentials": []map[string]interface{}{
			{
				"type":      "password",
				"value":     password,
				"temporary": false,
			},
		},
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", baseURL+"/admin/realms/"+realm+"/users", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 && resp.StatusCode != 409 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create user %s failed (%d): %s", username, resp.StatusCode, string(respBody))
	}
	return nil
}

func createLDAPFederation(baseURL, token, realm string, tcfg TemplateConfig) (string, error) {
	usersDN := "CN=Users," + tcfg.DomainDC
	bindDN := "CN=Administrator," + usersDN

	payload := map[string]interface{}{
		"name":         "samba-ad",
		"providerId":   "ldap",
		"providerType": "org.keycloak.storage.UserStorageProvider",
		"config": map[string][]string{
			"connectionUrl":          {"ldap://polyon-dc:389"},
			"bindDn":                 {bindDN},
			"bindCredential":        {tcfg.DCAdminPassword},
			"usersDn":               {usersDN},
			"vendor":                {"ad"},
			"editMode":              {"READ_ONLY"},
			"userObjectClasses":     {"user"},
			"usernameLDAPAttribute": {"sAMAccountName"},
			"rdnLDAPAttribute":     {"cn"},
			"uuidLDAPAttribute":    {"objectGUID"},
			"searchScope":          {"2"},
			"enabled":              {"true"},
			"priority":             {"0"},
			"syncRegistrations":    {"false"},
			"trustEmail":           {"true"},
		},
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", baseURL+"/admin/realms/"+realm+"/components", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 409 {
		return "existing", nil
	}
	if resp.StatusCode != 201 {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("create LDAP federation failed (%d): %s", resp.StatusCode, string(respBody))
	}

	// Extract component ID from Location header
	location := resp.Header.Get("Location")
	parts := strings.Split(location, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1], nil
	}
	return "", nil
}

func triggerLDAPSync(baseURL, token, realm, componentID string) error {
	syncURL := fmt.Sprintf("%s/admin/realms/%s/user-storage/%s/sync?action=triggerFullSync",
		baseURL, realm, componentID)

	req, _ := http.NewRequest("POST", syncURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("LDAP sync failed (%d): %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// ensureADUserMailAttrs ensures all AD DC users have a mail attribute.
// Users without mail get {sAMAccountName}@{domain}.
// System accounts (krbtgt, Guest) are skipped.
func ensureADUserMailAttrs(tcfg *TemplateConfig) error {
	namespace := tcfg.Namespace
	domain := strings.ToLower(tcfg.Domain)
	// Build BaseDN from domain parts: cmars.com → DC=cmars,DC=com
	parts := strings.Split(domain, ".")
	dnParts := make([]string, len(parts))
	for i, p := range parts {
		dnParts[i] = "DC=" + p
	}
	baseDN := strings.Join(dnParts, ",")

	skipUsers := map[string]bool{
		"krbtgt": true, "Guest": true, "guest": true,
	}

	// List AD users via kubectl exec → samba-tool user list
	out, err := kubectlExec(namespace, "app=polyon-dc",
		[]string{"samba-tool", "user", "list"})
	if err != nil {
		return fmt.Errorf("samba-tool user list: %w", err)
	}

	users := strings.Split(strings.TrimSpace(out), "\n")
	var errors []string

	for _, username := range users {
		username = strings.TrimSpace(username)
		if username == "" || skipUsers[username] {
			continue
		}

		// Check if user already has mail attribute
		showOut, err := kubectlExec(namespace, "app=polyon-dc",
			[]string{"samba-tool", "user", "show", username})
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s: show failed: %v", username, err))
			continue
		}

		hasMail := false
		for _, line := range strings.Split(showOut, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "mail:") {
				hasMail = true
				break
			}
		}

		if hasMail {
			appendLog("info", fmt.Sprintf("  %s: 메일 속성 이미 존재 — 건너뜀", username))
			continue
		}

		// Set mail attribute via ldbmodify
		mail := fmt.Sprintf("%s@%s", strings.ToLower(username), domain)
		// Find the user's DN — typically CN=username,CN=Users,DC=...
		userDN := fmt.Sprintf("CN=%s,CN=Users,%s", username, baseDN)

		ldif := fmt.Sprintf("dn: %s\nchangetype: modify\nadd: mail\nmail: %s\n", userDN, mail)
		_, err = kubectlExec(namespace, "app=polyon-dc",
			[]string{"bash", "-c", fmt.Sprintf("echo '%s' | ldbmodify -H /var/lib/samba/private/sam.ldb", ldif)})
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s: ldbmodify failed: %v", username, err))
			continue
		}
		appendLog("info", fmt.Sprintf("  %s → %s 메일 속성 설정 완료", username, mail))
	}

	if len(errors) > 0 {
		return fmt.Errorf("%d errors: %s", len(errors), strings.Join(errors, "; "))
	}
	return nil
}

// kubectlExec runs a command in a pod via kubectl exec.
func kubectlExec(namespace, labelSelector string, cmd []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get pod name
	getPod := exec.CommandContext(ctx, "kubectl", "get", "pods",
		"-n", namespace, "-l", labelSelector,
		"-o", "jsonpath={.items[0].metadata.name}")
	podNameBytes, err := getPod.Output()
	if err != nil {
		return "", fmt.Errorf("kubectl get pod (%s): %w", labelSelector, err)
	}
	podName := strings.TrimSpace(string(podNameBytes))
	if podName == "" {
		return "", fmt.Errorf("no pod found for selector %s", labelSelector)
	}

	args := []string{"exec", podName, "-n", namespace, "--"}
	args = append(args, cmd...)
	execCmd := exec.CommandContext(ctx, "kubectl", args...)
	var stdout, stderr bytes.Buffer
	execCmd.Stdout = &stdout
	execCmd.Stderr = &stderr
	if err := execCmd.Run(); err != nil {
		errMsg := stderr.String()
		if errMsg == "" {
			errMsg = stdout.String()
		}
		return "", fmt.Errorf("%s", strings.TrimSpace(errMsg))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// provisionERPEngineDB creates the polyon_erp database and K8s Secret for ERPEngine.
func provisionERPEngineDB(tcfg TemplateConfig) error {
	// 1. Create PostgreSQL database via kubectl exec
	createDBSQL := "CREATE DATABASE polyon_erp OWNER polyon;"
	_, err := kubectlExec(tcfg.Namespace, "app=polyon-db",
		[]string{"psql", "-U", "polyon", "-c", createDBSQL})
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return fmt.Errorf("create db: %w", err)
	}
	appendLog("info", "  polyon_erp DB 생성 완료 (또는 이미 존재)")

	// 2. Create K8s Secret: polyon-erpengine-secret
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	secretYAML := fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: polyon-erpengine-secret
  namespace: %s
type: Opaque
stringData:
  db_host: "polyon-db"
  db_port: "5432"
  db_name: "polyon_erp"
  db_user: "polyon"
  db_password: "%s"
  admin_password: "%s"
`, tcfg.Namespace, tcfg.DBPassword, tcfg.ConsoleAdminPassword)

	applyCmd := exec.CommandContext(ctx, "kubectl", "apply", "-f", "-")
	applyCmd.Stdin = strings.NewReader(secretYAML)
	var stderr bytes.Buffer
	applyCmd.Stderr = &stderr
	if err := applyCmd.Run(); err != nil {
		return fmt.Errorf("create secret: %s", strings.TrimSpace(stderr.String()))
	}
	appendLog("info", "  polyon-erpengine-secret 생성 완료")
	return nil
}
