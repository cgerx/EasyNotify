package server

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	Key string `json:"key"`
}

func SaveKey(path, key string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".easynotify-key-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := json.NewEncoder(file).Encode(Config{key}); err != nil {
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
