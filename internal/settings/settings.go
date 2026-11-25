package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/zeromicro/go-zero/core/logx"
)

// 运行时可编辑的配置，优先级：本文件里的非空值 > 环境变量。
// 环境变量仍然是全局默认值，UI 改动写进 Path 后立即生效（无需重启容器）。
const (
	Path = "/data/settings.json"

	KeyIgnoreImages      = "IGNORE_IMAGES"
	KeyIgnoreContainers  = "IGNORE_CONTAINERS"
	KeyNotifyURL         = "NOTIFY_URL"
	KeyNotifyFormat      = "NOTIFY_FORMAT"
	KeyAutoUpdate        = "AUTO_UPDATE"
	KeyAutoUpdateCron    = "AUTO_UPDATE_CRON"
	KeyAutoUpdateInclude = "AUTO_UPDATE_INCLUDE"
	KeyAutoUpdateMax     = "AUTO_UPDATE_MAX"
	KeyPruneOldImages    = "PRUNE_OLD_IMAGES"
	KeyPruneKeep         = "PRUNE_KEEP"
	KeyPruneDangling     = "PRUNE_DANGLING"
	KeyPruneDryRun       = "PRUNE_DRY_RUN"
	KeyWecomCorpID       = "WECOM_CORP_ID"
	KeyWecomSecret       = "WECOM_SECRET"
	KeyWecomAgentID      = "WECOM_AGENT_ID"
	KeyWecomToUser       = "WECOM_TOUSER"
	KeyWecomProxy        = "WECOM_PROXY"
)

// Config 就是上面那些键的可编辑版本（值统一用字符串，方便表单直接绑定）
type Config struct {
	IgnoreImages      string `json:"ignoreImages"`
	IgnoreContainers  string `json:"ignoreContainers"`
	NotifyURL         string `json:"notifyUrl"`
	NotifyFormat      string `json:"notifyFormat"`
	AutoUpdate        string `json:"autoUpdate"`
	AutoUpdateCron    string `json:"autoUpdateCron"`
	AutoUpdateInclude string `json:"autoUpdateInclude"`
	AutoUpdateMax     string `json:"autoUpdateMax"`
	PruneOldImages    string `json:"pruneOldImages"`
	PruneKeep         string `json:"pruneKeep"`
	PruneDangling     string `json:"pruneDangling"`
	PruneDryRun       string `json:"pruneDryRun"`
	WecomCorpID       string `json:"wecomCorpId"`
	WecomSecret       string `json:"wecomSecret"`
	WecomAgentID      string `json:"wecomAgentId"`
	WecomToUser       string `json:"wecomToUser"`
	WecomProxy        string `json:"wecomProxy"`
}

var (
	mu       sync.RWMutex
	current  Config
	onChange []func()
)

// envKeys 环境变量名 -> Config 字段名
var envKeys = map[string]string{
	KeyIgnoreImages:      "ignoreImages",
	KeyIgnoreContainers:  "ignoreContainers",
	KeyNotifyURL:         "notifyUrl",
	KeyNotifyFormat:      "notifyFormat",
	KeyAutoUpdate:        "autoUpdate",
	KeyAutoUpdateCron:    "autoUpdateCron",
	KeyAutoUpdateInclude: "autoUpdateInclude",
	KeyAutoUpdateMax:     "autoUpdateMax",
	KeyPruneOldImages:    "pruneOldImages",
	KeyPruneKeep:         "pruneKeep",
	KeyPruneDangling:     "pruneDangling",
	KeyPruneDryRun:       "pruneDryRun",
	KeyWecomCorpID:       "wecomCorpId",
	KeyWecomSecret:       "wecomSecret",
	KeyWecomAgentID:      "wecomAgentId",
	KeyWecomToUser:       "wecomToUser",
	KeyWecomProxy:        "wecomProxy",
}

// Init 启动时读取已保存的配置；文件不存在或损坏时按空配置启动（退回环境变量）
func Init() {
	mu.Lock()
	defer mu.Unlock()
	raw, err := os.ReadFile(Path)
	if err != nil {
		current = Config{}
		return
	}
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		logx.Errorf("读取 %s 失败，按空配置启动: %v", Path, err)
		current = Config{}
		return
	}
	current = c
}

// Str 按环境变量名取值：文件里的非空值优先，否则退回环境变量
func Str(envKey string) string {
	if field, ok := envKeys[envKey]; ok {
		mu.RLock()
		v := pick(current, field)
		mu.RUnlock()
		if v != "" {
			return v
		}
	}
	return os.Getenv(envKey)
}

