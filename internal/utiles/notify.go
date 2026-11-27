package utiles

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/onlyLTY/dockerCopilot/internal/settings"
	"github.com/onlyLTY/dockerCopilot/internal/svc"
	"github.com/zeromicro/go-zero/core/logx"
)

// NotifyPayload 一次更新任务的结果。NOTIFY_URL 收到 generic 格式时就是这段 JSON。
type NotifyPayload struct {
	Container  string `json:"container"`
	Image      string `json:"image"`
	Result     string `json:"result"` // success / skipped / failed / rollback / test / available / event
	Message    string `json:"message"`
	DurationMS int64  `json:"duration_ms"`
	Time       string `json:"time"`
}

// NotifyConf 一次通知所需的全部配置。
// 生产路径用 confFromSettings() 从已保存配置构造；
// 测试路径从请求体表单当前值构造（不写数据库）。
type NotifyConf struct {
	URL        string
	Format     string
	CorpID     string
	Secret     string
	AgentID    string
	ToUser     string
	WecomProxy string
}

func confFromSettings() NotifyConf {
	return NotifyConf{
		URL:        settings.Str(settings.KeyNotifyURL),
		Format:     settings.Str(settings.KeyNotifyFormat),
		CorpID:     settings.Str(settings.KeyWecomCorpID),
		Secret:     settings.Str(settings.KeyWecomSecret),
		AgentID:    settings.Str(settings.KeyWecomAgentID),
		ToUser:     settings.Str(settings.KeyWecomToUser),
		WecomProxy: settings.Str(settings.KeyWecomProxy),
	}
}

// SendTestMessage 同步发送测试通知，失败时返回带具体原因的错误。
// 企业微信应用模式不要求 URL；其它模式要求 URL 非空。
func SendTestMessage(c NotifyConf, p NotifyPayload) error {
	if notifyFormat(c, c.URL) == "wecom_app" {
		clearWecomToken() // 测试时用表单当前值，强制重新取 token
	} else if strings.TrimSpace(c.URL) == "" {
		return fmt.Errorf("通知地址为空，请先填入 webhook 地址或选择企业微信应用模式")
	}
	return sendNotify(c, c.URL, p)
}

// notifyConfigured 判断通知是否可用：企业微信应用模式不需要 URL。
func notifyConfigured(c NotifyConf) bool {
	return notifyFormat(c, c.URL) == "wecom_app" || strings.TrimSpace(c.URL) != ""
}

// NotifyUpdate 异步推送更新结果：失败只记日志，绝不影响更新本身。
//
//	NOTIFY_URL=https://api.day.app/xxxx        （自动识别 bark）
//	NOTIFY_URL=https://oapi.dingtalk.com/robot/send?access_token=xxx
//	NOTIFY_URL=https://open.feishu.cn/open-apis/bot/v2/hook/xxx
//	NOTIFY_URL=https://ntfy.sh/mytopic
//	NOTIFY_URL=https://example.com/hook        （generic，原样 POST JSON）
//
// 识别不准时用 NOTIFY_FORMAT=bark|dingtalk|feishu|ntfy|generic 强制指定。
func NotifyUpdate(p NotifyPayload) {
	c := confFromSettings()
	if !notifyConfigured(c) {
		return
	}
	if p.Time == "" {
		p.Time = time.Now().Format(time.RFC3339)
	}
	go func() {
		if err := sendNotify(c, c.URL, p); err != nil {
			logx.Errorf("更新通知发送失败: %v", err)
		}
	}()
}

var notifiedAvailable sync.Map // 镜像:tag -> 已通知过的远端 digest

