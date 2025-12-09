package viper

import (
	"path/filepath"

	spfviper "github.com/spf13/viper"
)

// Config 封装 spf13/viper 实例，对外提供精简的 YAML/JSON 配置加载接口。
type Config struct {
	v *spfviper.Viper
}

// New 创建一个空的 Config。
// 在调用 Unmarshal/UnmarshalKey 之前需要先调用 LoadFile 加载配置文件。
func New() *Config {
	return &Config{
		v: spfviper.New(),
	}
}

// LoadFile 将 YAML 或 JSON 配置文件加载到 Config 中。
// 文件类型通过扩展名（.yaml/.yml/.json）推断。
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
		// 让 viper 自行推断类型，或在读取时返回清晰的错误信息。
	}

	return c.v.ReadInConfig()
}

// Unmarshal 将完整配置反序列化到 dst。
// dst 应为结构体或 map 的指针。
func (c *Config) Unmarshal(dst interface{}) error {
	if c.v == nil {
		return nil
	}
	return c.v.Unmarshal(dst)
}

// UnmarshalKey 将指定 key 对应的子配置反序列化到 dst。
// dst 应为结构体或 map 的指针。
func (c *Config) UnmarshalKey(key string, dst interface{}) error {
	if c.v == nil {
		return nil
	}
	return c.v.UnmarshalKey(key, dst)
}
