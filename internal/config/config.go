package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"

	"github.com/ActionBox-Cloud/actionbox-cli/internal/netutil"
)

const DefaultBaseURL = "https://api.actionbox.cloud"

const maxConfigBytes = 64 << 10

type Config struct {
	BaseURL string `json:"base_url"`
	Token   string `json:"token"`
}

func Path() (string, error) {
	if override := os.Getenv("ACTIONBOX_CONFIG_FILE"); override != "" {
		return override, nil
	}
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "actionbox", "config.json"), nil
}

func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}

	cfg := Config{}
	data, readErr := readSecureConfig(path)
	if readErr == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return Config{}, fmt.Errorf("could not read Actionbox config: %w", err)
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return Config{}, readErr
	}

	if value := strings.TrimSpace(os.Getenv("ACTIONBOX_API_URL")); value != "" {
		cfg.BaseURL = value
	}
	if value := strings.TrimSpace(os.Getenv("ACTIONBOX_TOKEN")); value != "" {
		cfg.Token = value
	}
	// Environment-only CI configuration should require only the secret. The
	// hosted API URL is public and is the authoritative default.
	if cfg.BaseURL == "" && cfg.Token != "" {
		cfg.BaseURL = DefaultBaseURL
	}
	if cfg.BaseURL == "" || cfg.Token == "" {
		if readErr != nil {
			return Config{}, errors.New("Actionbox is not configured; run actionbox configure first")
		}
		return Config{}, errors.New("Actionbox config is incomplete; run actionbox configure again")
	}
	if err := Validate(cfg); err != nil {
		return Config{}, err
	}
	return Normalize(cfg), nil
}

// readSecureConfig binds the permission and symlink checks to the file that is
// actually read. Opening first and comparing the descriptor with a subsequent
// Lstat prevents a path swap from making us validate a different file.
func readSecureConfig(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	openedInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("Actionbox config must not be a symbolic link; run actionbox configure again")
	}
	if !openedInfo.Mode().IsRegular() || !pathInfo.Mode().IsRegular() {
		return nil, errors.New("Actionbox config path is not a regular file")
	}
	if !os.SameFile(openedInfo, pathInfo) {
		return nil, errors.New("Actionbox config changed while it was being opened; try again")
	}
	if runtime.GOOS != "windows" && openedInfo.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("Actionbox config permissions are too open (%#o); run actionbox configure again", openedInfo.Mode().Perm())
	}

	data, err := io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxConfigBytes {
		return nil, fmt.Errorf("Actionbox config exceeds %d bytes", maxConfigBytes)
	}
	return data, nil
}

func Save(cfg Config) error {
	cfg = Normalize(cfg)
	if err := Validate(cfg); err != nil {
		return err
	}
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if os.Getenv("ACTIONBOX_CONFIG_FILE") == "" {
		if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".config.json-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	keepTemp := false
	defer func() {
		_ = temp.Close()
		if !keepTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temp.Write(append(data, '\n')); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		if info, statErr := os.Lstat(path); statErr == nil {
			if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
				return errors.New("Actionbox config path is not a regular file")
			}
			if err := os.Remove(path); err != nil {
				return err
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("could not replace Actionbox config: %w", err)
	}
	keepTemp = true
	return nil
}

func Normalize(cfg Config) Config {
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.Token = strings.TrimSpace(cfg.Token)
	return cfg
}

func Validate(cfg Config) error {
	cfg = Normalize(cfg)
	if cfg.BaseURL == "" {
		return errors.New("API base URL cannot be empty")
	}
	parsed, err := url.Parse(cfg.BaseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("invalid API base URL %q; use a full http(s) URL", cfg.BaseURL)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("invalid API base URL %q; use http:// or https://", cfg.BaseURL)
	}
	if parsed.Scheme == "http" && !netutil.IsLoopbackHost(parsed.Hostname()) {
		return errors.New("API base URL must use HTTPS unless it targets localhost")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Hostname() == "" {
		return errors.New("API base URL must not contain credentials, query parameters, or fragments")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return errors.New("API base URL must not contain a path")
	}
	if cfg.Token == "" {
		return errors.New("Source token cannot be empty")
	}
	if strings.IndexFunc(cfg.Token, unicode.IsSpace) >= 0 {
		return errors.New("Source token cannot contain whitespace")
	}
	return nil
}
