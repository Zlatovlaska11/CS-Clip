package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	DemosFolder     string `json:"demos_folder"`
	ClipsFolder     string `json:"clips_folder"`
	Tickrate        int    `json:"tickrate"`
	MaxUploadSizeGB int    `json:"max_upload_size_gb"`
}

func Load(path string) (*Config, error) {
	cfg := &Config{
		DemosFolder:     "./demos",
		ClipsFolder:     "./clips",
		Tickrate:        64,
		MaxUploadSizeGB: 20,
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

func Save(path string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
