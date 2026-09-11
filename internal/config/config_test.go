package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveLoadAndEnvironmentOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	t.Setenv("ACTIONBOX_CONFIG_FILE", path)
	t.Setenv("ACTIONBOX_API_URL", "")
	t.Setenv("ACTIONBOX_TOKEN", "")

	if err := Save(Config{BaseURL: "https://api.actionbox.cloud/", Token: "  axb_live_test  "}); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if permission := info.Mode().Perm(); permission != 0o600 {
		t.Fatalf("config permissions = %#o, want 0600", permission)
	}
	if permission := infoFromDirectory(t, filepath.Dir(path)).Mode().Perm(); permission != 0o700 {
		t.Fatalf("config directory permissions = %#o, want 0700", permission)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.BaseURL != "https://api.actionbox.cloud" || cfg.Token != "axb_live_test" {
		t.Fatalf("loaded config = %+v", cfg)
	}

	t.Setenv("ACTIONBOX_API_URL", "http://localhost:8000/")
	t.Setenv("ACTIONBOX_TOKEN", "axb_live_env")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load with environment overrides returned error: %v", err)
	}
	if cfg.BaseURL != "http://localhost:8000" || cfg.Token != "axb_live_env" {
		t.Fatalf("environment-overridden config = %+v", cfg)
	}
}

func TestLoadUsesHostedAPIForEnvironmentToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-config.json")
	t.Setenv("ACTIONBOX_CONFIG_FILE", path)
	t.Setenv("ACTIONBOX_API_URL", "")
	t.Setenv("ACTIONBOX_TOKEN", "axb_live_environment_only")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.BaseURL != DefaultBaseURL || cfg.Token != "axb_live_environment_only" {
		t.Fatalf("environment-only config = %+v", cfg)
	}
}

func infoFromDirectory(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config directory: %v", err)
	}
	return info
}

func TestLoadRejectsWorldReadableConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("ACTIONBOX_CONFIG_FILE", path)
	t.Setenv("ACTIONBOX_API_URL", "")
	t.Setenv("ACTIONBOX_TOKEN", "")
	if err := os.WriteFile(path, []byte(`{"base_url":"https://api.actionbox.cloud","token":"axb_live_test"}
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "permissions are too open") {
		t.Fatalf("Load error = %v, want permission error", err)
	}
}

func TestLoadRejectsSymlinkConfig(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.json")
	target := filepath.Join(directory, "target.json")
	t.Setenv("ACTIONBOX_CONFIG_FILE", path)
	t.Setenv("ACTIONBOX_API_URL", "")
	t.Setenv("ACTIONBOX_TOKEN", "")
	if err := os.WriteFile(target, []byte(`{"base_url":"https://api.actionbox.cloud","token":"axb_live_test"}
`), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("Load error = %v, want symbolic-link error", err)
	}
}

func TestLoadRejectsOversizedConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("ACTIONBOX_CONFIG_FILE", path)
	t.Setenv("ACTIONBOX_API_URL", "")
	t.Setenv("ACTIONBOX_TOKEN", "")
	if err := os.WriteFile(path, []byte(strings.Repeat(" ", maxConfigBytes+1)), 0o600); err != nil {
		t.Fatalf("write oversized config: %v", err)
	}

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("Load error = %v, want size error", err)
	}
}

func TestValidateRejectsUnsafeURLsAndTokens(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{name: "query", cfg: Config{BaseURL: "https://api.actionbox.cloud?token=leak", Token: "axb_live_test"}, want: "query"},
		{name: "fragment", cfg: Config{BaseURL: "https://api.actionbox.cloud#fragment", Token: "axb_live_test"}, want: "fragment"},
		{name: "plaintext remote", cfg: Config{BaseURL: "http://api.actionbox.cloud", Token: "axb_live_test"}, want: "HTTPS"},
		{name: "path", cfg: Config{BaseURL: "https://api.actionbox.cloud/prefix", Token: "axb_live_test"}, want: "path"},
		{name: "token whitespace", cfg: Config{BaseURL: "https://api.actionbox.cloud", Token: "axb_live test"}, want: "whitespace"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := Validate(test.cfg)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateAllowsLoopbackHTTP(t *testing.T) {
	for _, baseURL := range []string{"http://localhost:8000", "http://127.0.0.1:8000", "http://[::1]:8000"} {
		if err := Validate(Config{BaseURL: baseURL, Token: "axb_live_test"}); err != nil {
			t.Fatalf("Validate(%q) returned error: %v", baseURL, err)
		}
	}
}
