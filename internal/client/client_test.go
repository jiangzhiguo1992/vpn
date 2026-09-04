// Package client 客户端产物测试:分享链接与各配置渲染。
//
// 重点:链接/配置字段与输入节点一致(防凭据漂移)与跨产物一致性。
package client

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"vpn/internal/conf"
)

// fixtureNodes 返回三通道节点基线(vless/ss/h2 自签)。
func fixtureNodes() []conf.Node {
	return []conf.Node{
		{Name: "hk-01-vless", Type: conf.TypeVLESSReality,
			Address: "hk.example.com", Port: 443, UUID: "8a2f3dfa-ddf3-471a-ab3a-d4110d631d92",
			ServerName: "www.apple.com", PublicKey: "zRAPkZIJ-p7lWdzOi4i4O8JUas5vvd3TzMmYUQm1i2w",
			ShortID: "cafe2554decd2a45"},
		{Name: "hk-01-h2", Type: conf.TypeHysteria2,
			Address: "hk.example.com", Port: 8443, Password: "eabcc87e53095032770fdec57012235c8ee1f1ea916b0c5593a516628cf3dac3",
			Insecure: true},
	}
}

// ===== ShareLink =====

// TestShareLink_vless 解析 vless:// 链接断言全参数。
func TestShareLink_vless(t *testing.T) {
	n := fixtureNodes()[0]
	link := ShareLink(n)
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("解析失败: %v (%s)", err, link)
	}
	if u.Scheme != "vless" || u.User.Username() != n.UUID {
		t.Fatalf("scheme/user 错误: %s", link)
	}
	if u.Host != "hk.example.com:443" {
		t.Fatalf("host 错误: %s", u.Host)
	}
	q := u.Query()
	for k, want := range map[string]string{
		"type": "tcp", "security": "reality", "encryption": "none",
		"fp": "chrome", "flow": "xtls-rprx-vision",
		"sni": n.ServerName, "pbk": n.PublicKey, "sid": n.ShortID,
	} {
		if q.Get(k) != want {
			t.Fatalf("query %s = %q, want %q", k, q.Get(k), want)
		}
	}
	if u.Fragment != "hk-01-vless" {
		t.Fatalf("fragment = %q", u.Fragment)
	}
}

// TestShareLink_h2 解析 hysteria2:// 链接断言 insecure/obfs 参数。
func TestShareLink_h2(t *testing.T) {
	n := fixtureNodes()[1]
	link := ShareLink(n)
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("解析失败: %v (%s)", err, link)
	}
	if u.Scheme != "hysteria2" || u.User.Username() != n.Password {
		t.Fatalf("scheme/auth 错误: %s", link)
	}
	if q := u.Query(); q.Get("insecure") != "1" {
		t.Fatalf("insecure = %q", q.Get("insecure"))
	}
	// 混淆节点:obfs 与 obfs-password 成对出现
	n2 := n
	n2.Obfs = "obfs-secret"
	link2 := ShareLink(n2)
	u2, _ := url.Parse(link2)
	if u2.Query().Get("obfs") != "salamander" || u2.Query().Get("obfs-password") != "obfs-secret" {
		t.Fatalf("obfs 参数错误: %s", link2)
	}
	// 受信证书节点:insecure=0 + sni
	n3 := n
	n3.Insecure = false
	n3.ServerName = "vpn.example.com"
	u3, _ := url.Parse(ShareLink(n3))
	if u3.Query().Get("insecure") != "0" || u3.Query().Get("sni") != "vpn.example.com" {
		t.Fatalf("受信证书参数错误: %s", ShareLink(n3))
	}
}

// TestShareLink_IPv6 验证 IPv6 地址链接加方括号。
func TestShareLink_IPv6(t *testing.T) {
	n := fixtureNodes()[0]
	n.Address = "2001:db8::1"
	u, err := url.Parse(ShareLink(n))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if u.Host != "[2001:db8::1]:443" {
		t.Fatalf("IPv6 host = %q", u.Host)
	}
}

// ===== RenderLinks / RenderSub =====

