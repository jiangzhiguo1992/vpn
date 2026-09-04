// Package client sing-box 官方客户端配置渲染(完整 JSON)。
//
// 设计决策:
//   - 输出形态 = 全节点完整配置:direct + 各节点 outbound + auto(urltest
//     自动选择)+ proxy(selector 手动选择,默认首节点),route.final=proxy
//   - 产物矩阵(1 通用版 + 3 桌面 tun 版,共 4 份 sing-box 产物):
//   - sing-box.json(通用):无 TUN,CLI/服务器/移动端(SFI/SFA 自带 TUN
//     开关)导入即用
//   - sing-box-sfm.json(SFM/macOS 桌面 GUI 全接管):inbounds 追加 tun
//     inbound + platform.http_proxy。SFM 无 tun 时只启动内核不接管系统
//     流量;带 tun 后系统流量全接管,仪表出现"系统HTTP代理"卡片
//     (实测 SFM 1.14.0 standalone)。tun 地址取官方示例默认
//     172.18.0.1/30(/30 仅覆盖 4 个地址,与常见局域网撞网段概率低);
//     撞网段启动报 "bind: can't assign requested address",按
//     docs/clients/sing-box.md 更换未占用私有段
//   - sing-box-sfw.json / sing-box-sfl.json(SFW/Windows、SFL/Linux
//     桌面 GUI 全接管):Windows/Linux 官方桌面客户端与 SFM 同架构
//     (纯内核,不自动注入 TUN),按官方推荐生成 tun 变体(Linux 追加
//     auto_redirect);未经 Windows/Linux 真机实测,参数依据官方文档
//     (sing-box.sagernet.org/clients/desktop)
//   - 分流形态 = 全局代理 + 国内直连分流 + 广告拦截(默认内置):dns/route
//     引用官方 remote rule-set(geosite-geolocation-cn / geosite-geolocation-!cn /
//     geoip-cn / geosite-category-ads-all,raw.githubusercontent 直链),国内
//     域名走直连 DNS、国内站点直连、广告域名直接 reject、其余走代理。
//     规则集经 http_clients(detour 指向 proxy)下载,
//     经代理隧道获取海外源更稳,并由 experimental.cache_file 落盘缓存
//     (缺缓存每次启动重新下载;首启下载失败会 FATAL,代理隧道可用时
//     下载通常可达——代理出口在海外)。注:http_clients 显式下载出站是
//     1.14+ 写法(隐式默认出站下载 1.14 弃用、1.16 移除),规避迁移窗口
//   - dns 段带国内 UDP 直连与国外 DoH(经 proxy detour)双服务器:
//     final 走 DoH 防泄漏,域名解析与连接同出口
//   - route.default_domain_resolver=dns-direct 满足 sing-box 对出站域名
//     解析器的要求(1.14 起缺失即拒启,1.12/1.13 缺省仅告警);指向直连
//     DNS,不依赖代理隧道先行可用,与 dns.final 走 DoH 的防泄漏分工
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

// renderSingBox 渲染 sing-box 官方客户端完整配置(JSON 字节),inbounds
// 片段由调用方注入(mixed 纯内核版或 mixed+tun 全接管版)。
//
// 调用说明:nodes 非空(至少一个节点,调用方保证)。返回:缩进 JSON
// 字节;任一节点渲染失败时返回错误。
func renderSingBox(nodes []conf.Node, inbounds string) ([]byte, error) {
	frags := make([]string, 0, len(nodes))
	names := make([]string, 0, len(nodes))
	for i := range nodes {
		n := &nodes[i]
		frag, err := singBoxOutbound(n)
		if err != nil {
			return nil, fmt.Errorf("client.renderSingBox: 节点 %q: %w", n.Name, err)
		}
		frags = append(frags, frag)
		names = append(names, n.Name)
	}
	groupNames := make([]string, len(names))
	for i, n := range names {
		groupNames[i] = jq(n)
	}
	vals := map[string]string{
		"INBOUNDS":     inbounds,
		"OUTBOUNDS":    strings.Join(frags, ",\n"),
		"NODE_NAMES":   strings.Join(groupNames, ", "),
		"DEFAULT_NODE": jq(names[0]),
	}
	return renderJSON(singBoxTemplate, vals)
}