// FromFile 报告某个键当前是否由文件（UI）设置，供 UI 提示"环境变量已忽略"
func FromFile(envKey string) bool {
	field, ok := envKeys[envKey]
	if !ok {
		return false
	}
	mu.RLock()
	defer mu.RUnlock()
	return pick(current, field) != ""
}

// Get 返回当前文件配置（不含环境变量兜底值）
func Get() Config {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// EnvValues 返回环境变量里设置的原始值，供 UI 显示"环境变量默认值"
func EnvValues() map[string]string {
	out := make(map[string]string, len(envKeys))
	for envKey := range envKeys {
		out[envKey] = os.Getenv(envKey)
	}
	return out
}

// Update 覆盖并落盘，返回是否真的写入了文件
func Update(c Config) error {
	mu.Lock()
	current = normalize(c)
	raw, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		mu.Unlock()
		return err
	}
	if err := os.MkdirAll(filepath.Dir(Path), 0o755); err != nil {
		mu.Unlock()
		return err
	}
	if err := os.WriteFile(Path, raw, 0o600); err != nil {
		mu.Unlock()
		return err
	}
	callbacks := append([]func(){}, onChange...)
	mu.Unlock()

	for _, fn := range callbacks {
		fn()
	}
	logx.Infof("设置已更新并写入 %s", Path)
	return nil
}

// OnChange 注册配置变更回调（例如重排自动更新的 cron）
func OnChange(fn func()) {
	mu.Lock()
	defer mu.Unlock()
	onChange = append(onChange, fn)
}

// EnvKeys 返回 环境变量名 -> Config 字段名 的映射（只读用途）
func EnvKeys() map[string]string {
	out := make(map[string]string, len(envKeys))
	for k, v := range envKeys {
		out[k] = v
	}
	return out
}

// FieldValue 取某个字段名的当前值
func FieldValue(c Config, field string) string {
	return pick(c, field)
}

func pick(c Config, field string) string {
	switch field {
	case "ignoreImages":
		return c.IgnoreImages
	case "ignoreContainers":
		return c.IgnoreContainers
	case "notifyUrl":
		return c.NotifyURL
	case "notifyFormat":
		return c.NotifyFormat
	case "autoUpdate":
		return c.AutoUpdate
	case "autoUpdateCron":
		return c.AutoUpdateCron
	case "autoUpdateInclude":
		return c.AutoUpdateInclude
	case "autoUpdateMax":
		return c.AutoUpdateMax
	case "pruneOldImages":
		return c.PruneOldImages
	case "pruneKeep":
		return c.PruneKeep
	case "pruneDangling":
		return c.PruneDangling
	case "pruneDryRun":
		return c.PruneDryRun
	case "wecomCorpId":
		return c.WecomCorpID
	case "wecomSecret":
		return c.WecomSecret
	case "wecomAgentId":
		return c.WecomAgentID
	case "wecomToUser":
		return c.WecomToUser
	case "wecomProxy":
		return c.WecomProxy
	}
	return ""
}

func normalize(c Config) Config {
	trim := func(s string) string { return strings.TrimSpace(s) }
	c.IgnoreImages = trim(c.IgnoreImages)
	c.IgnoreContainers = trim(c.IgnoreContainers)
	c.NotifyURL = trim(c.NotifyURL)
	c.NotifyFormat = strings.ToLower(trim(c.NotifyFormat))
	c.AutoUpdate = trim(c.AutoUpdate)
	c.AutoUpdateCron = trim(c.AutoUpdateCron)
	c.AutoUpdateInclude = trim(c.AutoUpdateInclude)
	c.AutoUpdateMax = trim(c.AutoUpdateMax)
	c.PruneOldImages = trim(c.PruneOldImages)
	c.PruneKeep = trim(c.PruneKeep)
	c.PruneDangling = trim(c.PruneDangling)
	c.PruneDryRun = trim(c.PruneDryRun)
	c.WecomCorpID = trim(c.WecomCorpID)
	c.WecomSecret = trim(c.WecomSecret)
	c.WecomAgentID = trim(c.WecomAgentID)
	c.WecomToUser = trim(c.WecomToUser)
	c.WecomProxy = trim(c.WecomProxy)
	if c.AutoUpdateCron == "" {
		c.AutoUpdateCron = "0 4 * * *"
	}
	if _, err := strconv.Atoi(c.AutoUpdateMax); c.AutoUpdateMax != "" && err != nil {
		c.AutoUpdateMax = ""
	}
	if _, err := strconv.Atoi(c.PruneKeep); c.PruneKeep != "" && err != nil {
		c.PruneKeep = ""
	}
	return c
}