// TestRenderLinksSub_一致性 sub 解码后与 links 逐行一致。
func TestRenderLinksSub_一致性(t *testing.T) {
	nodes := fixtureNodes()
	links, err := RenderLinks(nodes)
	if err != nil {
		t.Fatalf("RenderLinks: %v", err)
	}
	want := strings.Split(strings.TrimRight(links, "\n"), "\n")
	if len(want) != 2 {
		t.Fatalf("链接行数 = %d", len(want))
	}
	sub, err := RenderSub(nodes)
	if err != nil {
		t.Fatalf("RenderSub: %v", err)
	}
	dec, err := base64.StdEncoding.DecodeString(sub)
	if err != nil {
		t.Fatalf("sub 解码失败: %v", err)
	}
	got := strings.Split(strings.TrimRight(string(dec), "\n"), "\n")
	if len(got) != len(want) {
		t.Fatalf("sub 行数 = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("第 %d 行不一致:\n got %s\nwant %s", i, got[i], want[i])
		}
	}
}

// TestRenderLinks_未知类型 未知节点类型报错而非静默产出空行。
func TestRenderLinks_未知类型(t *testing.T) {
	nodes := fixtureNodes()
	nodes[0].Type = conf.NodeType("trojan")
	_, err := RenderLinks(nodes)
	if err == nil || !strings.Contains(err.Error(), "trojan") {
		t.Fatalf("期望未知类型错误,实际: %v", err)
	}
}

// ===== RenderSingBox =====

