package viper

import (
	"path/filepath"

	spfviper "github.com/spf13/viper"
)

// Config wraps a spf13/viper instance and provides
// a narrow API for YAML/JSON configuration loading.
type Config struct {
	v *spfviper.Viper
}

// New creates an empty Config.
// Call LoadFile before using Unmarshal/UnmarshalKey.
func New() *Config {
	return &Config{
		v: spfviper.New(),
	}
}

// LoadFile loads a YAML or JSON config file into the Config.
// The file type is inferred from the extension (.yaml/.yml/.json).
func (c *Config) LoadFile(path string) error {
	if c.v == nil {
		c.v = spfviper.New()
	}

	c.v.SetConfigFile(path)

	switch ext := filepath.Ext(path); ext {
	case ".yaml", ".yml":
		c.v.SetConfigType("yaml")
	case ".json":
		c.v.SetConfigType("json")
	default:
		// let viper infer the type or return a clear error on read
	}

	return c.v.ReadInConfig()
}

// Unmarshal unmarshals the entire config into dst.
// dst should be a pointer to a struct or map.
func (c *Config) Unmarshal(dst interface{}) error {
	if c.v == nil {
		return nil
	}
	return c.v.Unmarshal(dst)
}

// UnmarshalKey unmarshals a specific key subtree into dst.
// dst should be a pointer to a struct or map.
func (c *Config) UnmarshalKey(key string, dst interface{}) error {
	if c.v == nil {
		return nil
	}
	return c.v.UnmarshalKey(key, dst)
}

