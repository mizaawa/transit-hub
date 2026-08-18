package config

import (
	"fmt"
	"strings"
)

// Validate 检查配置有效性，在启动时尽早发现配置问题。
func (c Config) Validate() error {
	var errors []string

	if c.DatabaseURL == "" {
		errors = append(errors, "DATABASE_URL is required")
	}

	if c.RedisURL == "" {
		errors = append(errors, "REDIS_URL is required")
	}

	if c.Port == "" {
		errors = append(errors, "PORT is required")
	}

	// 公开注册开启时，需要 SMTP 配置才能发送验证码
	if c.AllowPublicRegister && c.AdminEmail == "" {
		errors = append(errors, "ADMIN_EMAIL is required when ALLOW_PUBLIC_REGISTER=true")
	}

	if len(errors) > 0 {
		return fmt.Errorf("configuration errors:\n  - %s", strings.Join(errors, "\n  - "))
	}

	return nil
}