// TestRenderSingBox_结构 断言 outbound 集合/代理组/凭据一致。
func TestRenderSingBox_结构(t *testing.T) {
	nodes := fixtureNodes()
	data, err := RenderSingBox(nodes)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	m := parseJSONMap(t, data)
	// outbound tags:direct + 2 节点 + auto + proxy
	tags := outboundTags(m)
	want := []string{"direct", "hk-01-vless", "hk-01-h2", "auto", "proxy"}
	if len(tags) != len(want) {
		t.Fatalf("outbound 数量 = %d, want %d\n%s", len(tags), len(want), data)
	}
	for i, w := range want {
		if tags[i] != w {
			t.Fatalf("outbound[%d] tag = %q, want %q", i, tags[i], w)
		}
	}
	// vless 凭据一致(防漂移)
	if got := walk(m, "outbounds", "1", "uuid"); got != nodes[0].UUID {
		t.Fatalf("vless uuid 漂移: %v", got)
	}
	if got := walk(m, "outbounds", "1", "tls", "reality", "public_key"); got != nodes[0].PublicKey {
		t.Fatal("vless public_key 漂移")
	}
	// h2 自签:insecure true 无 server_name
	if got := walk(m, "outbounds", "2", "tls", "insecure"); got != true {
		t.Fatal("h2 自签应 insecure=true")
	}
	// urltest 组引用全部节点
	auto := walk(m, "outbounds", "3", "outbounds").([]any)
	if len(auto) != 2 {
		t.Fatalf("urltest 引用数 = %d", len(auto))
	}
	// 路由 final proxy
	if got := walk(m, "route", "final"); got != "proxy" {
		t.Fatalf("route.final = %v", got)
	}
	// 出站域名解析器(sing-box 1.12+ 要求,1.14 起缺失拒启)
	if got := walk(m, "route", "default_domain_resolver"); got != "dns-direct" {
		t.Fatalf("route.default_domain_resolver = %v", got)
	}
	// 内网直连规则在
	if got := walk(m, "route", "rules", "3", "ip_is_private"); got != true {
		t.Fatal("缺 ip_is_private 直连规则")
	}
	// 通用版 inbounds 锚点:仅 mixed,绝不能混入 tun(占位化注入防回归)
	inbounds, _ := m["inbounds"].([]any)
	if len(inbounds) != 1 {
		t.Fatalf("通用版 inbounds 数量 = %d, want 1(仅 mixed)", len(inbounds))
	}
	if got := walk(inbounds[0], "type"); got != "mixed" {
		t.Fatalf("通用版 inbounds[0].type = %v, want mixed", got)
	}
	lp, ok := walk(inbounds[0], "listen_port").(json.Number)
	if !ok || lp.String() != "7890" {
		t.Fatalf("通用版 inbounds[0].listen_port = %v, want 7890", walk(inbounds[0], "listen_port"))
	}
	if strings.Contains(string(data), `"type": "tun"`) {
		t.Fatal("通用版不应含 tun inbound")
	}
	// 内置分流锚点:rule_set 三个官方 srs、dns/route 引用、下载客户端与缓存
	rs, _ := m["route"].(map[string]any)["rule_set"].([]any)
	if len(rs) != 4 {
		t.Fatalf("rule_set 数量 = %d, want 4(cn/!cn/geoip/ads)", len(rs))
	}
	for i, want := range []string{"geosite-geolocation-cn", "geosite-geolocation-!cn", "geoip-cn", "geosite-category-ads-all"} {
		if walk(rs[i], "tag") != want {
			t.Fatalf("rule_set[%d].tag = %v, want %s", i, walk(rs[i], "tag"), want)
		}
	}
	// v14 形态守护(与 OpenWrt legacy 双形态互锚):桌面版 rule_set 各项目前
	// 绝不带 download_detour,下载 detour 由 http_clients/default_http_client
	// 承担;若模板误把 legacy 分支带进桌面产物,这里即刻暴露
	for i := range rs {
		if walk(rs[i], "download_detour") != nil {
			t.Fatalf("rule_set[%d] 不应含 download_detour(桌面 v14 形态): %v", i, walk(rs[i], "download_detour"))
		}
	}
	if got := walk(m, "route", "default_http_client"); got != "rule-set-download" {
		t.Fatalf("route.default_http_client = %v", got)
	}
	hc, _ := m["http_clients"].([]any)
	if len(hc) != 1 || walk(hc[0], "detour") != "proxy" {
		t.Fatalf("http_clients 应含一个 detour=proxy 的下载客户端: %v", hc)
	}
	if got := walk(m, "dns", "rules", "0", "rule_set"); got != "geosite-geolocation-cn" {
		t.Fatalf("dns.rules[0] 应让国内域名走 dns-direct: %v", got)
	}
	if got := walk(m, "experimental", "cache_file", "enabled"); got != true {
		t.Fatal("experimental.cache_file 应启用(规则集缓存必需)")
	}
	// 广告拦截:reject 在分流放行之前(hijack-dns 后第一条)
	if got := walk(m, "route", "rules", "2", "rule_set"); got != "geosite-category-ads-all" {
		t.Fatalf("route.rules[2] 应为广告拦截: %v", got)
	}
	if got := walk(m, "route", "rules", "2", "action"); got != "reject" {
		t.Fatalf("route.rules[2].action = %v, want reject", got)
	}
	// 路由:国内域名直连 + geoip-cn 兜底直连(行为语义层锚定)
	if got := walk(m, "route", "rules", "4", "rule_set"); got != "geosite-geolocation-cn" {
		t.Fatalf("route.rules[4] 应为国内站点直连: %v", got)
	}
	if got := walk(m, "route", "rules", "4", "action"); got != "route" {
		t.Fatalf("route.rules[4].action = %v, want route", got)
	}
	if got := walk(m, "route", "rules", "4", "outbound"); got != "direct" {
		t.Fatalf("route.rules[4] 应直连: outbound = %v", got)
	}
	if got := walk(m, "route", "rules", "5", "type"); got != "logical" {
		t.Fatalf("route.rules[5] 应为 geoip 兜底 logical 规则: %v", got)
	}
	if got := walk(m, "route", "rules", "5", "rules", "0", "rule_set"); got != "geoip-cn" {
		t.Fatalf("route.rules[5] 内层应含 geoip-cn: %v", got)
	}
	if got := walk(m, "route", "rules", "5", "rules", "1", "rule_set"); got != "geosite-geolocation-!cn" {
		t.Fatalf("route.rules[5] 内层应含 geosite-geolocation-!cn: %v", got)
	}
	if got := walk(m, "route", "rules", "5", "rules", "1", "invert"); got != true {
		t.Fatal("route.rules[5] 内层 !cn 应 invert")
	}
	if got := walk(m, "route", "rules", "5", "outbound"); got != "direct" {
		t.Fatalf("route.rules[5] 兜底应直连: outbound = %v", got)
	}
	if got := walk(hc[0], "tag"); got != "rule-set-download" {
		t.Fatalf("http_clients[0].tag = %v, want rule-set-download(与 default_http_client 互锚)", got)
	}
}

