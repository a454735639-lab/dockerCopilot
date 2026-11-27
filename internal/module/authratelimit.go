package module

import (
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// 登录失败限速：同一来源 10 分钟内失败 5 次，锁定 10 分钟。
//
// 只有"可信代理"才允许用 CF-Connecting-IP / X-Forwarded-For 判定来源：
// cloudflared  tunnel 通常是本机回环地址，默认视为可信；其它部署若确实位于
// 反向代理之后，可设置 TRUST_PROXY_HEADERS=1 显式开启。否则一律用 RemoteAddr，
// 避免攻击者伪造请求头绕过锁定。
const (
	authFailureWindow = 10 * time.Minute
	authMaxFailures   = 5
	authLockoutPeriod = 10 * time.Minute
)

type authFailState struct {
	times       []time.Time
	lockedUntil time.Time
}

var (
	authFailures sync.Map // ip -> *authFailState
	authMu       sync.Mutex
)

func ClientIP(r *http.Request) string {
	remote := remoteIP(r)
	if !trustedProxy(remote) {
		return remote
	}
	if v := headerIP(r.Header.Get("CF-Connecting-IP")); v != "" {
		return v
	}
	if v := headerIP(r.Header.Get("X-Forwarded-For")); v != "" {
		return v
	}
	return remote
}

func remoteIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return strings.Trim(host, "[]")
	}
	return r.RemoteAddr
}

func trustedProxy(remote string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("TRUST_PROXY_HEADERS"))) {
	case "1", "true", "yes", "on":
		return true
	}
	ip := net.ParseIP(remote)
	return ip != nil && ip.IsLoopback()
}

// headerIP 只接受单个合法 IP，避免把任意字符串当成限速键。
func headerIP(raw string) string {
	value := strings.TrimSpace(strings.Split(raw, ",")[0])
	if net.ParseIP(value) == nil {
		return ""
	}
	return value
}

// AuthLocked 返回该来源是否处于锁定状态及剩余时长
func AuthLocked(ip string) (time.Duration, bool) {
	authMu.Lock()
	defer authMu.Unlock()
	v, ok := authFailures.Load(ip)
	if !ok {
		return 0, false
	}
	st := v.(*authFailState)
	now := time.Now()
	if now.Before(st.lockedUntil) {
		return st.lockedUntil.Sub(now), true
	}
	return 0, false
}

// RecordAuthFailure 记录一次失败，达到阈值返回锁定时长（0 表示未触发锁定）
func RecordAuthFailure(ip string) time.Duration {
	authMu.Lock()
	defer authMu.Unlock()

	now := time.Now()
	v, _ := authFailures.LoadOrStore(ip, &authFailState{})
	st := v.(*authFailState)

	kept := st.times[:0]
	for _, ts := range st.times {
		if now.Sub(ts) <= authFailureWindow {
			kept = append(kept, ts)
		}
	}
	st.times = append(kept, now)

	if len(st.times) >= authMaxFailures {
		st.lockedUntil = now.Add(authLockoutPeriod)
		st.times = nil
		return authLockoutPeriod
	}
	return 0
}

// ResetAuthFailures 登录成功后清空该来源的失败记录
func ResetAuthFailures(ip string) {
	authMu.Lock()
	defer authMu.Unlock()
	authFailures.Delete(ip)
}
