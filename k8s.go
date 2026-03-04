package main

import (
	"bytes"
	"embed"
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
	Namespace        string
	Domain           string
	DomainUpper      string // e.g. CMARS.COM
	DomainNetBIOS    string // e.g. CMARS
	AdminPassword    string
	OrgName          string
	PostgresPassword string
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

	return TemplateConfig{
		Namespace:        cfg.Namespace,
		Domain:           domain,
		DomainUpper:      domainUpper,
		DomainNetBIOS:    netbios,
		AdminPassword:    cfg.AdminPassword,
		OrgName:          cfg.OrgName,
		PostgresPassword: cfg.AdminPassword, // reuse admin password for postgres
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
