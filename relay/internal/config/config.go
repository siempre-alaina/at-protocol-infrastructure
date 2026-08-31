package config

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
)

// Config holds all configuration for the relay
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Ingest   IngestConfig   `mapstructure:"ingest"`
	Identity IdentityConfig `mapstructure:"identity"`
	Metrics  MetricsConfig  `mapstructure:"metrics"`
}

type ServerConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

type DatabaseConfig struct {
	PostgresURL     string `mapstructure:"postgres_url"`
	MaxOpenConns    int    `mapstructure:"max_open_conns"`
	MaxIdleConns    int    `mapstructure:"max_idle_conns"`
	ConnMaxLifetime string `mapstructure:"conn_max_lifetime"`
}

type IngestConfig struct {
	InitialHosts      []string      `mapstructure:"initial_hosts"`
	WorkerCount       int           `mapstructure:"worker_count"`
	ReconnectDelay    time.Duration `mapstructure:"reconnect_delay"`
	MaxReconnectDelay time.Duration `mapstructure:"max_reconnect_delay"`
	BufferSize        int           `mapstructure:"buffer_size"`
}

type IdentityConfig struct {
	PLCURL    string        `mapstructure:"plc_url"`
	CacheSize int           `mapstructure:"cache_size"`
	CacheTTL  time.Duration `mapstructure:"cache_ttl"`
}

type MetricsConfig struct {
	Enabled bool `mapstructure:"enabled"`
	Port    int  `mapstructure:"port"`
}

// Load reads configuration from file and environment variables
func Load(configPath string) (*Config, error) {
	v := viper.New()

	// Set defaults
	setDefaults(v)

	// Read config file
	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.SetConfigName("relay")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("/etc/relay")
	}

	// Read environment variables
	v.SetEnvPrefix("RELAY")
	v.AutomaticEnv()

	// Read config file (optional - env vars can override)
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
		// Config file not found is OK - use defaults and env vars
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	// Server defaults
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 2470)

	// Database defaults
	v.SetDefault("database.postgres_url", "postgres://relay:CHANGE_ME@localhost/relay?sslmode=disable")
	v.SetDefault("database.max_open_conns", 25)
	v.SetDefault("database.max_idle_conns", 5)
	v.SetDefault("database.conn_max_lifetime", "5m")

	// Ingest defaults
	v.SetDefault("ingest.initial_hosts", []string{})
	v.SetDefault("ingest.worker_count", 4)
	v.SetDefault("ingest.reconnect_delay", "5s")
	v.SetDefault("ingest.max_reconnect_delay", "5m")
	v.SetDefault("ingest.buffer_size", 10000)

	// Identity defaults
	v.SetDefault("identity.plc_url", "https://plc.directory")
	v.SetDefault("identity.cache_size", 100000)
	v.SetDefault("identity.cache_ttl", "1h")

	// Metrics defaults
	v.SetDefault("metrics.enabled", true)
	v.SetDefault("metrics.port", 9091)
}

// Validate checks that the configuration is valid
func (c *Config) Validate() error {
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", c.Server.Port)
	}

	if c.Database.PostgresURL == "" {
		return fmt.Errorf("database.postgres_url is required")
	}

	if c.Ingest.WorkerCount <= 0 {
		return fmt.Errorf("ingest.worker_count must be positive")
	}

	if c.Identity.CacheSize <= 0 {
		return fmt.Errorf("identity.cache_size must be positive")
	}

	return nil
}

// Address returns the server listen address
func (c *ServerConfig) Address() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}
