package utiles

import (
	"errors"

	"github.com/onlyLTY/dockerCopilot/internal/svc"
)

// UpdateProgram 容器部署下不再支持「程序内自更新」。
//
// v2.1.3 的实现有三个硬伤：
//  1. release 包内的二进制路径是 dist/linux/<arch>/dockerCopilot-new，
//     而 start.sh 只认工作目录下的 ./dockerCopilot-new —— 解压后永远不会生效，
//     界面却报「更新成功」（这是「更新了还是旧版本」的来源之一）；
//  2. 解压时 target := dest + "/" + header.Name，未清洗路径，存在 tar 路径穿越；
//  3. 下载走 http.Get，没有任何完整性/签名校验，githubProxy 又允许指向任意主机。
//
// 容器部署的正确升级路径是拉新镜像后重建容器，因此这里直接返回可执行的指引，
// 而不是继续维护一条注定不生效的下载链路。
func UpdateProgram(ctx *svc.ServiceContext) error {
	return errors.New("当前为容器部署，不支持程序内自更新：请执行 docker compose pull && docker compose up -d（或拉取新镜像后重建容器）")
}
