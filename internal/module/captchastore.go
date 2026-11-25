package module

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// 图形验证码：4 位随机字符 + 干扰线/噪点的 SVG，一次性使用，2 分钟过期。
// 自绘而不接 Cloudflare Turnstile：面板常经 Tailscale IP（100.x）直连，
// Turnstile 需要域名白名单，IP 访问会失效。
const (
	captchaTTL      = 2 * time.Minute
	captchaAlphabet = "abcdefghjkmnpqrstuvwxyz23456789" // 去掉 0/o/1/l/i 等易混字符
)

type captchaEntry struct {
	code    string
	expires time.Time
}

var (
	captchaMu    sync.Mutex
	captchaStore = map[string]captchaEntry{}
)

// captchaRandInt 返回 [0, n) 的密码学随机数；n<=0 或出错时退回时间取模
func captchaRandInt(n int) int {
	if n <= 0 {
		return 0
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return int(time.Now().UnixNano() % int64(n))
	}
	return int(v.Int64())
}

// GenCaptcha 生成一个新验证码，返回 id / 渲染好的 SVG / 明文（明文仅在 CAPTCHA_DEBUG=1 时对外返回）
func GenCaptcha() (id, svg, code string) {
	purgeCaptchas()

	var b strings.Builder
	for i := 0; i < 4; i++ {
		b.WriteString(string(captchaAlphabet[captchaRandInt(len(captchaAlphabet))]))
	}
	code = b.String()
	id = uuid.NewString()
	svg = renderCaptcha(code)

	captchaMu.Lock()
	captchaStore[id] = captchaEntry{code: strings.ToLower(code), expires: time.Now().Add(captchaTTL)}
	captchaMu.Unlock()
	return id, svg, code
}

// ValidateCaptcha 校验并消费验证码（无论对错都用掉，防止重放）
func ValidateCaptcha(id, code string) bool {
	if id == "" || strings.TrimSpace(code) == "" {
		return false
	}
	captchaMu.Lock()
	defer captchaMu.Unlock()
	entry, ok := captchaStore[id]
	if !ok {
		return false
	}
	delete(captchaStore, id)
	if time.Now().After(entry.expires) {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(code), entry.code)
}

func purgeCaptchas() {
	captchaMu.Lock()
	defer captchaMu.Unlock()
	now := time.Now()
	for id, entry := range captchaStore {
		if now.After(entry.expires) {
			delete(captchaStore, id)
		}
	}
}

func renderCaptcha(code string) string {
	colors := []string{"#0ea5e9", "#f59e0b", "#10b981", "#8b5cf6", "#ef4444", "#3b82f6"}
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="120" height="44" viewBox="0 0 120 44" role="img" aria-label="验证码">`)
	fmt.Fprintf(&b, `<rect width="120" height="44" rx="6" fill="#eef2f7"/>`)
	for i := 0; i < 4; i++ {
		c := colors[captchaRandInt(len(colors))]
		fmt.Fprintf(&b, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="%s" stroke-width="1.2" opacity="0.5"/>`,
			captchaRandInt(110), captchaRandInt(44), captchaRandInt(110), captchaRandInt(44), c)
	}
	for i, ch := range code {
		x := 14 + i*24 + captchaRandInt(6)
		y := 30 - captchaRandInt(6)
		rot := -18 + captchaRandInt(36)
		c := colors[captchaRandInt(len(colors))]
		fmt.Fprintf(&b, `<text x="%d" y="%d" font-family="monospace" font-size="22" font-weight="700" fill="%s" transform="rotate(%d %d %d)">%c</text>`,
			x, y, c, rot, x, y)
	}
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&b, `<circle cx="%d" cy="%d" r="1" fill="#94a3b8" opacity="0.4"/>`,
			captchaRandInt(120), captchaRandInt(44))
	}
	b.WriteString(`</svg>`)
	return b.String()
}
