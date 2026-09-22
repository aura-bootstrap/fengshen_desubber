package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Listen       string `yaml:"listen"`        // e.g. ":18080"
	RootPassword string `yaml:"root_password"` // 超级管理员初始密码,启动时幂等种入 root(空=不种)
	SessionKey   string `yaml:"session_key"`   // 管理会话 HMAC 密钥(空=回落 card_pepper)
	CardPepper   string `yaml:"card_pepper"`   // 卡哈希 HMAC 密钥（env CARD_PEPPER 优先）

	TOS TOSConfig `yaml:"tos"`
	LAS LASConfig `yaml:"las"`
}

type TOSConfig struct {
	AK       string `yaml:"ak"`
	SK       string `yaml:"sk"`
	Endpoint string `yaml:"endpoint"` // tos-cn-beijing.volces.com
	Region   string `yaml:"region"`   // cn-beijing
	Bucket   string `yaml:"bucket"`
}

type LASConfig struct {
	BaseURL         string `yaml:"base_url"` // https://operator.las.cn-beijing.volces.com
	APIKey          string `yaml:"api_key"`
	OperatorID      string `yaml:"operator_id"`
	OperatorVersion string `yaml:"operator_version"`
}

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if c.Listen == "" ||
		c.TOS.AK == "" || c.TOS.SK == "" || c.TOS.Bucket == "" ||
		c.LAS.BaseURL == "" || c.LAS.APIKey == "" {
		return nil, fmt.Errorf("config missing required fields")
	}
	if c.LAS.OperatorID == "" {
		c.LAS.OperatorID = "las_subtitle_erase"
	}
	if c.LAS.OperatorVersion == "" {
		c.LAS.OperatorVersion = "v1"
	}
	return &c, nil
}
