package module

import (
	"crypto/tls"
	"errors"
	"fmt"
	ref "github.com/distribution/reference"
	"github.com/onlyLTY/dockerCopilot/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
	"io"
	"net"
	"net/http"
	url2 "net/url"
	"strings"
	"sync"
	"time"
)

// ImageCheckList 检查更新处理后的镜像列表
type ImageCheckList struct {
	NeedUpdate bool
	Digest     string // 远端 digest，用于"发现新版本"通知去重
	Image      string // 仓库:tag，便于通知里直接展示
}

// ImageUpdateData 镜像更新检查结果。
// 每轮检查先算出一张新表再整体替换：既避免 HTTP 请求与定时任务并发读写同一张 map
// （Go 对 map 并发读写是 fatal error，不可 recover），也让本轮检查失败的条目自动失效，
// 不会残留在面板上变成"一直有更新"。
type ImageUpdateData struct {
	mu   sync.RWMutex
	data map[string]ImageCheckList
}

const ContentDigestHeader = "Docker-Content-Digest"

// 自身镜像名（大小写不敏感），检查时跳过，避免把自己标成"有更新"
const selfImageName = "dockercopilot"

func NewImageCheck() *ImageUpdateData {
	return &ImageUpdateData{
		data: map[string]ImageCheckList{},
	}
}

// Get 并发安全地读取某个镜像的检查结果
func (i *ImageUpdateData) Get(imageID string) (ImageCheckList, bool) {
	i.mu.RLock()
	v, ok := i.data[imageID]
	i.mu.RUnlock()
	return v, ok
}

// Snapshot 返回当前检查结果的副本，供通知等只读场景使用
func (i *ImageUpdateData) Snapshot() map[string]ImageCheckList {
	i.mu.RLock()
	defer i.mu.RUnlock()
	out := make(map[string]ImageCheckList, len(i.data))
	for k, v := range i.data {
		out[k] = v
	}
	return out
}

func (i *ImageUpdateData) replace(next map[string]ImageCheckList) {
	i.mu.Lock()
	i.data = next
	i.mu.Unlock()
}

func (i *ImageUpdateData) CheckUpdate(imageList []types.Image) {
	next := make(map[string]ImageCheckList, len(imageList))
	for _, image := range imageList {
		if strings.Contains(strings.ToLower(image.ImageName), selfImageName) {
			continue
		}
		// 忽略名单命中的镜像直接不查：省一次网络往返，也让它彻底不出现在面板上
		if IgnoredImage(image.ImageName, image.ImageTag) {
			continue
		}
		if result, ok := i.checkSingleImage(image); ok {
			next[image.ID] = result
		}
	}
	i.replace(next)
}

// checkSingleImage 第二个返回值表示本轮检查是否得到结论；
// false 时调用方不写入结果，上一轮的旧状态随之失效。
func (i *ImageUpdateData) checkSingleImage(image types.Image) (ImageCheckList, bool) {
	if len(image.RepoDigests) == 0 {
		logx.Error("未在本地获取到repoDigest" + image.ImageName + ":" + image.ImageTag)
		return ImageCheckList{}, false
	}
	token, err := GetToken(image, "")
	if err != nil {
		logx.Error("获取token失败或者无需获取token，继续尝试检查" + err.Error())
	}
	digestURL, err := BuildManifestURL(image)
	if err != nil {
		logx.Error("获取digestURL失败" + err.Error())
		return ImageCheckList{}, false
	}
	remoteDigest, err := GetDigest(digestURL, token)
	if err != nil {
		logx.Error("获取digest失败" + err.Error())
		return ImageCheckList{}, false
	}
	// 如果远程 digest 为空，无法比较，本轮不下结论
	if remoteDigest == "" {
		logx.Error("远程Digest为空，无法比较" + image.ImageName + ":" + image.ImageTag)
		return ImageCheckList{}, false
	}

	// 修复：只要有一个本地 RepoDigests 与远程匹配，就认为不需要更新
	// 原逻辑中多个 RepoDigests 时，后续结果会覆盖之前的判断，导致误判
	needUpdate := true
	for _, localRepoDigest := range image.RepoDigests {
		parts := strings.Split(localRepoDigest, "@")
		if len(parts) < 2 {
			logx.Error("RepoDigest格式异常" + localRepoDigest)
			continue
		}
		localDigest := parts[1]
		if localDigest == "" {
			logx.Error("本地Digest为空" + image.ImageName + ":" + image.ImageTag)
			continue
		}
		if localDigest == remoteDigest {
			logx.Info(image.ImageName + ":" + image.ImageTag + " not need update")
			needUpdate = false
			break
		}
	}
	if needUpdate {
		logx.Info(image.ImageName + ":" + image.ImageTag + " need update")
		logx.Infof("localRepoDigests: %v, remoteDigest: %s", image.RepoDigests, remoteDigest)
	}
	return ImageCheckList{
		NeedUpdate: needUpdate,
		Digest:     remoteDigest,
		Image:      image.ImageName + ":" + image.ImageTag,
	}, true
}

func BuildManifestURL(image types.Image) (string, error) {
	normalizedRef, err := ref.ParseDockerRef(image.ImageName + ":" + image.ImageTag)
	if err != nil {
		return "", err
	}
	normalizedTaggedRef, isTagged := normalizedRef.(ref.NamedTagged)
	if !isTagged {
		return "", errors.New("镜像无tag" + normalizedRef.String())
	}

	host, ErrGetRegistryAddress := GetRegistryAddress(normalizedTaggedRef.Name())
	img, tag := ref.Path(normalizedTaggedRef), normalizedTaggedRef.Tag()

	if ErrGetRegistryAddress != nil {
		return "", ErrGetRegistryAddress
	}

	url := url2.URL{
		Scheme: "https",
		Host:   host,
		Path:   fmt.Sprintf("/v2/%s/manifests/%s", img, tag),
	}
	return url.String(), nil
}

func GetDigest(url string, token string) (string, error) {
	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}

	req, _ := http.NewRequest("HEAD", url, nil)

	if token != "" {
		req.Header.Add("Authorization", token)
	}
	// 优先请求 manifest list / OCI image index，以匹配本地 RepoDigests 记录的 digest
	// 本地 RepoDigests 记录的是 manifest list 的 digest（多架构镜像）
	// 如果优先请求单个 manifest（manifest.v2+json），registry 可能返回某个架构的具体 manifest，
	// 其 digest 与 manifest list 的 digest 不同，导致误判需要更新
	req.Header.Add("Accept", "application/vnd.docker.distribution.manifest.list.v2+json")
	req.Header.Add("Accept", "application/vnd.oci.image.index.v1+json")
	req.Header.Add("Accept", "application/vnd.docker.distribution.manifest.v2+json")
	req.Header.Add("Accept", "application/vnd.oci.image.manifest.v1+json")
	req.Header.Add("Accept", "application/vnd.docker.distribution.manifest.v1+json")

	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			logx.Error("GetDigest关闭body失败" + err.Error())
		}
	}(res.Body)

	if res.StatusCode != 200 {
		wwwAuthHeader := res.Header.Get("www-authenticate")
		if wwwAuthHeader == "" {
			wwwAuthHeader = "not present"
		}
		return "", fmt.Errorf("registry responded to head request with %q, auth: %q", res.Status, wwwAuthHeader)
	}
	return res.Header.Get(ContentDigestHeader), nil
}
