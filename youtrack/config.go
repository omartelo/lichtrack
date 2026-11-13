package youtrack

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	configFileName = "config.json"
	configFileMode = 0o600
)

type Config struct {
	BaseURL string `json:"baseUrl"`
	Token   string `json:"token"`
	// Insecure is the person's choice to skip certificate verification.
	Insecure bool `json:"insecure,omitempty"`
}

func (c Config) Client() *Client {
	if c.Insecure {
		return NewInsecure(c.BaseURL, c.Token)
	}
	return New(c.BaseURL, c.Token)
}

// LoadConfig returns an error matching os.ErrNotExist when nothing was saved yet.
func LoadConfig(dir string) (Config, error) {
	var cfg Config
	raw, err := os.ReadFile(filepath.Join(dir, configFileName))
	if err != nil {
		return cfg, fmt.Errorf("youtrack: read config: %w", err)
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("youtrack: parse config: %w", err)
	}
	return cfg, nil
}

func SaveConfig(dir string, cfg Config) error {
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("youtrack: encode config: %w", err)
	}
	path := filepath.Join(dir, configFileName)
	if err := os.WriteFile(path, raw, configFileMode); err != nil {
		return fmt.Errorf("youtrack: write config: %w", err)
	}
	// WriteFile keeps the mode of a file that already exists.
	if err := os.Chmod(path, configFileMode); err != nil {
		return fmt.Errorf("youtrack: restrict config: %w", err)
	}
	return nil
}
