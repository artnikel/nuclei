// Package config provides configuration loading from YAML files
package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// LicenseConfig holds license-related settings
type LicenseConfig struct {
	ServerURL string `yaml:"server_url"`
	Key       string `yaml:"key"`
}

// AppConfig holds app-related settings
type AppConfig struct {
	ID string `yaml:"id"`
}

// Config aggregates all service configurations
type Config struct {
	License    LicenseConfig    `yaml:"license"`
	App AppConfig `yaml:"app"`
}

// LoadConfig loads the configuration from the given YAML file path
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}