// TestRenderSingBoxSFM_结构 SFM 专用版:inbounds 为 mixed+tun 且 tun
// 字段正确;outbounds 与通用版一致(双版本仅 inbounds 不同,防漂移)。
func TestRenderSingBoxSFM_结构(t *testing.T) {
	nodes := fixtureNodes()
	data, err := RenderSingBoxSFM(nodes)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	m := parseJSONMap(t, data)
	inbounds, _ := m["inbounds"].([]any)
	if len(inbounds) != 2 {
		t.Fatalf("inbounds 数量 = %d, want 2(mixed+tun)", len(inbounds))
	}
	tun, _ := inbounds[1].(map[string]any)
	if tun["type"] != "tun" || tun["tag"] != "tun-in" {
		t.Fatalf("inbounds[1] 应为 tun: %v", tun)
	}
	if tun["auto_route"] != true || tun["strict_route"] != true {
		t.Fatal("tun 应 auto_route/strict_route=true(全接管)")
	}
	// 系统代理卡片依赖 platform.http_proxy(parseJSONMap 用 UseNumber,
	// server_port 为 json.Number)
	platform, _ := tun["platform"].(map[string]any)
	hp, _ := platform["http_proxy"].(map[string]any)
	if hp == nil || hp["enabled"] != true || hp["server"] != "127.0.0.1" {
		t.Fatalf("tun platform.http_proxy 配置错误: %v", hp)
	}
	hpPort, ok := hp["server_port"].(json.Number)
	if !ok || hpPort.String() != "7890" {
		t.Fatalf("tun platform.http_proxy.server_port = %v, want 7890", hp["server_port"])
	}
	// 双端口对等:http_proxy.server_port 必须与 mixed listen_port 一致
	// (两处模板常量各自硬编码,防只改一处造成漂移)
	mixedPort, ok2 := walk(inbounds[0], "listen_port").(json.Number)
	if !ok2 || mixedPort.String() != hpPort.String() {
		t.Fatalf("mixed listen_port(%v) 与 http_proxy.server_port(%v) 不对等", mixedPort, hpPort)
	}
	// 与通用版对照:outbounds 完全一致(仅 inbounds 不同)
	plain, err := RenderSingBox(nodes)
	if err != nil {
		t.Fatalf("通用版渲染失败: %v", err)
	}
	mPlain := parseJSONMap(t, plain)
	if !reflect.DeepEqual(m["outbounds"], mPlain["outbounds"]) {
		t.Fatalf("SFM 版与通用版 outbounds 不一致\nSFM: %v\nplain: %v",
			m["outbounds"], mPlain["outbounds"])
	}
}

// checkTunVariant 桌面 tun 变体共享断言:inbounds=[mixed,tun]、tun 基础
// 字段(auto_route/strict_route,按平台形态传入)、outbounds 与通用版一致;
// extra 校验平台特有字段(值可为标量或嵌套结构,DeepEqual 比较)。
func checkTunVariant(t *testing.T, data, plain []byte, wantAutoRoute bool, extra map[string]any) {
	t.Helper()
	m := parseJSONMap(t, data)
	inbounds, _ := m["inbounds"].([]any)
	if len(inbounds) != 2 {
		t.Fatalf("inbounds 数量 = %d, want 2(mixed+tun)", len(inbounds))
	}
	tun, _ := inbounds[1].(map[string]any)
	if tun["type"] != "tun" || tun["tag"] != "tun-in" {
		t.Fatalf("inbounds[1] 应为 tun: %v", tun)
	}
	if tun["auto_route"] != wantAutoRoute || tun["strict_route"] != wantAutoRoute {
		t.Fatalf("tun auto_route/strict_route = %v/%v, want %v(mac/Linux 全接管,Windows 载体形态)",
			tun["auto_route"], tun["strict_route"], wantAutoRoute)
	}
	for k, v := range extra {
		if !reflect.DeepEqual(tun[k], v) {
			t.Fatalf("tun[%q] = %v, want %v", k, tun[k], v)
		}
	}
	if !reflect.DeepEqual(m["outbounds"], parseJSONMap(t, plain)["outbounds"]) {
		t.Fatalf("tun 变体与通用版 outbounds 不一致\ntun: %v\nplain: %v",
			m["outbounds"], parseJSONMap(t, plain)["outbounds"])
	}
}

