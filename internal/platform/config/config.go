package config

import (
	"fmt"
	"os"
)

type Config struct {
	DatabaseURL      string
	DatabaseAdminURL string
	RedisURL         string
	HTTPPort         string
	LogLevel         string
	EfiSandbox       bool
}

func Load() (*Config, error) {
	c := &Config{
		DatabaseURL:      os.Getenv("DATABASE_URL"),
		DatabaseAdminURL: os.Getenv("DATABASE_ADMIN_URL"),
		RedisURL:         getDefault("REDIS_URL", "redis://localhost:6379/0"),
		HTTPPort:         getDefault("HTTP_PORT", "8080"),
		LogLevel:         getDefault("LOG_LEVEL", "info"),
		EfiSandbox:       os.Getenv("EFI_SANDBOX") != "false",
	}
	if c.DatabaseURL == "" {
		return nil, fmt.Errorf("config: DATABASE_URL is required")
	}
	return c, nil
}

func getDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