// RenderSingBox 渲染通用版客户端完整配置(无 TUN):CLI 跑、服务器网关、
// 移动端官方 app(SFI/SFA 自带 TUN 开关)等场景导入即用,不要求特权。
func RenderSingBox(nodes []conf.Node) ([]byte, error) {
	return renderSingBox(nodes, singBoxInboundsMixed)
}

// RenderSingBoxSFM 渲染 SFM 专用版(macOS 桌面 GUI 全接管):在通用版
// inbounds 上追加 tun inbound + platform.http_proxy。
//
// 用途:macOS 官方客户端 SFM 只启动内核时不会接管系统流量(浏览器仍直连);
// 带 tun 后系统流量全接管(auto_route),且仪表出现"系统HTTP代理"卡片,
// 开关由 SFM 管理系统代理(实测 SFM 1.14.0 standalone,2026-09)。
// tun 地址取 sing-box 官方示例默认 172.18.0.1/30:/30 仅覆盖 4 个地址,
// 与常见局域网(172.16/172.17/172.19 等 /16-/19 段)撞网段概率低;若
// 启动报 "bind: can't assign requested address"(本机局域网恰覆盖该
// 地址),按 docs/clients/sing-box.md FAQ 更换其它未占用私有段。
func RenderSingBoxSFM(nodes []conf.Node) ([]byte, error) {
	return renderSingBox(nodes, singBoxInboundsMixed+",\n"+singBoxInboundTun)
}

// RenderSingBoxSFW 渲染 SFW 专用版(Windows 桌面 GUI 全接管):在通用版
// inbounds 上追加 tun inbound。
//
// 用途:Windows 官方桌面客户端(SFW,sing-box for Desktop)与 SFM 同架构
// (纯内核,不自动注入 TUN);带 tun 后系统流量全接管(auto_route 经 WFP,
// strict_route 防泄漏)。地址与 macOS 版同为 172.18.0.1/30(官方示例默认,
// 撞本机局域网时报 bind 错,按文档更换)。注:按官方推荐生成,未经
// Windows 真机实测。
func RenderSingBoxSFW(nodes []conf.Node) ([]byte, error) {
	return renderSingBox(nodes, singBoxInboundsMixed+",\n"+singBoxInboundTunBase)
}

// RenderSingBoxSFL 渲染 SFL 专用版(Linux 桌面/服务 GUI 全接管):在通用版
// inbounds 上追加 tun inbound。
//
// 用途:Linux 官方桌面客户端(SFL,sing-box for Desktop)与 SFM 同架构
// (纯内核,不自动注入 TUN);带 tun 后系统流量全接管。按官方推荐追加
// auto_redirect(nftables 重定向,需 root/CAP_NET_ADMIN;内核不支持时
// 删掉该行即可,不影响 auto_route)。地址同 172.18.0.1/30(见 SFW)。
// 注:按官方推荐生成,未经 Linux 真机实测。
func RenderSingBoxSFL(nodes []conf.Node) ([]byte, error) {
	return renderSingBox(nodes, singBoxInboundsMixed+",\n"+singBoxInboundTunLinux)
}

// singBoxInboundsMixed 是通用版 inbounds 片段(纯 mixed 本地入站)。
const singBoxInboundsMixed = `    {
      "type": "mixed",
      "tag": "mixed-in",
      "listen": "127.0.0.1",
      "listen_port": 7890
    }`

