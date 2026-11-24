package types

import "github.com/onlyLTY/dockerCopilot/internal/settings"

// SettingsData GET /api/settings 返回的数据
type SettingsData struct {
	Values  map[string]string `json:"values"`  // 当前生效值（文件优先，其次环境变量）
	Sources map[string]string `json:"sources"` // "file"=UI 设置，"env"=环境变量
	Env     map[string]string `json:"env"`     // 环境变量原始值
}

// SettingsUpdateReq POST /api/settings 的请求体，字段与 Config 一致
type SettingsUpdateReq = settings.Config
