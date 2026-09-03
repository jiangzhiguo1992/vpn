// Package client sing-box 官方客户端配置渲染(完整 JSON)。
//
// 设计决策:
//   - 输出形态 = 全节点完整配置:direct + 各节点 outbound + auto(urltest
//     自动选择)+ proxy(selector 手动选择,默认首节点),route.final=proxy
//   - 分流形态 = 全局代理 + 内网直连(ip_is_private),不依赖任何外置
//     .srs/geo 规则文件,官方客户端(SFI/SFA/CLI)导入即用;需要国内
//     直连分流的用户按 docs/clients/sing-box.md 追加规则
//   - dns 段带国内 UDP 直连与国外 DoH(经 proxy detour)双服务器:
//     final 走 DoH 防泄漏,域名解析与连接同出口
//   - 与分享链接同源:节点 outbound 字段 = 链接参数 = 清单凭据,
//     任何渲染之间不可能漂移
//
// 职责边界:纯渲染;节点模型在 internal/conf;产物写盘在 cmd/vpn。
package client

import (
	"fmt"
	"strings"

	"vpn/internal/conf"
)

// RenderSingBox 渲染 sing-box 官方客户端完整配置(JSON 字节)。
//
// 调用说明:nodes 非空(至少一个节点,调用方保证)。返回:缩进 JSON
// 字节;任一节点渲染失败时返回错误。
func RenderSingBox(nodes []conf.Node) ([]byte, error) {
	frags := make([]string, 0, len(nodes))
	names := make([]string, 0, len(nodes))
	for i := range nodes {
		n := &nodes[i]
		frag, err := singBoxOutbound(n)
		if err != nil {
			return nil, fmt.Errorf("client.RenderSingBox: 节点 %q: %w", n.Name, err)
		}
		frags = append(frags, frag)
		names = append(names, n.Name)
	}
	groupNames := make([]string, len(names))
	for i, n := range names {
		groupNames[i] = jq(n)
	}
	vals := map[string]string{
		"OUTBOUNDS":    strings.Join(frags, ",\n"),
		"NODE_NAMES":   strings.Join(groupNames, ", "),
		"DEFAULT_NODE": jq(names[0]),
	}
	return renderJSON(singBoxTemplate, vals)
}

// singBoxOutbound 渲染单节点 outbound 片段(JSON 对象文本,4 空格元素
// 缩进,由模板 outbounds 数组承接)。
func singBoxOutbound(n *conf.Node) (string, error) {
	vals := map[string]string{
		"TAG":    jq(n.Name),
		"SERVER": jq(n.Address),
		"PORT":   fmt.Sprint(n.Port),
	}
	var tpl string
	switch n.Type {
	case conf.TypeVLESSReality:
		if n.UUID == "" || n.ServerName == "" || n.PublicKey == "" || n.ShortID == "" {
			return "", fmt.Errorf("vless 节点字段缺失(凭据未回填?)")
		}
		vals["UUID"] = jq(n.UUID)
		vals["SNI"] = jq(n.ServerName)
		vals["PUBLIC_KEY"] = jq(n.PublicKey)
		vals["SHORT_ID"] = jq(n.ShortID)
		tpl = `    {
      "type": "vless",
      "tag": {{.TAG}},
      "server": {{.SERVER}},
      "server_port": {{.PORT}},
      "uuid": {{.UUID}},
      "flow": "xtls-rprx-vision",
      "tls": {
        "enabled": true,
        "server_name": {{.SNI}},
        "utls": {
          "enabled": true,
          "fingerprint": "chrome"
        },
        "reality": {
          "enabled": true,
          "public_key": {{.PUBLIC_KEY}},
          "short_id": {{.SHORT_ID}}
        }
      }
    }`
	case conf.TypeHysteria2:
		if n.Password == "" {
			return "", fmt.Errorf("hysteria2 节点字段缺失(凭据未回填?)")
		}
		vals["PASSWORD"] = jq(n.Password)
		// TLS 段内嵌 server_name 引用不能经模板嵌套执行(map 值不做模板
		// 处理),直接在 Go 侧拼好成品文本
		tls := `      "tls": {
        "enabled": true,
        "insecure": true
      }`
		if !n.Insecure {
			tls = `      "tls": {
        "enabled": true,
        "server_name": ` + jq(n.ServerName) + `,
        "insecure": false
      }`
		}
		vals["TLS"] = tls
		obfs := ""
		if n.Obfs != "" {
			vals["OBFS_PASSWORD"] = jq(n.Obfs)
			obfs = ",\n" + `      "obfs": {
        "type": "salamander",
        "password": {{.OBFS_PASSWORD}}
      }`
		}
		tpl = `    {
      "type": "hysteria2",
      "tag": {{.TAG}},
      "server": {{.SERVER}},
      "server_port": {{.PORT}},
      "password": {{.PASSWORD}},
` + "{{.TLS}}" + obfs + `
    }`
	default:
		return "", fmt.Errorf("未知节点类型 %q", n.Type)
	}
	out, err := renderFragment(tpl, vals)
	if err != nil {
		return "", err
	}
	return out, nil
}

// singBoxTemplate 是客户端完整配置骨架(节点 outbound 片段拼入
// {{.OUTBOUNDS}},代理组引用 {{.NODE_NAMES}})。
const singBoxTemplate = `{
  "log": {
    "level": "info"
  },
  "dns": {
    "servers": [
      {
        "type": "udp",
        "tag": "dns-direct",
        "server": "223.5.5.5"
      },
      {
        "type": "https",
        "tag": "dns-proxy",
        "detour": "proxy",
        "server": "1.1.1.1",
        "server_port": 443,
        "path": "/dns-query"
      }
    ],
    "final": "dns-proxy",
    "strategy": "ipv4_only"
  },
  "inbounds": [
    {
      "type": "mixed",
      "tag": "mixed-in",
      "listen": "127.0.0.1",
      "listen_port": 7890
    }
  ],
  "outbounds": [
    {
      "type": "direct",
      "tag": "direct"
    },
{{.OUTBOUNDS}},
    {
      "type": "urltest",
      "tag": "auto",
      "outbounds": [{{.NODE_NAMES}}]
    },
    {
      "type": "selector",
      "tag": "proxy",
      "outbounds": [{{.NODE_NAMES}}],
      "default": {{.DEFAULT_NODE}}
    }
  ],
  "route": {
    "rules": [
      {
        "action": "sniff"
      },
      {
        "protocol": "dns",
        "action": "hijack-dns"
      },
      {
        "ip_is_private": true,
        "outbound": "direct"
      }
    ],
    "final": "proxy"
  }
}
`
