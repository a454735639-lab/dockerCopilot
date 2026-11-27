package utiles

import (
	"strings"
	"testing"
)

func TestResolveNotifyURLRejectsUnsafeTargets(t *testing.T) {
	t.Setenv("NOTIFY_ALLOW_PRIVATE", "")

	cases := []struct {
		name string
		url  string
	}{
		{"非 http(s) 协议", "file:///etc/passwd"},
		{"缺少主机名", "http:///hook"},
		{"云元数据地址", "http://169.254.169.254/latest/meta-data/"},
		{"本机回环", "http://127.0.0.1:8080/hook"},
		{"内网地址", "http://10.0.0.5/hook"},
	}
	for _, c := range cases {
		if _, err := resolveNotifyURL(c.url); err == nil {
			t.Errorf("%s: 期望被拒绝，实际通过: %s", c.name, c.url)
		}
	}
}

func TestResolveNotifyURLAllowsPrivateWhenOptedIn(t *testing.T) {
	t.Setenv("NOTIFY_ALLOW_PRIVATE", "1")
	// 内网自建 ntfy：显式放行后应返回可用的 dial 地址。
	addr, err := resolveNotifyURL("http://127.0.0.1:2586/topic")
	if err != nil {
		t.Fatalf("期望放行内网地址，实际报错: %v", err)
	}
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatalf("dial 地址异常: %s", addr)
	}
}

func TestNotifyConfiguredAcceptsWecomAppWithoutURL(t *testing.T) {
	if notifyConfigured(NotifyConf{Format: "wecom_app"}) != true {
		t.Fatal("企业微信应用模式不需要 URL，应视为已配置")
	}
	if notifyConfigured(NotifyConf{}) != false {
		t.Fatal("没有 URL 也没有应用凭证时应视为未配置")
	}
	if notifyConfigured(NotifyConf{URL: "https://example.com/hook"}) != true {
		t.Fatal("有 webhook 地址时应视为已配置")
	}
}
