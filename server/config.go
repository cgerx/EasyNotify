package server

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	Key  string     `json:"key"`
	Bark BarkConfig `json:"bark,omitempty"`
}

func SaveKey(path, key string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	values := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &values); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if values == nil {
		values = map[string]any{}
	}
	values["key"] = key
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".easynotify-key-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := json.NewEncoder(file).Encode(values); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
func LoadKey(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return "", err
	}
	return config.Key, ValidateKey(config.Key)
}

// LoadConfig reads configuration without validating the receiver key, which may
// be overridden by EASYNOTIFY_KEY.
func LoadConfig(path string) (Config, error) {
	var config Config
	data, err := os.ReadFile(path)
	if err != nil {
		return config, err
	}
	err = json.Unmarshal(data, &config)
	return config, err
}
