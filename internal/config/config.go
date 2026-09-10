package config

import (
	"crypto/sha256"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server  ServerConfig  `yaml:"server"`
	Storage StorageConfig `yaml:"storage"`
	Source  SourceConfig  `yaml:"source"`
	DR      DRConfig      `yaml:"dr"`
	Scrape  ScrapeConfig  `yaml:"scrape"`
}

type ServerConfig struct {
	Listen           string `yaml:"listen"`
	SessionSecret    string `yaml:"session_secret"`
	SessionSecretEnv string `yaml:"session_secret_env"` // optional: read from env if session_secret is empty
}

type StorageConfig struct {
	Path             string `yaml:"path"`
	EncryptionKey    string `yaml:"encryption_key"`
	EncryptionKeyEnv string `yaml:"encryption_key_env"` // optional: read from env if encryption_key is empty
}

type SourceConfig struct {
	Kubeconfig string          `yaml:"kubeconfig"`
	Namespaces NamespaceConfig `yaml:"namespaces"`
}

type NamespaceConfig struct {
	Include        []string `yaml:"include"`
	Exclude        []string `yaml:"exclude"`
	All            bool     `yaml:"all"`
	DefaultExclude *bool    `yaml:"default_exclude"`
}

func (n NamespaceConfig) UseDefaultExclude(hasInclude bool) bool {
	if hasInclude {
		return false
	}
	if n.DefaultExclude != nil {
		return *n.DefaultExclude
	}
	return true
}

type DRConfig struct {
	Kubeconfig string `yaml:"kubeconfig"`
}

type ScrapeConfig struct {
	Interval                string              `yaml:"interval"`
	ExcludeKinds            []string            `yaml:"exclude_kinds"`
	ExcludeNames            map[string][]string `yaml:"exclude_names"`
	PreserveHelmAnnotations bool                `yaml:"preserve_helm_annotations"`
	PreserveReplicas        bool                `yaml:"preserve_replicas"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := &Config{
		Server: ServerConfig{
			Listen: ":8080",
		},
		Storage: StorageConfig{
			Path: "./data",
		},
		Scrape: ScrapeConfig{
			PreserveReplicas: true,
		},
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Migrate mistaken use of *_env fields as inline secrets (value is hex, not an env var name).
	if cfg.Server.SessionSecret == "" && looksLikeSecretValue(cfg.Server.SessionSecretEnv) {
		cfg.Server.SessionSecret = cfg.Server.SessionSecretEnv
		cfg.Server.SessionSecretEnv = ""
	}
	if cfg.Storage.EncryptionKey == "" && looksLikeSecretValue(cfg.Storage.EncryptionKeyEnv) {
		cfg.Storage.EncryptionKey = cfg.Storage.EncryptionKeyEnv
		cfg.Storage.EncryptionKeyEnv = ""
	}

	if cfg.Server.Listen == "" {
		cfg.Server.Listen = ":8080"
	}
	if cfg.Storage.Path == "" {
		cfg.Storage.Path = "./data"
	}

	if err := cfg.validateNamespaces(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func looksLikeSecretValue(s string) bool {
	if len(s) < 16 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		case c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

func (c *Config) validateNamespaces() error {
	hasInclude := len(c.Source.Namespaces.Include) > 0
	hasExclude := len(c.Source.Namespaces.Exclude) > 0
	hasAll := c.Source.Namespaces.All

	if hasInclude && hasAll {
		return fmt.Errorf("namespaces: cannot set both include and all: true")
	}
	if !hasInclude && !hasExclude && !hasAll {
		return fmt.Errorf("namespaces: must set include, exclude, or all: true")
	}
	return nil
}

func (c *Config) SessionSecret() ([]byte, error) {
	val, err := c.secretValue(c.Server.SessionSecret, c.Server.SessionSecretEnv, "session_secret")
	if err != nil {
		return nil, err
	}
	return parseSecretBytes(val, 32, "session_secret")
}

func (c *Config) EncryptionKey() ([]byte, error) {
	val, err := c.secretValue(c.Storage.EncryptionKey, c.Storage.EncryptionKeyEnv, "encryption_key")
	if err != nil {
		return nil, err
	}
	key, err := parseSecretBytes(val, 32, "encryption_key")
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		sum := sha256.Sum256(key)
		return sum[:], nil
	}
	return key, nil
}

func (c *Config) secretValue(inline, envName, field string) (string, error) {
	if inline != "" {
		return inline, nil
	}
	if envName != "" {
		val := os.Getenv(envName)
		if val == "" {
			return "", fmt.Errorf("%s is empty and environment variable %s is not set", field, envName)
		}
		return val, nil
	}
	return "", fmt.Errorf("%s is required in config (or set %s_env to read from an environment variable)", field, field)
}

func (c *Config) ScrapeInterval() (time.Duration, error) {
	if c.Scrape.Interval == "" {
		return 0, nil
	}
	return time.ParseDuration(c.Scrape.Interval)
}

func parseSecretBytes(val string, minLen int, name string) ([]byte, error) {
	if len(val) == minLen*2 && isHex(val) {
		b, err := decodeHex(val)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		return b, nil
	}
	if len(val) < minLen {
		return nil, fmt.Errorf("%s must be at least %d bytes (or %d hex characters)", name, minLen, minLen*2)
	}
	return []byte(val), nil
}

func isHex(s string) bool {
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		case c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

func decodeHex(val string) ([]byte, error) {
	n := len(val) / 2
	b := make([]byte, n)
	for i := 0; i < n; i++ {
		var hi, lo byte
		c := val[i*2]
		d := val[i*2+1]
		switch {
		case c >= '0' && c <= '9':
			hi = c - '0'
		case c >= 'a' && c <= 'f':
			hi = c - 'a' + 10
		case c >= 'A' && c <= 'F':
			hi = c - 'A' + 10
		default:
			return nil, fmt.Errorf("invalid hex")
		}
		switch {
		case d >= '0' && d <= '9':
			lo = d - '0'
		case d >= 'a' && d <= 'f':
			lo = d - 'a' + 10
		case d >= 'A' && d <= 'F':
			lo = d - 'A' + 10
		default:
			return nil, fmt.Errorf("invalid hex")
		}
		b[i] = hi<<4 | lo
	}
	return b, nil
}
