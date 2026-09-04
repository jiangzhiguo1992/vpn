// Package client sing-box 官方客户端配置渲染(完整 JSON)。
//
// 设计决策:
//   - 输出形态 = 全节点完整配置:direct + 各节点 outbound + auto(urltest
//     自动选择)+ proxy(selector 手动选择,默认首节点),route.final=proxy
//   - 产物矩阵(1 通用版 + 3 桌面 tun 版 + 1 OpenWrt 网关盒子版,共 5 份
//     sing-box 产物):
//   - sing-box.json(通用):无 TUN,CLI/服务器/移动端(SFI/SFA 自带 TUN
//     开关)导入即用
//   - sing-box-sfm.json(SFM/macOS 桌面 GUI 全接管):inbounds 追加 tun
//     inbound + platform.http_proxy。SFM 无 tun 时只启动内核不接管系统
//     流量;带 tun 后系统流量全接管,仪表出现"系统HTTP代理"卡片
//     (实测 SFM 1.14.0 standalone)。tun 地址取官方示例默认
//     172.18.0.1/30(/30 仅覆盖 4 个地址,与常见局域网撞网段概率低);
//     撞网段启动报 "bind: can't assign requested address",按
//     docs/clients/sing-box.md 更换未占用私有段
//   - sing-box-sfw.json(SFW/Windows 桌面客户端):tun 仅作 platform.http_proxy
//     载体(auto_route=false)。Windows 实测(2026-09):tun 带 auto_route=true
//     时 1.14 Windows WFP 形成环路("只有上传没有下载",system/gvisor 均复现,
//     平台缺陷);auto_route=false + platform.http_proxy.enabled=true 时 SFW
//     连接即在用户上下文执行 WinInet 自动设置系统代理,浏览器即用零手动
//   - sing-box-sfl.json(SFL/Linux 桌面 GUI 全接管):同架构 tun 变体,
//     按官方推荐追加 auto_redirect(nftables,需 root;不支持时删该行);
//     Linux GUI 的 platform.http_proxy 可用性未经实测,未配置。注:
//     sfl 未经对应平台完整真机验证,参数依据官方文档
//     (sing-box.sagernet.org/clients/desktop)
//   - sing-box-openwrt.json(OpenWrt 网关盒子,裸 sing-box 全接管):
//     inbounds 仅 tun(双栈),auto_redirect 让 sing-box 自动接管 fw4
//     防火墙、strict_route=false 与既有路由策略共存;配合 cmd/vpn 的
//     vpn openwrt 一键部署(官方源 ipk + sing-box check 校验 + dnsmasq
//     DNS 让位)。未经 OpenWrt 真机验证(2026-09),清单见 docs/openwrt.md
//   - 分流形态 = 全局代理 + 国内直连分流 + 广告拦截(默认内置):dns/route
//     引用官方 remote rule-set(geosite-geolocation-cn / geosite-geolocation-!cn /
//     geoip-cn / geosite-category-ads-all,raw.githubusercontent 直链),国内
//     域名走直连 DNS、国内站点直连、广告域名直接 reject、其余走代理。
//     规则集下载出站双形态由模板 RULE_DL_MODE(v14/legacy)切换:
//     v14 形态(桌面/通用版,1.14+)顶层 http_clients + route.
//     default_http_client(detour 指向 proxy);legacy 形态(OpenWrt 版,
//     官方源 1.12/1.13——sing-box 配置 DisallowUnknownFields,官方源解析
//     不了 v14 形态直接 FATAL)无此二者、rule_set 每项 download_detour=
//     proxy 代替。下载经代理隧道(海外源更稳)后由 experimental.
//     cache_file 落盘缓存(缺缓存每次启动重新下载;首启下载失败会
//     FATAL,代理隧道可用时下载通常可达——代理出口在海外)。版本演进:
//     http_clients 是 1.14 默认出站下载弃用后的显式写法;download_detour
//     1.12 起支持、1.14 弃用但可用、计划 1.16 移除,届时官方源已跟上,
//     OpenWrt 整体切回 v14 形态(分流语义不变,下载出站始终走 proxy)
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