// TestRenderSingBoxSFW_结构 Windows 版:与 macOS 版同构(带
// platform.http_proxy——Windows 实测缺该段时 SFW 无系统代理能力)。
func TestRenderSingBoxSFW_结构(t *testing.T) {
	nodes := fixtureNodes()
	data, err := RenderSingBoxSFW(nodes)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	plain, err := RenderSingBox(nodes)
	if err != nil {
		t.Fatalf("通用版渲染失败: %v", err)
	}
	checkTunVariant(t, data, plain, false, nil)
	// platform.http_proxy 必须存在(与 sfm 版同构;否则 SFW 无系统代理开关)
	m := parseJSONMap(t, data)
	tun := m["inbounds"].([]any)[1].(map[string]any)
	platform, _ := tun["platform"].(map[string]any)
	hp, _ := platform["http_proxy"].(map[string]any)
	if hp == nil || hp["enabled"] != true || hp["server"] != "127.0.0.1" {
		t.Fatalf("SFW 版缺 platform.http_proxy(系统代理开关依赖): %v", tun["platform"])
	}
	hpPort, ok := hp["server_port"].(json.Number)
	if !ok || hpPort.String() != "7890" {
		t.Fatalf("SFW 版 http_proxy.server_port = %v, want 7890", hp["server_port"])
	}
	// 双端口对等:mixed listen_port 必须与 http_proxy.server_port 一致
	// (mixed/sfm/sfw 三处常量各自硬编码 7890,防只改一处造成漂移)
	mixedPort, ok2 := walk(m, "inbounds", "0", "listen_port").(json.Number)
	if !ok2 || mixedPort.String() != hpPort.String() {
		t.Fatalf("SFW mixed listen_port(%v) 与 http_proxy.server_port(%v) 不对等", mixedPort, hpPort)
	}
}

// TestRenderSingBoxSFL_结构 Linux 版:基础 tun + auto_redirect(nftables,
// 官方推荐;内核不支持时可删该字段)。
func TestRenderSingBoxSFL_结构(t *testing.T) {
	nodes := fixtureNodes()
	data, err := RenderSingBoxSFL(nodes)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	plain, err := RenderSingBox(nodes)
	if err != nil {
		t.Fatalf("通用版渲染失败: %v", err)
	}
	checkTunVariant(t, data, plain, true, map[string]any{"auto_redirect": true})
}

