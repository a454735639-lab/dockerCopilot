package utiles

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/onlyLTY/dockerCopilot/internal/config"
	"github.com/zeromicro/go-zero/core/logx"
)

func GetRemoteVersion() (remoteVersion string, err error) {
	localVersion := config.Version
	if strings.Contains(localVersion, "FNOS") {
		logx.Infof("飞牛版本，无需在线更新")
		return localVersion, nil
	}

	githubProxy := os.Getenv("githubProxy")
	if githubProxy != "" {
		githubProxy = strings.TrimRight(githubProxy, "/") + "/"
	}
	// 版本文件放在默认分支上，按构建类型选择对应文件；原实现固定读 UGREEN 分支，
	// 导致 latest 构建检查的是错误的版本。
	versionFile := "version"
	if strings.Contains(strings.ToUpper(localVersion), "UGREEN") {
		versionFile = "ugreen_version"
	}
	versionURL := githubProxy + "https://raw.githubusercontent.com/onlyLTY/dockerCopilot/latest/" + versionFile
	remoteVersion, err = fetchVersionFromURL(versionURL)
	if err != nil {
		return "0.0.0", err
	}

	if CompareVersion(remoteVersion, localVersion) > 0 {
		logx.Infof("发现新版本! 本地: %s, 远程: %s", localVersion, remoteVersion)
		return remoteVersion, nil
	}
	logx.Infof("当前已是最新版本: %s", localVersion)
	return localVersion, nil
}

// CompareVersion 比较 v1 与 v2：返回 1 表示 v1 更新，0 相同，-1 更旧。
// 逐段比较，数字段按数值比较，非数字段按字符串比较，兼容 v 前缀。
func CompareVersion(v1, v2 string) int {
	segments := func(v string) []string {
		v = strings.TrimSpace(v)
		v = strings.TrimPrefix(v, "v")
		v = strings.TrimPrefix(v, "V")
		return strings.Split(v, ".")
	}
	a, b := segments(v1), segments(v2)
	for i := 0; i < len(a) || i < len(b); i++ {
		var av, bv string
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		ai, aErr := strconv.Atoi(av)
		bi, bErr := strconv.Atoi(bv)
		if aErr == nil && bErr == nil {
			if ai > bi {
				return 1
			}
			if ai < bi {
				return -1
			}
			continue
		}
		if av > bv {
			return 1
		}
		if av < bv {
			return -1
		}
	}
	return 0
}

func fetchVersionFromURL(url string) (string, error) {
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
		},
	}

	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			logx.Error("关闭Body失败:", err)
		}
	}(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("获取版本文件失败: HTTP %d", resp.StatusCode)
	}

	versionData, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	version := strings.TrimSpace(string(versionData))
	if version == "" || len(version) > 32 {
		return "", fmt.Errorf("版本文件内容异常")
	}
	return version, nil
}
