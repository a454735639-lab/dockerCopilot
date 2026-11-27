package module

import (
	"path"
	"strings"

	"github.com/onlyLTY/dockerCopilot/internal/settings"
)

// 忽略名单（环境变量，逗号分隔，支持 * 通配）：
//
//	IGNORE_IMAGES=redis:*,ghcr.io/foo/bar,nginx       匹配 "镜像名:tag" 或裸镜像名
//	IGNORE_CONTAINERS=db,redis-*                      匹配容器名
//
// 命中的镜像不再检查更新，命中的容器不再显示"可更新"标记。
// EnvList 读取逗号分隔的环境变量，返回小写去空后的列表
func EnvList(key string) []string {
	raw := strings.TrimSpace(settings.Str(key))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// MatchAny 大小写不敏感的通配匹配（path.Match 语义）
func MatchAny(patterns []string, value string) bool {
	if len(patterns) == 0 {
		return false
	}
	value = strings.ToLower(value)
	for _, p := range patterns {
		if p == value {
			return true
		}
		if ok, err := path.Match(p, value); err == nil && ok {
			return true
		}
	}
	return false
}

// IgnoredImage 判断镜像是否在忽略名单里
func IgnoredImage(imageName, imageTag string) bool {
	// 每次都读当前配置，保证 UI 保存后立即生效；用局部变量避免并发写共享状态。
	ignoreImages := EnvList("IGNORE_IMAGES")
	return MatchAny(ignoreImages, imageName+":"+imageTag) ||
		MatchAny(ignoreImages, imageName)
}

// IgnoredContainer 判断容器是否在忽略名单里（传不含前导 / 的容器名）
func IgnoredContainer(name string) bool {
	ignoreContainers := EnvList("IGNORE_CONTAINERS")
	return MatchAny(ignoreContainers, name)
}
