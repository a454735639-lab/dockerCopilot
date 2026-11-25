package utiles

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
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
	default:
		return "generic"
	}
}