// TestRenderSingBoxOpenWrt_结构 OpenWrt 网关盒子版:inbounds 仅 tun(无
// mixed/无 platform),auto_redirect=true 接管 fw4、strict_route=false 让位
// 既有路由策略;route.auto_detect_interface 防环;cache_file 显式落盘
// /etc/sing-box/cache.db(tmpfs 重启丢失问题);规则集下载为 legacy 形态
// (无 http_clients/default_http_client,rule_set 每项 download_detour=
// proxy——官方源 1.12/1.13 兼容);outbounds 与通用版一致。
func TestRenderSingBoxOpenWrt_结构(t *testing.T) {
	nodes := fixtureNodes()
	data, err := RenderSingBoxOpenWrt(nodes)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	m := parseJSONMap(t, data)
	// inbounds 恰 1 个且为 tun(无 mixed/无 platform——网关盒子形态)
	inbounds, _ := m["inbounds"].([]any)
	if len(inbounds) != 1 {
		t.Fatalf("inbounds 数量 = %d, want 1(仅 tun)", len(inbounds))
	}
	tun, _ := inbounds[0].(map[string]any)
	if tun["type"] != "tun" || tun["tag"] != "tun-in" {
		t.Fatalf("inbounds[0] 应为 tun: %v", tun)
	}
	if tun["auto_route"] != true {
		t.Fatal("tun 应 auto_route=true(接管系统流量)")
	}
	if tun["auto_redirect"] != true {
		t.Fatal("tun 应 auto_redirect=true(自动接管 fw4 防火墙)")
	}
	if tun["strict_route"] != false {
		t.Fatal("tun 应 strict_route=false(盒子已有 fw4 路由策略,避免 ip rule 冲突)")
	}
	if _, ok := tun["platform"]; ok {
		t.Fatalf("盒子无 GUI,不应有 platform 段: %v", tun["platform"])
	}
	// 双栈地址防 IPv6 直连泄漏
	addr, _ := tun["address"].([]any)
	wantAddr := []string{"172.18.0.1/30", "fdfe:dcba:9876::1/126"}
	if len(addr) != 2 {
		t.Fatalf("tun address = %v, want 双栈", tun["address"])
	}
	for i, w := range wantAddr {
		if addr[i] != w {
			t.Fatalf("tun address[%d] = %v, want %s", i, addr[i], w)
		}
	}
	// 出站接口自动检测(防 TUN 环路)在 route 内
	if got := walk(m, "route", "auto_detect_interface"); got != true {
		t.Fatalf("route.auto_detect_interface = %v, want true", got)
	}
	// cache_file 显式落盘(OpenWrt /etc 非 tmpfs,重启不丢规则集缓存)
	if got := walk(m, "experimental", "cache_file", "path"); got != "/etc/sing-box/cache.db" {
		t.Fatalf("cache_file.path = %v, want /etc/sing-box/cache.db", got)
	}
	// 规则集下载用 legacy 形态(官方源 1.12/1.13 DisallowUnknownFields,
	// 桌面 v14 的 http_clients/default_http_client 对它们直接 FATAL):
	// 顶层无 http_clients、route 无 default_http_client
	if _, ok := m["http_clients"]; ok {
		t.Fatal("OpenWrt legacy 形态不应有顶层 http_clients(1.12/1.13 解析 FATAL)")
	}
	routeMap, _ := m["route"].(map[string]any)
	if _, ok := routeMap["default_http_client"]; ok {
		t.Fatal("OpenWrt legacy 形态不应有 route.default_http_client")
	}
	// 下载 detour 由每个 rule_set 的 download_detour=proxy 承担(替代
	// http_clients;1.12 起支持,分流语义与桌面 v14 一致)
	rs, _ := routeMap["rule_set"].([]any)
	if len(rs) != 4 {
		t.Fatalf("rule_set 数量 = %d, want 4", len(rs))
	}
	for i := range rs {
		if walk(rs[i], "download_detour") != "proxy" {
			t.Fatalf("rule_set[%d].download_detour = %v, want proxy", i, walk(rs[i], "download_detour"))
		}
	}
	// 与通用版对照:outbounds 完全一致(仅 inbounds/route/cache 不同)
	plain, err := RenderSingBox(nodes)
	if err != nil {
		t.Fatalf("通用版渲染失败: %v", err)
	}
	if !reflect.DeepEqual(m["outbounds"], parseJSONMap(t, plain)["outbounds"]) {
		t.Fatal("OpenWrt 版与通用版 outbounds 不一致")
	}
}

// TestRenderSingBox_H2受信 受信证书节点渲染 server_name 且 insecure=false。
func TestRenderSingBox_H2受信(t *testing.T) {
	nodes := fixtureNodes()
	nodes[1].Insecure = false
	nodes[1].ServerName = "vpn.example.com"
	data, err := RenderSingBox(nodes)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	m := parseJSONMap(t, data)
	if got := walk(m, "outbounds", "2", "tls", "server_name"); got != "vpn.example.com" {
		t.Fatalf("h2 server_name = %v", got)
	}
	if got := walk(m, "outbounds", "2", "tls", "insecure"); got != false {
		t.Fatal("受信证书应 insecure=false")
	}
}