// singBoxInboundTunBase 是 Windows(SFW)版追加的 tun 入站片段:基础形态
// (auto_route/strict_route/stack),无平台专属字段。地址 172.18.0.1/30
// 为官方示例默认,撞本机局域网时报 bind 错,按文档更换(见 RenderSingBoxSFW)。
const singBoxInboundTunBase = `    {
      "type": "tun",
      "tag": "tun-in",
      "address": ["172.18.0.1/30"],
      "auto_route": true,
      "strict_route": true,
      "stack": "system"
    }`

// singBoxInboundTunLinux 是 Linux(SFL)版追加的 tun 入站片段:基础形态 +
// auto_redirect(Linux 专用,nftables,官方推荐;需 root/CAP_NET_ADMIN)。
// 地址说明同 singBoxInboundTunBase(见 RenderSingBoxSFL)。
const singBoxInboundTunLinux = `    {
      "type": "tun",
      "tag": "tun-in",
      "address": ["172.18.0.1/30"],
      "auto_route": true,
      "strict_route": true,
      "auto_redirect": true,
      "stack": "system"
    }`

// singBoxInboundTun 是 SFM 专用版追加的 tun 入站片段。platform.http_proxy
// 让 SFM 仪表渲染"系统HTTP代理"卡片(server_port 与 mixed 入站一致);
// stack "system" 实测 macOS GUI(NetworkExtension)可用;地址 172.18.0.1/30
// 为官方示例默认,撞本机局域网时报 bind 错,按文档更换(见 RenderSingBoxSFM)。
const singBoxInboundTun = `    {
      "type": "tun",
      "tag": "tun-in",
      "address": ["172.18.0.1/30"],
      "auto_route": true,
      "strict_route": true,
      "stack": "system",
      "platform": {
        "http_proxy": {
          "enabled": true,
          "server": "127.0.0.1",
          "server_port": 7890
        }
      }
    }`

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
// {{.OUTBOUNDS}},代理组引用 {{.NODE_NAMES}},入站片段拼入
// {{.INBOUNDS}})。
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
    "strategy": "ipv4_only",
    "rules": [
      {
        "rule_set": "geosite-geolocation-cn",
        "server": "dns-direct"
      }
    ]
  },
  "inbounds": [
{{.INBOUNDS}}
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
    "default_domain_resolver": "dns-direct",
    "default_http_client": "rule-set-download",
    "rule_set": [
      {
        "type": "remote",
        "tag": "geosite-geolocation-cn",
        "format": "binary",
        "url": "https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-geolocation-cn.srs"
      },
      {
        "type": "remote",
        "tag": "geosite-geolocation-!cn",
        "format": "binary",
        "url": "https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-geolocation-!cn.srs"
      },
      {
        "type": "remote",
        "tag": "geoip-cn",
        "format": "binary",
        "url": "https://raw.githubusercontent.com/SagerNet/sing-geoip/rule-set/geoip-cn.srs"
      },
      {
        "type": "remote",
        "tag": "geosite-category-ads-all",
        "format": "binary",
        "url": "https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-category-ads-all.srs"
      }
    ],
    "rules": [
      {
        "action": "sniff"
      },
      {
        "protocol": "dns",
        "action": "hijack-dns"
      },
      {
        "rule_set": "geosite-category-ads-all",
        "action": "reject"
      },
      {
        "ip_is_private": true,
        "outbound": "direct"
      },
      {
        "rule_set": "geosite-geolocation-cn",
        "action": "route",
        "outbound": "direct"
      },
      {
        "type": "logical",
        "mode": "and",
        "rules": [
          {
            "rule_set": "geoip-cn"
          },
          {
            "rule_set": "geosite-geolocation-!cn",
            "invert": true
          }
        ],
        "action": "route",
        "outbound": "direct"
      }
    ],
    "final": "proxy"
  },
  "http_clients": [
    {
      "tag": "rule-set-download",
      "engine": "go",
      "detour": "proxy"
    }
  ],
  "experimental": {
    "cache_file": {
      "enabled": true
    }
  }
}
`
