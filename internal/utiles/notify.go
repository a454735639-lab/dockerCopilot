package utiles

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

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
	rawURL := strings.TrimSpace(os.Getenv("NOTIFY_URL"))
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
	format := strings.ToLower(strings.TrimSpace(os.Getenv("NOTIFY_FORMAT")))
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
		"success":  "✅",
		"skipped":  "⏭",
		"failed":   "❌",
		"rollback": "↩️",
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