// NotifyAvailable 每轮检查结束后，对"有新版本"的镜像推一条通知。
// 同一 digest 只推一次，不会每小时重复轰炸。
// 不开启 AUTO_UPDATE 时，它等价于 Watchtower 的 monitor-only：只提醒、不动容器。
func NotifyAvailable(serviceContext *svc.ServiceContext) {
	if !notifyConfigured(confFromSettings()) {
		return
	}
	for _, item := range serviceContext.HubImageInfo.Snapshot() {
		if !item.NeedUpdate || item.Digest == "" || item.Image == "" {
			continue
		}
		if prev, ok := notifiedAvailable.Load(item.Image); ok && prev == item.Digest {
			continue
		}
		notifiedAvailable.Store(item.Image, item.Digest)
		NotifyUpdate(NotifyPayload{
			Container: "-",
			Image:     item.Image,
			Result:    "available",
			Message:   "发现新版本（尚未更新）",
		})
	}
}

// NotifyEvent 推送一条通用事件（如登录失败告警）
func NotifyEvent(title, body string) {
	NotifyUpdate(NotifyPayload{
		Container: "-",
		Image:     "-",
		Result:    "event",
		Message:   title + "：" + body,
	})
}

func sendNotify(c NotifyConf, rawURL string, p NotifyPayload) error {
	format := notifyFormat(c, rawURL)
	if format == "wecom_app" {
		return sendWecomApp(c, p)
	}
	// 校验阶段就把域名解析成具体 IP；不能只校验一次再让 HTTP 客户端重新解析，
	// 否则攻击者可以用 DNS rebinding 在校验后把域名指到内网。
	dialAddr, err := resolveNotifyURL(rawURL)
	if err != nil {
		return err
	}
	body, contentType, err := buildNotifyBody(c, rawURL, p)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", rawURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	client := wecomHTTPClient(c)
	if dialAddr != "" && !notifyProxyInUse(c, req) {
		client = pinnedHTTPClient(client, dialAddr)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("通知接口返回 %d", resp.StatusCode)
	}
	return nil
}

func buildNotifyBody(c NotifyConf, rawURL string, p NotifyPayload) ([]byte, string, error) {
	format := notifyFormat(c, rawURL)
	text := notifyText(p)
	const jsonType = "application/json; charset=utf-8"

	switch format {
	case "wecom_webhook":
		body, err := json.Marshal(map[string]interface{}{
			"msgtype":  "markdown",
			"markdown": map[string]string{"content": notifyText(p)},
		})
		return body, jsonType, err
	case "wecom_app":
		// 应用消息走 sendWecomApp（两段请求），buildNotifyBody 不会到这里
		return []byte("{}"), jsonType, nil
	case "bark":
		body, err := json.Marshal(map[string]string{
			"title": "dockerCopilot " + p.Result,
			"body":  text,
			"group": "dockerCopilot",
		})
		return body, jsonType, err
	case "dingtalk":
		body, err := json.Marshal(map[string]interface{}{
			"msgtype": "text",
			"text":    map[string]string{"content": text},
		})
		return body, jsonType, err
	case "feishu":
		body, err := json.Marshal(map[string]interface{}{
			"msg_type": "text",
			"content":  map[string]string{"text": text},
		})
		return body, jsonType, err
	case "ntfy":
		return []byte(text), "text/plain; charset=utf-8", nil
	default: // generic
		body, err := json.Marshal(p)
		return body, jsonType, err
	}
}

func notifyText(p NotifyPayload) string {
	icon := map[string]string{
		"success":   "✅",
		"skipped":   "⏭",
		"failed":    "❌",
		"rollback":  "↩️",
		"available": "🆕",
		"event":     "🔔",
		"test":      "🧪",
	}[p.Result]
	if icon == "" {
		icon = "ℹ️"
	}
	return fmt.Sprintf("%s %s %s\n镜像: %s\n%s\n耗时: %dms",
		icon, p.Container, p.Result, p.Image, p.Message, p.DurationMS)
}

