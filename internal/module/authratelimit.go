package module

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

// 登录失败限速：同一来源 10 分钟内失败 5 次，锁定 10 分钟。
//
// 面板可能经 Cloudflare 隧道暴露公网，此时 RemoteAddr 是 cloudflared 本机，
// 所有访问者共享同一个 IP，限速会误伤——所以来源优先取
// CF-Connecting-IP（由 Cloudflare 边缘写入，客户端伪造会被覆盖），
// 其次 X-Forwarded-For 第一跳，最后才是 RemoteAddr。
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
	if v := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); v != "" {
		return v
	}
	if xf := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xf != "" {
		parts := strings.Split(xf, ",")
		return strings.TrimSpace(parts[0])
	}
	if idx := strings.LastIndex(r.RemoteAddr, ":"); idx > 0 {
		return strings.TrimSpace(strings.Trim(r.RemoteAddr[:idx], "[]"))
	}
	return r.RemoteAddr
}

// AuthLocked 返回该来源是否处于锁定状态及剩余时长
func AuthLocked(ip string) (time.Duration, bool) {
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
