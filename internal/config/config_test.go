package config

import "testing"

func TestLoadFailsWhenRequiredEnvIsMissing(t *testing.T) {
	t.Setenv("WECHAT_TOKEN", "")
	t.Setenv("WECHAT_ALLOW_OPENID", "")
	t.Setenv("MEMOS_API_URL", "")
	t.Setenv("MEMOS_ACCESS_TOKEN", "")

	if _, err := Load(); err == nil {
		t.Fatal("缺少必填环境变量时应返回错误")
	}
}

func TestLoadAppliesDefaultsAndNormalizesURL(t *testing.T) {
	t.Setenv("WECHAT_TOKEN", "token")
	t.Setenv("WECHAT_ALLOW_OPENID", "openid")
	t.Setenv("MEMOS_API_URL", "memos.example.com/")
	t.Setenv("MEMOS_ACCESS_TOKEN", "access")
	t.Setenv("LISTEN_PORT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}

	if cfg.ListenPort != defaultListenPort {
		t.Errorf("LISTEN_PORT 缺省时期望 %s，实际 %q", defaultListenPort, cfg.ListenPort)
	}
	// 漏写 scheme + 多写结尾斜杠都应被归一化
	if cfg.MemosAPIURL != "https://memos.example.com" {
		t.Errorf("MemosAPIURL 期望 https://memos.example.com，实际 %q", cfg.MemosAPIURL)
	}
}
