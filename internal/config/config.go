// Package config 负责把环境变量收敛成服务运行所需的配置。
package config

import (
	"fmt"
	"os"
	"strings"
)

// defaultListenPort 与 .env.example / docker-compose 保持一致。
const defaultListenPort = "8080"

// Config 是服务的全部外部依赖配置，禁止硬编码，只能从环境变量注入。
type Config struct {
	// WechatToken 微信测试号后台自定义的 Token，用于回调签名校验。
	WechatToken string
	// WechatAllowOpenID 唯一放行的发送者 OpenID，其余来源一律忽略。
	WechatAllowOpenID string
	// MemosAPIURL Memos 实例基地址，不含 /api/v1 前缀。
	MemosAPIURL string
	// MemosAccessToken Memos 访问令牌，放在 Authorization: Bearer 之后。
	MemosAccessToken string
	// ListenPort 服务监听端口。
	ListenPort string
}

// Load 读取环境变量并做启动期校验：配置不全时快速失败，避免带病运行。
func Load() (*Config, error) {
	cfg := &Config{
		WechatToken:       strings.TrimSpace(os.Getenv("WECHAT_TOKEN")),
		WechatAllowOpenID: strings.TrimSpace(os.Getenv("WECHAT_ALLOW_OPENID")),
		MemosAPIURL:       normalizeBaseURL(os.Getenv("MEMOS_API_URL")),
		MemosAccessToken:  strings.TrimSpace(os.Getenv("MEMOS_ACCESS_TOKEN")),
		ListenPort:        strings.TrimSpace(os.Getenv("LISTEN_PORT")),
	}
	if cfg.ListenPort == "" {
		cfg.ListenPort = defaultListenPort
	}

	missing := make([]string, 0, 4)
	for _, item := range []struct {
		name  string
		value string
	}{
		{"WECHAT_TOKEN", cfg.WechatToken},
		{"WECHAT_ALLOW_OPENID", cfg.WechatAllowOpenID},
		{"MEMOS_API_URL", cfg.MemosAPIURL},
		{"MEMOS_ACCESS_TOKEN", cfg.MemosAccessToken},
	} {
		if item.value == "" {
			missing = append(missing, item.name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("缺少必需环境变量: %s", strings.Join(missing, ", "))
	}

	return cfg, nil
}

// normalizeBaseURL 容忍用户漏写 scheme 或多写结尾斜杠，避免拼接出非法 URL。
func normalizeBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = strings.TrimRight(raw, "/")
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	return raw
}
