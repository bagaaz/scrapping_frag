package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	// Database
	DBHost     string
	DBPort     int
	DBName     string
	DBUser     string
	DBPassword string

	// Workers
	WorkerCount    int
	RequestTimeout int

	// Proxy
	ProxyFile     string
	ProxyCooldown float64

	// Retry
	MaxRetries int
}

func Load() (*Config, error) {
	_ = godotenv.Load() // ignora erro se .env não existir

	cfg := &Config{
		DBHost:         getEnv("DB_HOST", "localhost"),
		DBPort:         getEnvInt("DB_PORT", 3306),
		DBName:         getEnv("DB_NAME", "fragrantica_db"),
		DBUser:         getEnv("DB_USER", "root"),
		DBPassword:     getEnv("DB_PASSWORD", ""),
		WorkerCount:    getEnvInt("WORKER_COUNT", 10),
		RequestTimeout: getEnvInt("REQUEST_TIMEOUT", 30),
		ProxyFile:      getEnv("PROXY_FILE", "proxies.txt"),
		ProxyCooldown:  getEnvFloat("PROXY_COOLDOWN", 1.0),
		MaxRetries:     getEnvInt("MAX_RETRIES", 3),
	}

	return cfg, nil
}

func (c *Config) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=true&loc=Local",
		c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}
