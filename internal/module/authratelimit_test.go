package module

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIPIgnoresSpoofedHeadersForDirectClient(t *testing.T) {
	t.Setenv("TRUST_PROXY_HEADERS", "")

	r := httptest.NewRequest(http.MethodPost, "/api/auth", nil)
	r.RemoteAddr = "203.0.113.9:51234"
	// 直连场景下伪造这两个头不应影响限速键。
	r.Header.Set("CF-Connecting-IP", "1.2.3.4")
	r.Header.Set("X-Forwarded-For", "5.6.7.8")

	if got := ClientIP(r); got != "203.0.113.9" {
		t.Fatalf("期望使用 RemoteAddr，实际 %q", got)
	}
}

func TestClientIPTrustsProxyHeadersFromLoopback(t *testing.T) {
	t.Setenv("TRUST_PROXY_HEADERS", "")

	r := httptest.NewRequest(http.MethodPost, "/api/auth", nil)
	r.RemoteAddr = "127.0.0.1:51234"
	r.Header.Set("CF-Connecting-IP", "1.2.3.4")

	if got := ClientIP(r); got != "1.2.3.4" {
		t.Fatalf("回环来源应信任 CF-Connecting-IP，实际 %q", got)
	}
}

func TestClientIPTrustsProxyHeadersWhenOptedIn(t *testing.T) {
	t.Setenv("TRUST_PROXY_HEADERS", "1")

	r := httptest.NewRequest(http.MethodPost, "/api/auth", nil)
	r.RemoteAddr = "203.0.113.9:51234"
	r.Header.Set("X-Forwarded-For", "198.51.100.7, 203.0.113.9")

	if got := ClientIP(r); got != "198.51.100.7" {
		t.Fatalf("显式信任代理时应取 XFF 第一跳，实际 %q", got)
	}
}

func TestClientIPIgnoresMalformedHeaderValue(t *testing.T) {
	t.Setenv("TRUST_PROXY_HEADERS", "1")

	r := httptest.NewRequest(http.MethodPost, "/api/auth", nil)
	r.RemoteAddr = "127.0.0.1:51234"
	r.Header.Set("X-Forwarded-For", "not-an-ip")

	if got := ClientIP(r); got != "127.0.0.1" {
		t.Fatalf("非法 IP 应回退到 RemoteAddr，实际 %q", got)
	}
}

func TestAuthLockoutResetsAfterSuccess(t *testing.T) {
	const ip = "198.51.100.66"
	ResetAuthFailures(ip)

	var locked bool
	for i := 0; i < authMaxFailures; i++ {
		if wait := RecordAuthFailure(ip); wait > 0 {
			locked = true
		}
	}
	if !locked {
		t.Fatalf("%d 次失败后应触发锁定", authMaxFailures)
	}
	if _, isLocked := AuthLocked(ip); !isLocked {
		t.Fatal("锁定状态应可读")
	}
	ResetAuthFailures(ip)
	if _, isLocked := AuthLocked(ip); isLocked {
		t.Fatal("登录成功后应清除锁定")
	}
}

func TestRevokeTokenMarksTokenInvalid(t *testing.T) {
	// exp 设成很远的将来，吊销条目应被保留。
	token := "eyJhbGciOiJIUzI1NiJ9.eyJleHAiOjQxMDI0NDQ4MDB9.sig"
	r := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	r.Header.Set("Authorization", "Bearer "+token)

	RevokeToken(r)
	if !IsTokenRevoked(token) {
		t.Fatal("登出后 token 应处于吊销状态")
	}

	other := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	other.Header.Set("Authorization", "Bearer other-token")
	if IsTokenRevoked("other-token") {
		t.Fatal("未吊销的 token 不应被判为失效")
	}
}

func TestCaptchaRateLimit(t *testing.T) {
	const ip = "198.51.100.77"
	allowed := 0
	for i := 0; i < captchaRateLimit+5; i++ {
		if AllowCaptcha(ip) {
			allowed++
		}
	}
	if allowed != captchaRateLimit {
		t.Fatalf("期望放行 %d 次，实际 %d 次", captchaRateLimit, allowed)
	}
}
