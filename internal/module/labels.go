package module

import (
	"strconv"
	"strings"
)

// 容器级策略标签（compose 里 labels: / docker run --label）：
//
//	dockercopilot.check=false        不检查该容器的镜像更新（面板不出"可更新"）
//	dockercopilot.auto-update=false  定时自动更新跳过该容器
//	dockercopilot.keep=3             清理旧镜像时，该容器所用仓库保留 3 个版本
//
// 不加标签 = 保持默认行为。环境变量仍是全局开关，标签只做单容器例外。
const (
	LabelCheck      = "dockercopilot.check"
	LabelAutoUpdate = "dockercopilot.auto-update"
	LabelKeep       = "dockercopilot.keep"
)

// LabelBool 读取布尔标签，取不到或值非法时返回 def
func LabelBool(labels map[string]string, key string, def bool) bool {
	if labels == nil {
		return def
	}
	raw, ok := labels[key]
	if !ok {
		return def
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

// LabelInt 读取正整数标签，取不到或值非法时返回 def
func LabelInt(labels map[string]string, key string, def int) int {
	if labels == nil {
		return def
	}
	raw, ok := labels[key]
	if !ok {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		return def
	}
	return n
}
