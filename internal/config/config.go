package config

import (
	"encoding/hex"
	"fmt"
	"os"
	"time"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Kanboard KanboardConfig `yaml:"kanboard"`
	Security SecurityConfig `yaml:"security"`
	Storage  StorageConfig  `yaml:"storage"`
}

type ServerConfig struct {
	Port string `yaml:"port"`
	Host string `yaml:"host"`
}

type KanboardConfig struct {
	DefaultURL string        `yaml:"default_url"`
	Timeout    time.Duration `yaml:"timeout"`
}

type SecurityConfig struct {
	EncryptionKeyEnv string `yaml:"encryption_key_env"`
}

type StorageConfig struct {
	DataDir string `yaml:"data_dir"`
}

func LoadConfig() (*Config, error) {
	config := &Config{
		Server: ServerConfig{
			Port: getEnvOrDefault("MCP_PORT", "8080"),
			Host: getEnvOrDefault("MCP_HOST", "0.0.0.0"),
		},
		Kanboard: KanboardConfig{
			DefaultURL: getEnvOrDefault("DEFAULT_KANBOARD_URL", ""),
			Timeout:    30 * time.Second,
		},
		Security: SecurityConfig{
			EncryptionKeyEnv: "ENCRYPTION_KEY",
		},
		Storage: StorageConfig{
			DataDir: getEnvOrDefault("DATA_DIR", "./data"),
		},
	}

	if timeoutStr := os.Getenv("KANBOARD_TIMEOUT"); timeoutStr != "" {
		if timeout, err := time.ParseDuration(timeoutStr); err == nil {
			config.Kanboard.Timeout = timeout
		}
	}

	return config, nil
}

func (c *Config) GetEncryptionKey() ([]byte, error) {
	keyHex := os.Getenv(c.Security.EncryptionKeyEnv)
	if keyHex == "" {
		return nil, fmt.Errorf("encryption key environment variable %s is not set", c.Security.EncryptionKeyEnv)
	}

	key, err := hex.DecodeString(keyHex)
	if err != nil {
		return nil, fmt.Errorf("failed to decode encryption key: %w", err)
	}

	if len(key) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes (64 hex characters)")
	}

	return key, nil
}

func (c *Config) Validate() error {
	if c.Kanboard.DefaultURL == "" {
		return fmt.Errorf("default Kanboard URL is required")
	}

	if c.Server.Port == "" {
		return fmt.Errorf("server port is required")
	}

	if c.Storage.DataDir == "" {
		return fmt.Errorf("data directory is required")
	}

	_, err := c.GetEncryptionKey()
	if err != nil {
		return fmt.Errorf("encryption key validation failed: %w", err)
	}

	return nil
}


func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