// outboundTags 收集 outbounds 的 tag(测试辅助)。
func outboundTags(m map[string]any) []string {
	list, _ := m["outbounds"].([]any)
	out := make([]string, 0, len(list))
	for _, e := range list {
		em, _ := e.(map[string]any)
		if tag, ok := em["tag"].(string); ok {
			out = append(out, tag)
		}
	}
	return out
}

// parseJSONMap 解析 JSON 到 map(UseNumber;测试辅助)。
func parseJSONMap(t *testing.T, data []byte) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("JSON 解析失败: %v\n%s", err, data)
	}
	return m
}

// walk 沿 path 逐层取值(map 键 + 数组下标;测试辅助)。
func walk(m any, path ...string) any {
	var cur any = m
	for _, k := range path {
		switch v := cur.(type) {
		case map[string]any:
			cur = v[k]
		case []any:
			var idx int
			if _, err := fmt.Sscanf(k, "%d", &idx); err != nil || idx < 0 || idx >= len(v) {
				return nil
			}
			cur = v[idx]
		default:
			return nil
		}
	}
	return cur
}

// TestCrossArtifact_一致性 两产物(links/sing-box.json)凭据互查。
// 黄金断言:同一节点的 uuid/密码在全部产物中一致(防凭据漂移)。
func TestCrossArtifact_一致性(t *testing.T) {
	nodes := fixtureNodes()
	links, _ := RenderLinks(nodes)
	sb, _ := RenderSingBox(nodes)
	// vless uuid 必须同时出现在两个产物
	uuid := nodes[0].UUID
	for name, content := range map[string]string{"links": links, "sing-box.json": string(sb)} {
		if !strings.Contains(content, uuid) {
			t.Fatalf("%s 缺 vless uuid(漂移)", name)
		}
	}
	// h2 密码必须同时出现在两个产物
	h2Pass := nodes[1].Password
	for name, content := range map[string]string{"links": links, "sing-box.json": string(sb)} {
		if !strings.Contains(content, h2Pass) {
			t.Fatalf("%s 缺 h2 密码(漂移)", name)
		}
	}
	// sing-box.json 与服务端对应:短 id 一致
	if !strings.Contains(string(sb), nodes[0].ShortID) {
		t.Fatal("sing-box.json 缺 short-id(漂移)")
	}
}

// ===== 注入安全(特殊字符跨产物) =====

// TestShareLink_h2密码特殊字符 空格必须 %20 编码进 userinfo(QueryEscape 产出
// + 而 userinfo 段不还原 +,link.go 有专门 ReplaceAll);解码后与原文全等。
func TestShareLink_h2密码特殊字符(t *testing.T) {
	n := fixtureNodes()[1]
	pass := "p@ss word+&密码"
	n.Password = pass
	link := ShareLink(n)
	if strings.Contains(link, " ") {
		t.Fatalf("链接含未编码空格: %s", link)
	}
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("解析失败: %v (%s)", err, link)
	}
	if u.User.Username() != pass {
		t.Fatalf("auth 解码 = %q, want %q(空格/%%2B/& 均须往返无损)", u.User.Username(), pass)
	}
}

