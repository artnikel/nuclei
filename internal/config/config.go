package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type LicenseConfig struct {
	ServerURL string `yaml:"server_url"`
	Key       string `yaml:"key"`
}

type AppConfig struct {
	ID string `yaml:"id"`
}

type Config struct {
	License    LicenseConfig    `yaml:"license"`
	App AppConfig `yaml:"app"`
}

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