// resolveNotifyURL 防止把通知 webhook 当成 SSRF 探针：只允许公网 http/https，
// 并返回本次校验解析出的 "IP:端口"，供实际请求固定使用，避免 DNS rebinding。
// 内网 ntfy 等自建服务可设置 NOTIFY_ALLOW_PRIVATE=1 显式放行。
func resolveNotifyURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("通知地址无效: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("通知地址仅支持 http/https")
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("通知地址缺少域名")
	}
	allowPrivate := strings.ToLower(strings.TrimSpace(os.Getenv("NOTIFY_ALLOW_PRIVATE"))) == "1"
	ips, err := net.LookupIP(host)
	if err != nil {
		return "", fmt.Errorf("无法解析通知域名 %s: %w", host, err)
	}
	var picked net.IP
	for _, ip := range ips {
		if ip == nil {
			continue
		}
		if !allowPrivate && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() || ip.IsUnspecified()) {
			return "", fmt.Errorf("通知地址不允许指向内网/本机地址: %s", host)
		}
		if picked == nil {
			picked = ip
		}
	}
	if picked == nil {
		return "", fmt.Errorf("通知域名 %s 没有可用解析结果", host)
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return net.JoinHostPort(picked.String(), port), nil
}

// notifyProxyInUse 判断这次请求是否会走代理；走代理时目标 DNS 由代理解析，
// 本地固定 IP 反而会连错地址，因此不套用 pinnedHTTPClient。
func notifyProxyInUse(c NotifyConf, req *http.Request) bool {
	if p := strings.TrimSpace(c.WecomProxy); p != "" && p != "none" {
		return true
	}
	proxyURL, err := http.ProxyFromEnvironment(req)
	return err == nil && proxyURL != nil
}

// pinnedHTTPClient 把已校验的 IP 固定到 DialContext。URL 的主机名保持不变，
// 因此 Host 头和 TLS SNI 仍然指向原域名，证书校验不受影响。
func pinnedHTTPClient(base *http.Client, dialAddr string) *http.Client {
	var tr *http.Transport
	if base != nil && base.Transport != nil {
		if t, ok := base.Transport.(*http.Transport); ok {
			tr = t.Clone()
		}
	}
	if tr == nil {
		tr = http.DefaultTransport.(*http.Transport).Clone()
	}
	dialer := &net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}
	tr.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, "tcp", dialAddr)
	}
	timeout := 8 * time.Second
	if base != nil && base.Timeout > 0 {
		timeout = base.Timeout
	}
	return &http.Client{Timeout: timeout, Transport: tr}
}

// notifyFormat 解析生效的通知格式：显式设置 > 按地址推断
func notifyFormat(c NotifyConf, rawURL string) string {
	if f := strings.ToLower(strings.TrimSpace(c.Format)); f != "" {
		return f
	}
	return guessNotifyFormat(rawURL)
}

func guessNotifyFormat(rawURL string) string {
	lower := strings.ToLower(rawURL)
	switch {
	case strings.Contains(lower, "api.day.app"):
		return "bark"
	case strings.Contains(lower, "oapi.dingtalk.com"):
		return "dingtalk"
	case strings.Contains(lower, "open.feishu.cn"), strings.Contains(lower, "open.larksuite.com"):
		return "feishu"
	case strings.Contains(lower, "ntfy"):
		return "ntfy"
	case strings.Contains(lower, "qyapi.weixin.qq.com") && strings.Contains(lower, "webhook"):
		return "wecom_webhook"
	default:
		return "generic"
	}
}

// ---------------- 企业微信应用消息（MP 同款：Corpid+Secret+Agentid）----------------

type wecomTokenEntry struct {
	token  string
	expiry time.Time
}

var (
	wecomTokenMu    sync.Mutex
	wecomTokenCache = map[string]wecomTokenEntry{}
)

func clearWecomToken() {
	wecomTokenMu.Lock()
	wecomTokenCache = map[string]wecomTokenEntry{}
	wecomTokenMu.Unlock()
}

