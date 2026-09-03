// Package client 分享链接聚合与通用订阅(links.txt / sub.txt)。
//
// 设计决策:
//   - links.txt 每行一个分享链接(节点顺序与清单一致,先 vless 后 h2);
//     sub.txt 是 links 的标准 base64(机场通用订阅格式,支持
//     订阅的客户端可用;也可作为自托管订阅源文件)
//   - 产物不含任何格式包装,纯文本,分发时由用户自行拷贝/传输
//
// 职责边界:纯文本组装;单链接生成在 link.go;产物写盘在 cmd/vpn。
package client

import (
	"encoding/base64"
	"fmt"
	"strings"

	"vpn/internal/conf"
)

// RenderLinks 渲染全节点分享链接文本(每行一个,末尾换行)。
//
// 调用说明:nodes 非空;未知节点类型返回错误(正常路径不可能出现,
// 节点全部来自清单三通道)。
func RenderLinks(nodes []conf.Node) (string, error) {
	var sb strings.Builder
	for _, n := range nodes {
		link := ShareLink(n)
		if link == "" {
			return "", fmt.Errorf("client.RenderLinks: 节点 %q 生成链接失败(未知类型 %q)", n.Name, n.Type)
		}
		sb.WriteString(link)
		sb.WriteByte('\n')
	}
	return sb.String(), nil
}

// RenderSub 渲染通用订阅文本:links 文本经标准 base64 编码(机场格式)。
//
// 调用说明:同 RenderLinks(内部复用其校验)。
func RenderSub(nodes []conf.Node) (string, error) {
	links, err := RenderLinks(nodes)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString([]byte(links)), nil
}