// RenderSingBoxSFW 渲染 SFW 专用版(Windows 桌面客户端):在通用版
// inbounds 上追加 tun inbound(仅作 platform.http_proxy 载体)。
//
// Windows 真机实测结论(2026-09,SFW + sing-box 1.14):
//   - tun 若带 auto_route=true:Windows 的 WFP 会把 sing-box 自身出站与
//     全部流量导向 TUN 形成环路("只有上传没有下载",直连/代理/DNS 全断;
//     stack system/gvisor 均复现,属 1.14 Windows 平台缺陷)
//   - 因此 Windows 形态:tun 仅承载 platform.http_proxy(auto_route:
//     false,不接管路由 → 无环),platform.http_proxy.enabled=true 让
//     SFW 连接时在用户上下文执行 WinInet 自动设置系统代理
//     (127.0.0.1:7890,实测生效)——走系统代理的应用(Chrome/Edge 等)
//     即用,无需手动设置;与 macOS 的 Dashboard 卡片是不同触发路径
//     (Windows 配置即生效)。覆盖边界:系统代理不影响自带代理设置或
//     直连 socket 的应用(Firefox 默认不跟随系统代理)
//
// 地址 172.18.0.1/30 说明同 RenderSingBoxSFM。
func RenderSingBoxSFW(nodes []conf.Node) ([]byte, error) {
	return renderSingBox(nodes, singBoxInboundsMixed+",\n"+singBoxInboundTunWindows)
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

// RenderSingBoxOpenWrt 渲染 OpenWrt 网关盒子专用版(裸 sing-box,TUN 全接管)。
//
// 用途:OpenWrt 官方源 sing-box(procd init + UCI 配置,config 'main' 含
// enabled/user 开关)部署到网关盒子后,以 root 运行并把局域网全部流量经
// TUN 接管。盒子是网关不是终端,形态与桌面变体(见 singBoxInboundTunOpenWrt
// 注释)不同:
//   - inbounds 仅 tun:无 mixed(网关侧无本地 127.0.0.1 代理消费方,不开
//     监听端口减攻击面)、无 platform.http_proxy(盒子无 GUI,该段无用)
//   - route 追加 auto_detect_interface=TUN 出站接口自动检测,防环路
//   - cache_file 显式落盘 /etc/sing-box/cache.db(/etc 非 tmpfs,重启后
//     规则集缓存仍在,免每次启动重新下载)
//   - 规则集下载用 legacy 形态(legacyRuleDL=true):无顶层 http_clients 与
//     route.default_http_client,rule_set 每项加 download_detour="proxy"。
//     原因:OpenWrt 官方源当前是 sing-box 1.12/1.13,配置解析
//     DisallowUnknownFields——桌面 v14 形态(http_clients/
//     default_http_client)对它们直接 FATAL parse error;download_detour
//     1.12 起支持、1.14 起弃用(deprecated but functional,优先级低于
//     http_clients)、计划 1.16 移除,届时官方源已跟上再整体切回 v14 形态
//     (分流语义不变,下载出站始终走 proxy detour,与桌面一致)
//
// 其余 dns/outbounds/route 分流与通用版同源,经 renderSingBoxFull 注入。
//
// 注:参数依据 sing-box 官方 TUN 文档与 OpenWrt 社区实践,未经 OpenWrt
// 真机验证(2026-09);真机验证清单见 docs/openwrt.md。
func RenderSingBoxOpenWrt(nodes []conf.Node) ([]byte, error) {
	return renderSingBoxFull(nodes, singBoxInboundTunOpenWrt,
		`"auto_detect_interface": true,`,
		",\n      \"path\": \"/etc/sing-box/cache.db\"", true)
}

// renderSingBox 渲染 sing-box 官方客户端完整配置(JSON 字节),inbounds
// 片段由调用方注入(mixed 纯内核版或 mixed+tun 全接管版)。纯委托
// renderSingBoxFull:route/cache 追加占位传空、legacyRuleDL=false(桌面/
// 通用版用 v14 形态,产物与改造前逐字节一致)。
//
// 调用说明:nodes 非空(至少一个节点,调用方保证)。返回:缩进 JSON
// 字节;任一节点渲染失败时返回错误。
func renderSingBox(nodes []conf.Node, inbounds string) ([]byte, error) {
	return renderSingBoxFull(nodes, inbounds, "", "", false)
}

// renderSingBoxFull 渲染 sing-box 官方客户端完整配置(JSON 字节),inbounds
// 片段与 route/cache 追加字段均由调用方注入。routeExtra/cacheExtra 是
// singBoxTemplate 占位(ROUTE_EXTRA/CACHE_EXTRA)的注入值,专供 OpenWrt
// 网关盒子变体补 auto_detect_interface 与 cache 落盘路径;其它变体传
// 空串(模板 if 条件不输出该行,产物与占位化改造前逐字节一致)。占位键
// 在 vals 里恒有值,防止 renderJSON 因模板占位缺失报错。
//
// legacyRuleDL 选择规则集下载出站形态(写入 vals 的 RULE_DL_MODE,模板
// 据其条件渲染):false → v14(顶层 http_clients + route.default_http_client,
// 桌面/通用版,1.14+);true → legacy(无此二者,rule_set 每项加
// download_detour=proxy,OpenWrt 版,兼容官方源 1.12/1.13)。OpenWrt 是
// 唯一 legacy 调用方,两形态原因与演进见 RenderSingBoxOpenWrt。
//
// 调用说明:nodes 非空(至少一个节点,调用方保证)。返回:缩进 JSON
// 字节;任一节点渲染失败或渲染结果非法时返回错误。
func renderSingBoxFull(nodes []conf.Node, inbounds, routeExtra, cacheExtra string, legacyRuleDL bool) ([]byte, error) {
	frags := make([]string, 0, len(nodes))
	names := make([]string, 0, len(nodes))
	for i := range nodes {
		n := &nodes[i]
		frag, err := singBoxOutbound(n)
		if err != nil {
			return nil, fmt.Errorf("client.renderSingBoxFull: 节点 %q: %w", n.Name, err)
		}
		frags = append(frags, frag)
		names = append(names, n.Name)
	}
	groupNames := make([]string, len(names))
	for i, n := range names {
		groupNames[i] = jq(n)
	}
	// 规则集下载出站形态(v14/legacy)写进 vals 由模板条件切换,v14 分支
	// 字面文本与仅桌面形态时代一致,桌面产物逐字节不变
	ruleDLMode := "v14"
	if legacyRuleDL {
		ruleDLMode = "legacy"
	}
	vals := map[string]string{
		"INBOUNDS":     inbounds,
		"OUTBOUNDS":    strings.Join(frags, ",\n"),
		"NODE_NAMES":   strings.Join(groupNames, ", "),
		"DEFAULT_NODE": jq(names[0]),
		"ROUTE_EXTRA":  routeExtra,
		"CACHE_EXTRA":  cacheExtra,
		"RULE_DL_MODE": ruleDLMode,
	}
	return renderJSON(singBoxTemplate, vals)
}

// singBoxInboundsMixed 是通用版 inbounds 片段(纯 mixed 本地入站)。
const singBoxInboundsMixed = `    {
      "type": "mixed",
      "tag": "mixed-in",
      "listen": "127.0.0.1",
      "listen_port": 7890
    }`

// singBoxInboundTunLinux 是 Linux(SFL)版追加的 tun 入站片段:基础形态 +
// auto_redirect(Linux 专用,nftables,官方推荐;需 root/CAP_NET_ADMIN)。
// 地址说明同 singBoxInboundTun(见 RenderSingBoxSFL)。Linux GUI 的系统
// 代理开关机制(platform.http_proxy 在 Linux 的可用性)未经实测,未配置。
const singBoxInboundTunLinux = `    {
      "type": "tun",
      "tag": "tun-in",
      "address": ["172.18.0.1/30"],
      "auto_route": true,
      "strict_route": true,
      "auto_redirect": true,
      "stack": "system"
    }`

// singBoxInboundTunWindows 是 Windows(SFW)版追加的 tun 入站片段:
// tun 仅作 platform.http_proxy 载体——auto_route/strict_route 必须
// false(Windows 1.14 上 auto_route 会形成 TUN 环路,见 RenderSingBoxSFW),
// platform.http_proxy.enabled=true 让 SFW 连接时自动设置系统代理(实测
// 生效,浏览器即用)。地址说明同 singBoxInboundTun(见 RenderSingBoxSFW)。
const singBoxInboundTunWindows = `    {
      "type": "tun",
      "tag": "tun-in",
      "address": ["172.18.0.1/30"],
      "auto_route": false,
      "strict_route": false,
      "stack": "system",
      "platform": {
        "http_proxy": {
          "enabled": true,
          "server": "127.0.0.1",
          "server_port": 7890
        }
      }
    }`

// singBoxInboundTun 是 SFM(macOS 桌面 GUI)版追加的 tun 入站片段:
// auto_route 全接管系统流量(macOS NE 下实测正常,无 Windows 的环路问题),
// platform.http_proxy 让 SFM 仪表出现"系统HTTP代理"卡片(GUI 开关,
// server_port 与 mixed 入站一致);stack "system" 实测 macOS GUI 可用。
// 地址 172.18.0.1/30 为官方示例默认,撞本机局域网时报 bind 错,按文档更换。
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

// singBoxInboundTunOpenWrt 是 OpenWrt 网关盒子版唯一的入站(tun)片段,
// 也是 sing-box-openwrt.json 与桌面变体的核心差异点。设计理由:
//   - 盒子=网关:auto_redirect=true 是官方 Linux 推荐(auto_route 的现代
//     替代),让 sing-box 启动时自动向 fw4 插入兼容的 nftables 重定向
//     规则,免手写 OpenWrt 防火墙段落
//   - strict_route=false:路由器本身已有 fw4 的路由/策略管理(auto_redirect
//     语义已足够严格),避免 sing-box 再注入 ip rule 与既有路由策略冲突;
//     与桌面端 strict_route=true 的"全权接管"定位不同
//   - 双栈地址(IPv4 172.18.0.1/30 + IPv6 fdfe:dcba:9876::1/126):路由
//     器侧同时拉起 IPv6 TUN,防客户端 IPv6 直连绕过代理泄漏(桌面单栈
//     变体无此需求——宿主自己的 v6 走系统路由即可)
//   - 无 mixed 入站、无 platform:盒子无本地代理消费方与 GUI(见
//     RenderSingBoxOpenWrt);与 SFL 桌面变体(仅 auto_route+auto_redirect
//     单栈)的差异即上述双栈/无 mixed/无 platform/cache 落盘
const singBoxInboundTunOpenWrt = `    {
      "type": "tun",
      "tag": "tun-in",
      "address": ["172.18.0.1/30", "fdfe:dcba:9876::1/126"],
      "auto_route": true,
      "auto_redirect": true,
      "strict_route": false,
      "stack": "system"
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
// {{.INBOUNDS}},route/cache 追加字段经 ROUTE_EXTRA/CACHE_EXTRA)。
//
// 规则集下载出站双形态由 RULE_DL_MODE(v14/legacy,见 renderSingBoxFull)
// 条件切换,v14 分支字面文本与改造前单形态模板逐字节一致(桌面/通用版
// 产物零影响):v14 → route 内 default_http_client + 顶层 http_clients;
// legacy → 无此二者(1.12/1.13 官方源 DisallowUnknownFields 直接 FATAL),
// rule_set 每项补 download_detour=proxy。
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
    "default_domain_resolver": "dns-direct",{{if .ROUTE_EXTRA}}
    {{.ROUTE_EXTRA}}{{end}}{{if eq .RULE_DL_MODE "v14"}}
    "default_http_client": "rule-set-download",{{end}}
    "rule_set": [
      {
        "type": "remote",
        "tag": "geosite-geolocation-cn",
        "format": "binary",
        "url": "https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-geolocation-cn.srs"{{if eq .RULE_DL_MODE "legacy"}},
        "download_detour": "proxy"{{end}}
      },
      {
        "type": "remote",
        "tag": "geosite-geolocation-!cn",
        "format": "binary",
        "url": "https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-geolocation-!cn.srs"{{if eq .RULE_DL_MODE "legacy"}},
        "download_detour": "proxy"{{end}}
      },
      {
        "type": "remote",
        "tag": "geoip-cn",
        "format": "binary",
        "url": "https://raw.githubusercontent.com/SagerNet/sing-geoip/rule-set/geoip-cn.srs"{{if eq .RULE_DL_MODE "legacy"}},
        "download_detour": "proxy"{{end}}
      },
      {
        "type": "remote",
        "tag": "geosite-category-ads-all",
        "format": "binary",
        "url": "https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-category-ads-all.srs"{{if eq .RULE_DL_MODE "legacy"}},
        "download_detour": "proxy"{{end}}
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
  },{{if eq .RULE_DL_MODE "v14"}}
  "http_clients": [
    {
      "tag": "rule-set-download",
      "engine": "go",
      "detour": "proxy"
    }
  ],{{end}}
  "experimental": {
    "cache_file": {
      "enabled": true{{.CACHE_EXTRA}}
    }
  }
}
`