// TestCrossArtifact_特殊字符注入 含引号/反斜杠的 Name 与 Password 跨产物
// 渲染安全(黄金注入测试):sing-box JSON 合法且值还原、链接 fragment 经
// PathEscape 可解析还原。
func TestCrossArtifact_特殊字符注入(t *testing.T) {
	nodes := fixtureNodes()
	nodes[0].Name = `hk"x`      // 引号进 tag/name/fragment(PathEscape)
	nodes[1].Password = `p"a\b` // 引号与反斜杠进 password(JSON 转义)
	links, err := RenderLinks(nodes)
	if err != nil {
		t.Fatalf("RenderLinks: %v", err)
	}
	sb, err := RenderSingBox(nodes)
	if err != nil {
		t.Fatalf("RenderSingBox: %v", err)
	}
	// parseJSONMap 即兜底 json.Valid;取值须与输入一致(无转义污染/截断)
	m := parseJSONMap(t, sb)
	if got := walk(m, "outbounds", "1", "tag"); got != nodes[0].Name {
		t.Fatalf("vless tag = %v, want %q", got, nodes[0].Name)
	}
	if got := walk(m, "outbounds", "2", "password"); got != nodes[1].Password {
		t.Fatalf("h2 password = %v, want %q", got, nodes[1].Password)
	}
	// Name 经 url.PathEscape 进 fragment,解析后应还原原名(含引号)
	u, err := url.Parse(strings.SplitN(links, "\n", 2)[0])
	if err != nil {
		t.Fatalf("链接解析失败: %v", err)
	}
	if u.Fragment != nodes[0].Name {
		t.Fatalf("fragment = %q, want %q", u.Fragment, nodes[0].Name)
	}
}

// ===== 字段缺失防御(渲染器校验) =====

// TestRenderSingBox_字段缺失 渲染器对凭据缺失做防御(singBoxOutbound 校验)。
func TestRenderSingBox_字段缺失(t *testing.T) {
	cases := []struct {
		name string
		idx  int
		mut  func(*conf.Node)
	}{
		{"vless 缺 UUID", 0, func(n *conf.Node) { n.UUID = "" }},
		{"vless 缺 PublicKey", 0, func(n *conf.Node) { n.PublicKey = "" }},
		{"vless 缺 ShortID", 0, func(n *conf.Node) { n.ShortID = "" }},
		{"h2 缺 Password", 1, func(n *conf.Node) { n.Password = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nodes := fixtureNodes()
			tc.mut(&nodes[tc.idx])
			_, err := RenderSingBox(nodes)
			if err == nil || !strings.Contains(err.Error(), "字段缺失") {
				t.Fatalf("期望错误含 \"字段缺失\",实际: %v", err)
			}
		})
	}
}

// ===== ShareLink 兜底与省略 =====

// TestShareLink_空Name兜底 Name 为空时 fragment 用 Address:Port 兜底。
func TestShareLink_空Name兜底(t *testing.T) {
	n := fixtureNodes()[0]
	n.Name = ""
	u, err := url.Parse(ShareLink(n))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if u.Fragment != "hk.example.com:443" {
		t.Fatalf("fragment = %q, want \"hk.example.com:443\"", u.Fragment)
	}
}

// TestShareLink_vless省略参数 vless 可选参数全空时 query 省略对应键。
func TestShareLink_vless省略参数(t *testing.T) {
	n := fixtureNodes()[0]
	n.ServerName, n.PublicKey, n.ShortID = "", "", ""
	u, err := url.Parse(ShareLink(n))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	q := u.Query()
	for _, k := range []string{"sni", "pbk", "sid"} {
		if q.Has(k) {
			t.Fatalf("query 不应含 %s: %s", k, ShareLink(n))
		}
	}
	// 常量参数仍在(链接可用性)
	if q.Get("type") != "tcp" || q.Get("security") != "reality" {
		t.Fatalf("常量参数丢失: %s", ShareLink(n))
	}
}

// ===== RenderSingBox h2 混淆 =====

// TestRenderSingBox_h2混淆 h2 混淆节点渲染 obfs(type=salamander)且自签
// insecure 保持 true。
func TestRenderSingBox_h2混淆(t *testing.T) {
	nodes := fixtureNodes()
	nodes[1].Obfs = "obfs-secret"
	data, err := RenderSingBox(nodes)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	m := parseJSONMap(t, data)
	if got := walk(m, "outbounds", "2", "obfs", "type"); got != "salamander" {
		t.Fatalf("obfs.type = %v", got)
	}
	if got := walk(m, "outbounds", "2", "obfs", "password"); got != "obfs-secret" {
		t.Fatalf("obfs.password = %v", got)
	}
	if got := walk(m, "outbounds", "2", "tls", "insecure"); got != true {
		t.Fatal("自签混淆节点应保持 insecure=true")
	}
}
