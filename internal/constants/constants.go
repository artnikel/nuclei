// Package constants defines shared constants used across the application
package constants

import "time"

const (
	// Timeouts
	FiveSecTimeout = 5 * time.Second
	TenSecTimeout  = 10 * time.Second
	OneMinTimeout = 1 * time.Minute
	FiveMinTimeout = 5 * time.Minute
	DayTimeout     = 24 * time.Hour
	// File formats
	TxtFileFormat = ".txt"
	YmlFileFormat = ".yml"
	YamlFileFormat = ".yaml"
	// Permissions
	FilePerm = 0o600
	DirPerm = 0o750
)