// getWecomToken 拉取并缓存 access_token（官方有效期 7200s，这里缓存 1 小时）。
// 缓存按 corpID+secret 区分，换凭证后不会继续用旧 token；网络请求放在锁外。
func getWecomToken(c NotifyConf, corpID, secret string) (string, error) {
	key := corpID + "\x00" + secret
	wecomTokenMu.Lock()
	entry, ok := wecomTokenCache[key]
	wecomTokenMu.Unlock()
	if ok && entry.token != "" && time.Now().Before(entry.expiry) {
		return entry.token, nil
	}

	reqURL := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/gettoken?corpid=%s&corpsecret=%s",
		url.QueryEscape(corpID), url.QueryEscape(secret))
	respBody, err := fetchJSON(c, reqURL)
	if err != nil {
		return "", err
	}
	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
	}
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return "", err
	}
	if tokenResp.ErrCode != 0 || tokenResp.AccessToken == "" {
		return "", fmt.Errorf("获取 access_token 失败(errcode=%d): %s", tokenResp.ErrCode, tokenResp.ErrMsg)
	}
	wecomTokenMu.Lock()
	wecomTokenCache[key] = wecomTokenEntry{
		token:  tokenResp.AccessToken,
		expiry: time.Now().Add(1 * time.Hour),
	}
	wecomTokenMu.Unlock()
	return tokenResp.AccessToken, nil
}

// wecomHTTPClient 企业微信专用 HTTP 客户端。
// NAS 上通常设了 HTTP_PROXY/HTTPS_PROXY（走国际代理），但 qyapi.weixin.qq.com 是国内 API，
// 走代理反而慢甚至不通。c.WecomProxy 可单独指定：
//
//	"none"   直连（不走系统代理，默认值）
//	""       跟随系统 HTTP_PROXY/HTTPS_PROXY
//	其他     走指定代理（如 socks5://... 或 http://...）
func wecomHTTPClient(c NotifyConf) *http.Client {
	proxyURL := strings.TrimSpace(c.WecomProxy)
	switch {
	case proxyURL == "":
		return &http.Client{Timeout: 8 * time.Second}
	case proxyURL == "none":
		return &http.Client{
			Timeout: 8 * time.Second,
			Transport: &http.Transport{
				Proxy: nil, // 直连，不走系统代理
			},
		}
	default:
		parsed, err := url.Parse(proxyURL)
		if err != nil {
			return &http.Client{Timeout: 8 * time.Second}
		}
		return &http.Client{
			Timeout: 8 * time.Second,
			Transport: &http.Transport{
				Proxy: http.ProxyURL(parsed),
			},
		}
	}
}

// fetchJSON GET 一个 JSON 接口并返回原始字节（走企业微信专用客户端）
func fetchJSON(c NotifyConf, reqURL string) ([]byte, error) {
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := wecomHTTPClient(c).Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return io.ReadAll(resp.Body)
}

// sendWecomApp 发送企业微信应用消息（touser 默认 @all）
func sendWecomApp(c NotifyConf, p NotifyPayload) error {
	corpID := strings.TrimSpace(c.CorpID)
	secret := strings.TrimSpace(c.Secret)
	agentID := strings.TrimSpace(c.AgentID)
	toUser := strings.TrimSpace(c.ToUser)
	if corpID == "" || secret == "" || agentID == "" {
		return fmt.Errorf("企业微信应用模式凭证不完整（Corp ID / Secret / Agent ID 必填）")
	}
	if toUser == "" {
		toUser = "@all"
	}
	token, err := getWecomToken(c, corpID, secret)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]interface{}{
		"touser":  toUser,
		"msgtype": "text",
		"agentid": agentID,
		"text":    map[string]string{"content": notifyText(p)},
	})
	if err != nil {
		return err
	}
	sendURL := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/message/send?access_token=%s", url.QueryEscape(token))
	req, err := http.NewRequest("POST", sendURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := wecomHTTPClient(c).Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var sendResp struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(raw, &sendResp); err != nil {
		return err
	}
	if sendResp.ErrCode != 0 {
		// token 失效时清缓存，下次重新取
		if sendResp.ErrCode == 40014 || sendResp.ErrCode == 42001 {
			clearWecomToken()
		}
		return fmt.Errorf("企业微信返回 errcode=%d: %s", sendResp.ErrCode, sendResp.ErrMsg)
	}
	return nil
}
