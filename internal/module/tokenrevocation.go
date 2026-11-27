package module

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/rest/httpx"
)

// JWT 吊销表：登出后把 token 拉黑到它自身的 exp 为止。
// 用原始 token 字符串做键，避免中间件为了校验吊销而依赖签名密钥。
var (
	revokeMu      sync.RWMutex
	revokedTokens = map[string]time.Time{}
)

// BearerToken 从 Authorization 头提取 token，不做签名校验。
func BearerToken(r *http.Request) string {
	raw := strings.TrimSpace(r.Header.Get("Authorization"))
	parts := strings.SplitN(raw, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

// RevokeToken 吊销当前请求携带的 token。解析不出 exp 时兜底保留 24 小时。
func RevokeToken(r *http.Request) {
	token := BearerToken(r)
	if token == "" {
		return
	}
	until := time.Now().Add(24 * time.Hour)
	if exp, ok := tokenExpiry(token); ok {
		t := time.Unix(exp, 0)
		if t.After(time.Now()) && t.Before(until) {
			until = t
		}
	}
	revokeMu.Lock()
	revokedTokens[token] = until
	purgeRevokedLocked()
	revokeMu.Unlock()
}

// IsTokenRevoked 判断 token 是否已被登出。
func IsTokenRevoked(token string) bool {
	revokeMu.RLock()
	until, ok := revokedTokens[token]
	revokeMu.RUnlock()
	return ok && time.Now().Before(until)
}

// TokenRevocationMiddleware 全局拦截已吊销 token，作为 server.Use 注册。
func TokenRevocationMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if token := BearerToken(r); token != "" && IsTokenRevoked(token) {
			httpx.WriteJson(w, http.StatusUnauthorized, map[string]interface{}{
				"code": http.StatusUnauthorized,
				"msg":  "登录已失效，请重新登录",
				"data": map[string]interface{}{},
			})
			return
		}
		next(w, r)
	}
}

// tokenExpiry 只解析 JWT 的 payload（不验签），用于确定吊销条目保留多久。
func tokenExpiry(token string) (int64, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 0, false
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, false
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(data, &claims); err != nil || claims.Exp <= 0 {
		return 0, false
	}
	return claims.Exp, true
}

func purgeRevokedLocked() {
	now := time.Now()
	for token, until := range revokedTokens {
		if now.After(until) {
			delete(revokedTokens, token)
		}
	}
}
