package main

import (
	"bytes"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"text/template"
	"time"
)

//go:embed manifests/*.yaml
var manifestFS embed.FS

// TemplateConfig holds all template variables for manifest rendering
type TemplateConfig struct {
	Namespace             string
	Domain                string
	DomainUpper           string // e.g. CMARS.COM
	DomainNetBIOS         string // e.g. CMARS
	AdminPassword         string
	ConsoleAdminPassword  string // Keycloak admin password
	OrgName               string
	PostgresPassword      string
	ConsoleDomain         string // e.g. console.cmars.com
	AuthDomain            string // e.g. auth.cmars.com
	MailDomain            string // e.g. mail.cmars.com
	PortalDomain          string // e.g. portal.cmars.com
	DomainDC                string // e.g. DC=cmars,DC=com
	CookieSecret            string // 16-byte base64 for oauth2-proxy
	ForwardAuthClientSecret string // 32-byte hex for oauth2-proxy confidential client
}

// DomainToDC converts a domain like "cmars.com" to "DC=cmars,DC=com"
func DomainToDC(domain string) string {
	parts := strings.Split(strings.ToLower(domain), ".")
	dcs := make([]string, len(parts))
	for i, p := range parts {
		dcs[i] = "DC=" + p
	}
	return strings.Join(dcs, ",")
}

// NewTemplateConfig derives computed fields from SetupConfig
func NewTemplateConfig(cfg SetupConfig) TemplateConfig {
	domain := cfg.Domain
	domainUpper := strings.ToUpper(domain)

	// NetBIOS name: first part of domain, uppercased
	netbios := domainUpper
	if idx := strings.Index(netbios, "."); idx > 0 {
		netbios = netbios[:idx]
	}

	// Subdomain defaults
	consoleSub := cfg.Subdomains.Console
	if consoleSub == "" {
		consoleSub = "console"
	}
	authSub := cfg.Subdomains.Auth
	if authSub == "" {
		authSub = "auth"
	}
	mailSub := cfg.Subdomains.Mail
	if mailSub == "" {
		mailSub = "mail"
	}
	portalSub := cfg.Subdomains.Portal
	if portalSub == "" {
		portalSub = "portal"
	}

	domainLower := strings.ToLower(domain)

	consoleAdminPw := cfg.ConsoleAdminPassword
	if consoleAdminPw == "" {
		consoleAdminPw = cfg.AdminPassword
	}

	// Generate CookieSecret (16 bytes, base64)
	cookieBytes := make([]byte, 16)
	rand.Read(cookieBytes)
	cookieSecret := base64.StdEncoding.EncodeToString(cookieBytes)

	// Generate ForwardAuthClientSecret (32 bytes, hex)
	authSecretBytes := make([]byte, 32)
	rand.Read(authSecretBytes)
	forwardAuthSecret := hex.EncodeToString(authSecretBytes)

	return TemplateConfig{
		Namespace:               cfg.Namespace,
		Domain:                  domain,
		DomainUpper:             domainUpper,
		DomainNetBIOS:           netbios,
		AdminPassword:           cfg.AdminPassword,
		ConsoleAdminPassword:    consoleAdminPw,
		OrgName:                 cfg.OrgName,
		PostgresPassword:        cfg.AdminPassword, // reuse admin password for postgres
		ConsoleDomain:           consoleSub + "." + domainLower,
		AuthDomain:              authSub + "." + domainLower,
		MailDomain:              mailSub + "." + domainLower,
		PortalDomain:            portalSub + "." + domainLower,
		DomainDC:                DomainToDC(domain),
		CookieSecret:            cookieSecret,
		ForwardAuthClientSecret: forwardAuthSecret,
	}
}

// renderManifest parses and executes a Go text/template manifest file
func renderManifest(name string, cfg TemplateConfig) (string, error) {
	data, err := manifestFS.ReadFile("manifests/" + name)
	if err != nil {
		return "", fmt.Errorf("read manifest %s: %w", name, err)
	}

	tmpl, err := template.New(name).Parse(string(data))
	if err != nil {
		return "", fmt.Errorf("parse manifest %s: %w", name, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, cfg); err != nil {
		return "", fmt.Errorf("render manifest %s: %w", name, err)
	}

	return buf.String(), nil
}

// applyManifest pipes rendered YAML to kubectl apply -f -
func applyManifest(yaml string) error {
	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(yaml)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("kubectl apply: %s\n%s", stderr.String(), err)
	}

	log.Printf("[K8S] %s", strings.TrimSpace(string(out)))
	return nil
}

// waitForReady waits for pods matching the label selector to become Ready
func waitForReady(ns, labelSelector string, timeout time.Duration) error {
	timeoutStr := fmt.Sprintf("%.0fs", timeout.Seconds())

	cmd := exec.Command("kubectl", "wait",
		"--for=condition=ready", "pod",
		"-l", labelSelector,
		"-n", ns,
		"--timeout="+timeoutStr,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("kubectl wait (%s): %s\n%s", labelSelector, stderr.String(), err)
	}

	log.Printf("[K8S] %s", strings.TrimSpace(string(out)))
	return nil
}

// deployManifest is a convenience that renders + applies + waits
func deployManifest(manifestName, labelSelector string, cfg TemplateConfig, timeout time.Duration) error {
	yaml, err := renderManifest(manifestName, cfg)
	if err != nil {
		return err
	}

	if err := applyManifest(yaml); err != nil {
		return err
	}

	if labelSelector != "" {
		if err := waitForReady(cfg.Namespace, labelSelector, timeout); err != nil {
			return err
		}
	}

	return nil
}

// ensureNamespace creates the namespace if it doesn't exist
func ensureNamespace(cfg TemplateConfig) error {
	yaml, err := renderManifest("namespace.yaml", cfg)
	if err != nil {
		return err
	}
	return applyManifest(yaml)
}
