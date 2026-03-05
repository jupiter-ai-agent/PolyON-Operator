package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

	// 2. Get admin token (use ConsoleAdminPassword for Keycloak)
	token, err := getKeycloakToken(keycloakURL, tcfg.ConsoleAdminPassword)
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

	// 4. Create "helios" realm
	appendLog("info", "helios Realm 생성 중...")
	if err := createRealm(keycloakURL, token, "helios"); err != nil {
		return fmt.Errorf("create helios realm: %w", err)
	}
	appendLog("success", "helios Realm 생성 완료")

	// 5. Create OIDC client "polyon-console" in admin realm
	appendLog("info", "polyon-console OIDC 클라이언트 생성 중...")
	if err := createOIDCClient(keycloakURL, token, "admin", "polyon-console", tcfg.ConsoleDomain); err != nil {
		return fmt.Errorf("create polyon-console client: %w", err)
	}
	appendLog("success", "polyon-console 클라이언트 생성 완료 (admin realm)")

	// 5b. Create confidential client "polyon-forward-auth" for oauth2-proxy
	appendLog("info", "polyon-forward-auth 클라이언트 생성 중...")
	if err := createConfidentialClient(keycloakURL, token, "admin", "polyon-forward-auth", tcfg.ConsoleDomain, tcfg.ForwardAuthClientSecret); err != nil {
		return fmt.Errorf("create polyon-forward-auth client: %w", err)
	}
	appendLog("success", "polyon-forward-auth 클라이언트 생성 완료 (admin realm)")

	// 6. Create OIDC client "polyon-portal" in helios realm
	appendLog("info", "polyon-portal OIDC 클라이언트 생성 중...")
	if err := createOIDCClient(keycloakURL, token, "helios", "polyon-portal", tcfg.PortalDomain); err != nil {
		return fmt.Errorf("create polyon-portal client: %w", err)
	}
	appendLog("success", "polyon-portal 클라이언트 생성 완료 (helios realm)")

	// 7. Create LDAP federation in both realms
	for _, realm := range []string{"admin", "helios"} {
		appendLog("info", fmt.Sprintf("%s realm LDAP 페더레이션 설정 중...", realm))
		fedID, err := createLDAPFederation(keycloakURL, token, realm, tcfg)
		if err != nil {
			return fmt.Errorf("create LDAP federation in %s: %w", realm, err)
		}
		appendLog("success", fmt.Sprintf("%s realm LDAP 페더레이션 완료 (id=%s)", realm, fedID))

		// 8. Trigger fullSync
		appendLog("info", fmt.Sprintf("%s realm LDAP 동기화 중...", realm))
		if err := triggerLDAPSync(keycloakURL, token, realm, fedID); err != nil {
			return fmt.Errorf("LDAP sync in %s: %w", realm, err)
		}
		appendLog("success", fmt.Sprintf("%s realm LDAP 동기화 완료", realm))
	}

	// 9. Deploy Ingress
	appendLog("info", "Ingress 배포 중...")
	mu.Lock()
	progress.Step = 7
	progress.Message = "Ingress 설정 중..."
	progress.Steps[6].Status = "running"
	progress.Steps[6].StartedAt = time.Now().UnixMilli()
	mu.Unlock()

	if err := deployManifest("ingress.yaml", "", tcfg, 0); err != nil {
		return fmt.Errorf("deploy ingress: %w", err)
	}
	appendLog("success", "Ingress 배포 완료")

	// 10. Deploy Console
	appendLog("info", "Console 배포 중...")
	mu.Lock()
	progress.Step = 6
	progress.Message = "Console 배포 중..."
	progress.Steps[5].Status = "running"
	progress.Steps[5].StartedAt = time.Now().UnixMilli()
	mu.Unlock()

	if err := deployManifest("console.yaml", "app=polyon-console", tcfg, 120*time.Second); err != nil {
		return fmt.Errorf("deploy console: %w", err)
	}
	appendLog("success", "Console 배포 완료")

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
	payload := map[string]interface{}{
		"clientId":     clientID,
		"enabled":      true,
		"publicClient": true,
		"protocol":     "openid-connect",
		"redirectUris":  []string{"https://" + redirectDomain + "/*"},
		"webOrigins":    []string{"https://" + redirectDomain},
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

func createConfidentialClient(baseURL, token, realm, clientID, redirectDomain, clientSecret string) error {
	payload := map[string]interface{}{
		"clientId":                clientID,
		"enabled":                 true,
		"publicClient":            false,
		"protocol":                "openid-connect",
		"clientAuthenticatorType": "client-secret",
		"secret":                  clientSecret,
		"redirectUris":            []string{"https://" + redirectDomain + "/oauth2/callback"},
		"webOrigins":              []string{"https://" + redirectDomain},
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
			"bindCredential":        {tcfg.AdminPassword},
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
