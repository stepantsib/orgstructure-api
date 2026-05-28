// Package config loads runtime configuration from the environment.
//
// Defaults are aimed at "docker-compose up" working out of the box: an API
// process inside a container talking to a Postgres container on the same
// network. The .env.example file documents every variable.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTP HTTPConfig
	DB   DBConfig
	Log  LogConfig

	MigrationsDir       string
	RunMigrationsOnStart bool
}

type HTTPConfig struct {
	Addr            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
}

type DBConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	Name     string
	SSLMode  string
}

type LogConfig struct {
	Level  string // debug | info | warn | error
	Format string // text | json
}

// DSN returns the Postgres connection string in the form GORM expects.
func (d DBConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		d.Host, d.Port, d.User, d.Password, d.Name, d.SSLMode,
	)
}

// Load reads the environment, applying defaults when variables are unset.
// Any parse error is fatal because the app cannot start without config.
func Load() (*Config, error) {
	port, err := getEnvInt("DB_PORT", 5432)
	if err != nil {
		return nil, err
	}
	readT, err := getEnvDuration("HTTP_READ_TIMEOUT", 10*time.Second)
	if err != nil {
		return nil, err
	}
	writeT, err := getEnvDuration("HTTP_WRITE_TIMEOUT", 15*time.Second)
	if err != nil {
		return nil, err
	}
	shutdownT, err := getEnvDuration("HTTP_SHUTDOWN_TIMEOUT", 15*time.Second)
	if err != nil {
		return nil, err
	}
	runMigrations, err := getEnvBool("RUN_MIGRATIONS_ON_START", true)
	if err != nil {
		return nil, err
	}

	return &Config{
		HTTP: HTTPConfig{
			Addr:            getEnv("HTTP_ADDR", ":8080"),
			ReadTimeout:     readT,
			WriteTimeout:    writeT,
			ShutdownTimeout: shutdownT,
		},
		DB: DBConfig{
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     port,
			User:     getEnv("DB_USER", "orgstructure"),
			Password: getEnv("DB_PASSWORD", "orgstructure"),
			Name:     getEnv("DB_NAME", "orgstructure"),
			SSLMode:  getEnv("DB_SSLMODE", "disable"),
		},
		Log: LogConfig{
			Level:  getEnv("LOG_LEVEL", "info"),
			Format: getEnv("LOG_FORMAT", "json"),
		},
		MigrationsDir:        getEnv("MIGRATIONS_DIR", "./migrations"),
		RunMigrationsOnStart: runMigrations,
	}, nil
}

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) (int, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return def, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return v, nil
}

func getEnvBool(key string, def bool) (bool, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return def, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean: %w", key, err)
	}
	return v, nil
}

func getEnvDuration(key string, def time.Duration) (time.Duration, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return def, nil
	}
	v, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration (e.g. 5s): %w", key, err)
	}
	return v, nil
}
