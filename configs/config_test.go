package configs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, content string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_FILE", path)
}

func TestMissingFileUsesDefaults(t *testing.T) {
	t.Setenv("CONFIG_FILE", filepath.Join(t.TempDir(), "absent.toml"))
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Limits.BodyBytes() != 1<<20 || cfg.Limits.ImportBytes() != 96<<20 || cfg.Gateway.Port != "8081" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestFileOverridesDefaultsAndEnvOverridesFile(t *testing.T) {
	writeConfig(t, `
[gateway]
port = "9000"
rate = 7
signature_max_age = "5s"

[limits]
max_body_mb = 2
max_import_mb = 8
`)
	t.Setenv("MAX_IMPORT_MB", "16")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Gateway.Port != "9000" || cfg.Gateway.Rate != 7 || cfg.Gateway.SignatureMaxAge != 5*time.Second {
		t.Fatalf("gateway = %+v", cfg.Gateway)
	}
	if cfg.Limits.BodyBytes() != 2<<20 || cfg.Limits.ImportBytes() != 16<<20 {
		t.Fatalf("limits = %+v", cfg.Limits)
	}
}

func TestSecretsComeOnlyFromEnv(t *testing.T) {
	writeConfig(t, "[pocketbase]\npassword = \"in-the-file\"\n")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "pocketbase.password") {
		t.Fatalf("err = %v", err)
	}
}

func TestRejectsBadConfig(t *testing.T) {
	for name, content := range map[string]string{
		"unknown key":  "[limits]\nmax_imprt_mb = 8\n",
		"wrong type":   "[limits]\nmax_body_mb = \"big\"\n",
		"zero limit":   "[limits]\nmax_body_mb = 0\n",
		"huge batch":   "[api]\nimport_batch_size = 1001\n",
		"invalid toml": "[limits\n",
	} {
		t.Run(name, func(t *testing.T) {
			writeConfig(t, content)
			if _, err := Load(); err == nil {
				t.Fatal("want error")
			}
		})
	}
}

func TestRejectsBadEnv(t *testing.T) {
	writeConfig(t, "")
	t.Setenv("MAX_BODY_MB", "1.5")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "MAX_BODY_MB") {
		t.Fatalf("err = %v", err)
	}
}
