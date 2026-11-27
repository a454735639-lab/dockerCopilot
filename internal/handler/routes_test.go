package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/onlyLTY/dockerCopilot/internal/config"
	"github.com/onlyLTY/dockerCopilot/internal/module"
	"github.com/onlyLTY/dockerCopilot/internal/svc"
	"github.com/zeromicro/go-zero/rest"
)

func TestProgressRouteRequiresJWT(t *testing.T) {
	port := getFreePort(t)
	cfg := config.Config{}
	cfg.Host = "127.0.0.1"
	cfg.Port = port
	cfg.Auth.AccessSecret = "test-secret"

	server := rest.MustNewServer(cfg.RestConf)
	RegisterHandlers(server, &svc.ServiceContext{Config: cfg})
	defer server.Stop()

	go server.Start()
	waitForServer(t, port)

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/api/progress/test-task", port))
	if err != nil {
		t.Fatalf("failed to call progress route: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without JWT, got %d", resp.StatusCode)
	}
}

func TestAuthRouteDoesNotRequireJWT(t *testing.T) {
	port := getFreePort(t)
	cfg := config.Config{}
	cfg.Host = "127.0.0.1"
	cfg.Port = port
	cfg.Auth.AccessSecret = "test-secret"

	server := rest.MustNewServer(cfg.RestConf)
	RegisterHandlers(server, &svc.ServiceContext{Config: cfg})
	defer server.Stop()

	go server.Start()
	waitForServer(t, port)

	// 登录必须先过验证码；不传验证码会稳定返回 400。
	captchaID, _, captchaCode := module.GenCaptcha()
	form := url.Values{}
	form.Set("secretKey", "test-secret")
	form.Set("captchaId", captchaID)
	form.Set("captchaCode", captchaCode)
	req, err := http.NewRequest(
		http.MethodPost,
		fmt.Sprintf("http://127.0.0.1:%d/api/auth", port),
		bytes.NewBufferString(form.Encode()),
	)
	if err != nil {
		t.Fatalf("failed to create auth request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to call auth route: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read auth response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from auth route without JWT, got %d with body %s", resp.StatusCode, string(body))
	}
}

// 登录拿到 token → 带 token 访问受保护接口 → 登出 → 同一 token 应被拒绝。
func TestLogoutRevokesToken(t *testing.T) {
	port := getFreePort(t)
	cfg := config.Config{}
	cfg.Host = "127.0.0.1"
	cfg.Port = port
	cfg.Auth.AccessSecret = "test-secret"
	// AccessExpire 为 0 时签发出的 token 立即过期，测试必须显式给有效期。
	cfg.Auth.AccessExpire = 3600

	server := rest.MustNewServer(cfg.RestConf)
	server.Use(module.TokenRevocationMiddleware)
	RegisterHandlers(server, &svc.ServiceContext{Config: cfg})
	defer server.Stop()

	go server.Start()
	waitForServer(t, port)

	captchaID, _, captchaCode := module.GenCaptcha()
	form := url.Values{}
	form.Set("secretKey", "test-secret")
	form.Set("captchaId", captchaID)
	form.Set("captchaCode", captchaCode)

	loginResp, err := http.Post(
		fmt.Sprintf("http://127.0.0.1:%d/api/auth", port),
		"application/x-www-form-urlencoded",
		bytes.NewBufferString(form.Encode()),
	)
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	loginBody, _ := io.ReadAll(loginResp.Body)
	loginResp.Body.Close()

	var parsed struct {
		Code int `json:"code"`
		Data struct {
			Jwt string `json:"jwt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(loginBody, &parsed); err != nil {
		t.Fatalf("unmarshal login response: %v (body %s)", err, loginBody)
	}
	if parsed.Data.Jwt == "" {
		t.Fatalf("登录未返回 token，响应体 %s", loginBody)
	}
	token := parsed.Data.Jwt

	call := func(method, path string) int {
		req, reqErr := http.NewRequest(method, fmt.Sprintf("http://127.0.0.1:%d%s", port, path), nil)
		if reqErr != nil {
			t.Fatalf("build request: %v", reqErr)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, doErr := http.DefaultClient.Do(req)
		if doErr != nil {
			t.Fatalf("call %s: %v", path, doErr)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	if code := call(http.MethodGet, "/api/progress/logout-test"); code == http.StatusUnauthorized {
		t.Fatalf("登出前 token 应可用，实际 %d", code)
	}
	if code := call(http.MethodPost, "/api/auth/logout"); code != http.StatusOK {
		t.Fatalf("登出应返回 200，实际 %d", code)
	}
	if code := call(http.MethodGet, "/api/progress/logout-test"); code != http.StatusUnauthorized {
		t.Fatalf("登出后 token 应被拒绝，实际 %d", code)
	}
}

func getFreePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	defer listener.Close()

	return listener.Addr().(*net.TCPAddr).Port
}

func waitForServer(t *testing.T, port int) {
	t.Helper()

	client := &http.Client{Timeout: 200 * time.Millisecond}
	url := fmt.Sprintf("http://127.0.0.1:%d/", port)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("server on port %d did not become ready", port)
}
