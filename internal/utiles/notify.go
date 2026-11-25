package utiles

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	Result     string `json:"result"` // success / skipped / failed / rollback
	Message    string `json:"message"`
	DurationMS int64  `json:"duration_ms"`
	Time       string `json:"time"`
}

// SendTestMessage 同步发送测试通知，用于验证配置（失败时返回错误）
func SendTestMessage(rawURL string, p NotifyPayload) error {
	if rawURL == "" {
		return fmt.Errorf("通知地址为空")
	}
	format := notifyFormat(rawURL)
	if format == "wecom_app" {
		return sendWecomApp(p)
	}
	body, contentType, err := buildNotifyBody(rawURL, p)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", rawURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	client := wecomHTTPClient()
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
	rawURL := strings.TrimSpace(settings.Str("NOTIFY_URL"))
	if rawURL == "" {
		return
	}
	if p.Time == "" {
		p.Time = time.Now().Format(time.RFC3339)
	}
	go func() {
		if err := sendNotify(rawURL, p); err != nil {
			logx.Errorf("更新通知发送失败: %v", err)
		}
	}()
}

var notifiedAvailable sync.Map // 镜像:tag -> 已通知过的远端 digest

// NotifyAvailable 每轮检查结束后，对"有新版本"的镜像推一条通知。
// 同一 digest 只推一次，不会每小时重复轰炸。
// 不开启 AUTO_UPDATE 时，它等价于 Watchtower 的 monitor-only：只提醒、不动容器。
func NotifyAvailable(serviceContext *svc.ServiceContext) {
	if strings.TrimSpace(settings.Str("NOTIFY_URL")) == "" {
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

func sendNotify(rawURL string, p NotifyPayload) error {
	format := notifyFormat(rawURL)
	if format == "wecom_app" {
		return sendWecomApp(p)
	}
	body, contentType, err := buildNotifyBody(rawURL, p)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", rawURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("通知接口返回 %d", resp.StatusCode)
	}
	return nil
}

func buildNotifyBody(rawURL string, p NotifyPayload) ([]byte, string, error) {
	format := strings.ToLower(strings.TrimSpace(settings.Str("NOTIFY_FORMAT")))
	if format == "" {
		format = guessNotifyFormat(rawURL)
	}
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
	}[p.Result]
	if icon == "" {
		icon = "ℹ️"
	}
	return fmt.Sprintf("%s %s %s\n镜像: %s\n%s\n耗时: %dms",
		icon, p.Container, p.Result, p.Image, p.Message, p.DurationMS)
}

// notifyFormat 解析生效的通知格式：显式设置 > 按地址推断
func notifyFormat(rawURL string) string {
	if f := strings.ToLower(strings.TrimSpace(settings.Str("NOTIFY_FORMAT"))); f != "" {
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

var (
	wecomTokenMu     sync.Mutex
	wecomTokenCache  string
	wecomTokenExpiry time.Time
)

// getWecomToken 拉取并缓存 access_token（官方有效期 7200s，这里缓存 1 小时）
func getWecomToken(corpID, secret string) (string, error) {
	wecomTokenMu.Lock()
	defer wecomTokenMu.Unlock()
	if wecomTokenCache != "" && time.Now().Before(wecomTokenExpiry) {
		return wecomTokenCache, nil
	}
	reqURL := fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/gettoken?corpid=%s&corpsecret=%s",
		url.QueryEscape(corpID), url.QueryEscape(secret))
	respBody, err := fetchJSON(reqURL)
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
		return "", fmt.Errorf("获取 access_token 失败: %s", tokenResp.ErrMsg)
	}
	wecomTokenCache = tokenResp.AccessToken
	wecomTokenExpiry = time.Now().Add(1 * time.Hour)
	return wecomTokenCache, nil
}

// wecomHTTPClient 企业微信专用 HTTP 客户端。
// NAS 上通常设了 HTTP_PROXY/HTTPS_PROXY（走国际代理），但 qyapi.weixin.qq.com 是国内 API，
// 走代理反而慢甚至不通。WECOM_PROXY 可单独指定：
//
//	"none"   直连（不走系统代理，默认值）
//	""       跟随系统 HTTP_PROXY/HTTPS_PROXY
//	其他     走指定代理（如 socks5://... 或 http://...）
func wecomHTTPClient() *http.Client {
	proxyURL := strings.TrimSpace(settings.Str("WECOM_PROXY"))
	switch {
	case proxyURL == "" || proxyURL == "none":
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
func fetchJSON(reqURL string) ([]byte, error) {
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := wecomHTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return io.ReadAll(resp.Body)
}

// sendWecomApp 发送企业微信应用消息（touser 默认 @all）
func sendWecomApp(p NotifyPayload) error {
	corpID := strings.TrimSpace(settings.Str("WECOM_CORP_ID"))
	secret := strings.TrimSpace(settings.Str("WECOM_SECRET"))
	agentID := strings.TrimSpace(settings.Str("WECOM_AGENT_ID"))
	toUser := strings.TrimSpace(settings.Str("WECOM_TOUSER"))
	if corpID == "" || secret == "" || agentID == "" {
		return fmt.Errorf("企业微信未配置完整（WECOM_CORP_ID / WECOM_SECRET / WECOM_AGENT_ID）")
	}
	if toUser == "" {
		toUser = "@all"
	}
	token, err := getWecomToken(corpID, secret)
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
	resp, err := wecomHTTPClient().Do(req)
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
			wecomTokenMu.Lock()
			wecomTokenCache = ""
			wecomTokenExpiry = time.Time{}
			wecomTokenMu.Unlock()
		}
		return fmt.Errorf("企业微信返回 errcode=%d: %s", sendResp.ErrCode, sendResp.ErrMsg)
	}
	return nil
}
