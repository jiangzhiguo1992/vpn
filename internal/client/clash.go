// Package client Clash 系订阅渲染(clash.yaml,mihomo 内核语法)。
//
// 设计决策:
//   - 面向 Clash Verge Rev / mihomo / OpenClash 等 Clash 系客户端:
//     节点 + PROXY 手动组 + AUTO url-test 自动组 + 国内直连分流规则
//   - 协议字段按 mihomo 语法(vless+reality 用 reality-opts 块;hy2 用
//     skip-cert-verify/obfs/obfs-password),版本要求见 docs/clients
//   - 分流规则用 GEOSITE/GEOIP 关键词(主流 GUI 客户端自带 geodata
//     并自动更新,开箱即用);裸 mihomo 内核用户需自行准备 geo 数据
//   - 渲染不走模板占位符(map 缺 key 静默输出 <no value> 的风险面),
//     全部值在 Go 侧经 JSON 转义(jq,与 YAML 双引号字符串转义集相同)
//     后直接拼装,结构由代码保证
//
// 职责边界:纯渲染;节点模型在 internal/conf;产物写盘在 cmd/vpn。
package client

import (
	"bytes"
	"fmt"
	"strings"

	"vpn/internal/conf"
)

// RenderClash 渲染 Clash 系订阅(clash.yaml 内容)。
//
// 调用说明:nodes 非空。返回:YAML 字节;任一节点渲染失败时返回错误。
func RenderClash(nodes []conf.Node) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(`# 由 vpn gen 生成(与服务器清单同源),适用于 Clash Verge Rev / mihomo 系客户端
mixed-port: 7890
allow-lan: false
mode: rule
log-level: info
ipv6: false

proxies:
`)
	for i := range nodes {
		n := &nodes[i]
		block, err := clashProxy(n)
		if err != nil {
			return nil, fmt.Errorf("client.RenderClash: 节点 %q: %w", n.Name, err)
		}
		buf.WriteString(block)
	}
	names := make([]string, len(nodes))
	for i, n := range nodes {
		names[i] = jq(n.Name)
	}
	quoted := strings.Join(names, "\n      - ")
	fmt.Fprintf(&buf, `proxy-groups:
  - name: PROXY
    type: select
    proxies:
      - AUTO
      - %s
  - name: AUTO
    type: url-test
    url: https://www.gstatic.com/generate_204
    interval: 300
    tolerance: 50
    proxies:
      - %s

rules:
  - GEOSITE,cn,DIRECT
  - GEOIP,CN,DIRECT
  - GEOIP,private,DIRECT,no-resolve
  - MATCH,PROXY
`, quoted, quoted)
	return buf.Bytes(), nil
}

// clashProxy 渲染单节点 proxy 块(YAML 文本,字段按 mihomo 语法)。
func clashProxy(n *conf.Node) (string, error) {
	head := fmt.Sprintf("  - name: %s\n    type: %s\n    server: %s\n    port: %d\n",
		jq(n.Name), clashType(n.Type), jq(n.Address), n.Port)
	switch n.Type {
	case conf.TypeVLESSReality:
		if n.UUID == "" || n.ServerName == "" || n.PublicKey == "" || n.ShortID == "" {
			return "", fmt.Errorf("vless 节点字段缺失(凭据未回填?)")
		}
		return head + fmt.Sprintf(`    uuid: %s
    network: tcp
    udp: true
    tls: true
    flow: xtls-rprx-vision
    servername: %s
    client-fingerprint: chrome
    reality-opts:
      public-key: %s
      short-id: %s
`, jq(n.UUID), jq(n.ServerName), jq(n.PublicKey), jq(n.ShortID)), nil
	case conf.TypeShadowsocks:
		if n.Password == "" || n.Method == "" {
			return "", fmt.Errorf("shadowsocks 节点字段缺失(凭据未回填?)")
		}
		return head + fmt.Sprintf(`    cipher: %s
    password: %s
    udp: true
`, jq(n.Method), jq(n.Password)), nil
	case conf.TypeHysteria2:
		if n.Password == "" {
			return "", fmt.Errorf("hysteria2 节点字段缺失(凭据未回填?)")
		}
		var rest string
		if n.Insecure {
			rest += "    skip-cert-verify: true\n"
		} else {
			rest += fmt.Sprintf("    sni: %s\n", jq(n.ServerName))
		}
		if n.Obfs != "" {
			rest += "    obfs: salamander\n"
			rest += fmt.Sprintf("    obfs-password: %s\n", jq(n.Obfs))
		}
		return head + fmt.Sprintf("    password: %s\n", jq(n.Password)) + rest, nil
	default:
		return "", fmt.Errorf("未知节点类型 %q", n.Type)
	}
}

// clashType 映射节点类型到 clash 的 type 值(与 conf 类型名一致)。
func clashType(t conf.NodeType) string {
	switch t {
	case conf.TypeVLESSReality:
		return "vless"
	case conf.TypeShadowsocks:
		return "ss"
	case conf.TypeHysteria2:
		return "hysteria2"
	}
	return string(t)
}
