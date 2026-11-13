package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	settingsFile     = "settings.json"
	settingsFileMode = 0o600
)

// Settings is what the person chose about the screen, apart from the
// connection, which youtrack.Config keeps.
type Settings struct {
	// CardFields names the custom fields a board card shows, in order.
	CardFields []string `json:"cardFields"`
}

// LoadSettings answers the zero Settings on a first run, which has no file.
func LoadSettings(dir string) (Settings, error) {
	var s Settings
	data, err := os.ReadFile(filepath.Join(dir, settingsFile))
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(data, &s)
}

func SaveSettings(dir string, s Settings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, settingsFile), data, settingsFileMode)
}
