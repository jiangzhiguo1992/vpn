// Package client 客户端产物测试:分享链接与各配置渲染。
//
// 重点:链接/配置字段与输入节点一致(防凭据漂移)与跨产物一致性。
package client

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"vpn/internal/conf"
)

// fixtureNodes 返回三通道节点基线(vless/ss/h2 自签)。
func fixtureNodes() []conf.Node {
	return []conf.Node{
		{Name: "hk-01-vless", Location: "香港", Type: conf.TypeVLESSReality,
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
	// 内网直连规则在
	if got := walk(m, "route", "rules", "2", "ip_is_private"); got != true {
		t.Fatal("缺 ip_is_private 直连规则")
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

// ===== RenderClash =====

// TestRenderClash_结构 断言节点块/组/规则与凭据一致。
func TestRenderClash_结构(t *testing.T) {
	nodes := fixtureNodes()
	data, err := RenderClash(nodes)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	out := string(data)
	// 节点块数量(只统计 proxies 段,排除 proxy-groups 的两个组)
	pStart, pEnd := strings.Index(out, "proxies:"), strings.Index(out, "proxy-groups:")
	seg := out[pStart:pEnd]
	if got := strings.Count(seg, "  - name:"); got != 2 {
		t.Fatalf("proxy 块数 = %d, want 2", got)
	}
	for _, want := range []string{
		"type: vless", "uuid: \"8a2f3dfa-ddf3-471a-ab3a-d4110d631d92\"",
		"flow: xtls-rprx-vision", "servername: \"www.apple.com\"",
		"client-fingerprint: chrome", "reality-opts:",
		"public-key: \"zRAPkZIJ-p7lWdzOi4i4O8JUas5vvd3TzMmYUQm1i2w\"",
		"short-id: \"cafe2554decd2a45\"",
		"type: hysteria2", "skip-cert-verify: true",
		"name: PROXY", "name: AUTO", "type: url-test",
		"GEOSITE,cn,DIRECT", "GEOIP,CN,DIRECT", "MATCH,PROXY",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("clash.yaml 缺关键内容 %q\n%s", want, out)
		}
	}
	// 规则顺序(site 先于 ip 先于兜底)
	si, gi, mi := strings.Index(out, "GEOSITE,cn,DIRECT"),
		strings.Index(out, "GEOIP,CN,DIRECT"), strings.Index(out, "MATCH,PROXY")
	if !(si >= 0 && si < gi && gi < mi) {
		t.Fatal("规则顺序错误:应 GEOSITE → GEOIP → MATCH")
	}
}

// TestRenderClash_H2受信与混淆 受信证书渲染 sni、混淆渲染 obfs。
func TestRenderClash_H2受信与混淆(t *testing.T) {
	nodes := fixtureNodes()
	nodes[1].Insecure = false
	nodes[1].ServerName = "vpn.example.com"
	nodes[1].Obfs = "obfs-secret"
	data, err := RenderClash(nodes)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	out := string(data)
	for _, want := range []string{"sni: \"vpn.example.com\"", "obfs: salamander", "obfs-password: \"obfs-secret\""} {
		if !strings.Contains(out, want) {
			t.Fatalf("缺 %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "skip-cert-verify: true") {
		t.Fatal("受信证书不应 skip-cert-verify")
	}
}

// TestRenderClash_未知类型 未知节点类型报错。
func TestRenderClash_未知类型(t *testing.T) {
	nodes := fixtureNodes()
	nodes[1].Type = conf.NodeType("trojan")
	_, err := RenderClash(nodes)
	if err == nil || !strings.Contains(err.Error(), "trojan") {
		t.Fatalf("期望未知类型错误,实际: %v", err)
	}
}

// TestCrossArtifact_一致性 三产物(links/sing-box/clash)凭据互查。
// 黄金断言:同一节点的 uuid/密码在全部产物中一致(防凭据漂移)。
func TestCrossArtifact_一致性(t *testing.T) {
	nodes := fixtureNodes()
	links, _ := RenderLinks(nodes)
	sb, _ := RenderSingBox(nodes)
	clash, _ := RenderClash(nodes)
	// vless uuid 必须同时出现在三个产物
	uuid := nodes[0].UUID
	for name, content := range map[string]string{"links": links, "sing-box.json": string(sb), "clash.yaml": string(clash)} {
		if !strings.Contains(content, uuid) {
			t.Fatalf("%s 缺 vless uuid(漂移)", name)
		}
	}
	// h2 密码必须同时出现在三个产物
	h2Pass := nodes[1].Password
	for name, content := range map[string]string{"links": links, "sing-box.json": string(sb), "clash.yaml": string(clash)} {
		if !strings.Contains(content, h2Pass) {
			t.Fatalf("%s 缺 h2 密码(漂移)", name)
		}
	}
	// clash.yaml 与服务端对应:短 id 一致
	if !strings.Contains(string(clash), nodes[0].ShortID) {
		t.Fatal("clash.yaml 缺 short-id(漂移)")
	}
}
