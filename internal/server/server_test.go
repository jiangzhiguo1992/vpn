// Package server 服务端产物测试:配置渲染与产物写盘。
//
// 重点:产物关键字段与输入清单一致(防凭据漂移)与产物文件权限。
package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"vpn/internal/conf"
)

// fixtureServer 返回已回填的合法三通道服务器(测试基线)。
func fixtureServer(t *testing.T) *conf.Server {
	t.Helper()
	s := &conf.Server{
		Name:        "hk-01",
		Address:     "hk.example.com",
		VLESS:       &conf.VLESSConfig{ServerName: "www.apple.com"},
		Shadowsocks: &conf.SSConfig{},
		Hysteria2:   &conf.H2Config{},
	}
	if err := (&conf.Inventory{Servers: []*conf.Server{s}}).Backfill(); err != nil {
		t.Fatalf("回填失败: %v", err)
	}
	return s
}

// parseJSON 把字节解析为 map(UseNumber 保留端口等数字字面量)。
func parseJSON(t *testing.T, data []byte) map[string]any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("JSON 解析失败: %v\n%s", err, data)
	}
	return m
}

// walk 沿 path 逐层取值(支持 map 键与数组下标 "0";越界返回 nil)。
func walk(m any, path ...string) any {
	var cur any = m
	for _, k := range path {
		switch v := cur.(type) {
		case map[string]any:
			cur = v[k]
		case []any:
			idx, err := strconv.Atoi(k)
			if err != nil || idx < 0 || idx >= len(v) {
				return nil
			}
			cur = v[idx]
		default:
			return nil
		}
	}
	return cur
}

// inbound 取第 i 个 inbound(测试辅助)。
func inbound(m map[string]any, i int) map[string]any {
	list, _ := m["inbounds"].([]any)
	if i >= len(list) {
		return nil
	}
	im, _ := list[i].(map[string]any)
	return im
}

// ===== RenderConfig =====

// TestRenderConfig_三通道 验证三 inbound 顺序/字段与清单凭据一致。
func TestRenderConfig_三通道(t *testing.T) {
	s := fixtureServer(t)
	data, err := RenderConfig(s)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	m := parseJSON(t, data)
	if got := walk(m, "log", "level"); got != "info" {
		t.Fatalf("log.level = %v", got)
	}
	if got := walk(m, "outbounds", "0", "type"); got != "direct" {
		t.Fatalf("outbound 类型 = %v", got)
	}
	if got := walk(m, "route", "final"); got != "direct" {
		t.Fatalf("route.final = %v", got)
	}
	// 三 inbound:类型顺序 vless/shadowsocks/hysteria2
	wantTypes := []string{"vless", "shadowsocks", "hysteria2"}
	for i, wt := range wantTypes {
		if got := walk(m, "inbounds", fmt.Sprint(i), "type"); got != wt {
			t.Fatalf("inbound[%d] 类型 = %v, want %s", i, got, wt)
		}
	}
	// vless:凭据与伪装站点一致性(防漂移)
	if got := walk(m, "inbounds", "0", "users", "0", "uuid"); got != s.VLESS.UUID {
		t.Fatalf("vless uuid 漂移: %v", got)
	}
	if got := walk(m, "inbounds", "0", "tls", "reality", "private_key"); got != s.VLESS.PrivateKey {
		t.Fatal("vless private_key 漂移")
	}
	if got := walk(m, "inbounds", "0", "tls", "reality", "short_id", "0"); got != s.VLESS.ShortID {
		t.Fatal("vless short_id 漂移")
	}
	if got := walk(m, "inbounds", "0", "tls", "server_name"); got != "www.apple.com" {
		t.Fatalf("vless server_name = %v", got)
	}
	if got := fmt.Sprint(walk(m, "inbounds", "0", "tls", "reality", "handshake", "server_port")); got != "443" {
		t.Fatalf("handshake server_port = %v(应恒 443,与监听端口无关)", got)
	}
	if got := fmt.Sprint(walk(m, "inbounds", "0", "listen_port")); got != "443" {
		t.Fatalf("vless listen_port = %v", got)
	}
	// ss:method/password;network 字段必须省略(sing-box 留空默认
	// tcp+udp 双栈,显式 "tcp,udp" 会被 schema 校验拒绝)
	if got := walk(m, "inbounds", "1", "password"); got != s.Shadowsocks.Password {
		t.Fatal("ss password 漂移")
	}
	if walk(m, "inbounds", "1", "network") != nil {
		t.Fatal("ss 不应渲染 network 字段(留空=默认 tcp+udp)")
	}
	if got := walk(m, "inbounds", "1", "method"); got != "aes-256-gcm" {
		t.Fatalf("ss method = %v", got)
	}
	// h2:密码与证书路径
	if got := walk(m, "inbounds", "2", "users", "0", "password"); got != s.Hysteria2.Password {
		t.Fatal("h2 password 漂移")
	}
	if got := walk(m, "inbounds", "2", "tls", "certificate_path"); got != "/etc/sing/cert.pem" {
		t.Fatalf("h2 证书路径 = %v", got)
	}
	if walk(m, "inbounds", "2", "obfs") != nil {
		t.Fatal("未配混淆不应渲染 obfs 段")
	}
}

// TestRenderConfig_通道组合 表驱动覆盖通道组合与可选段。
func TestRenderConfig_通道组合(t *testing.T) {
	mk := func(mut func(*conf.Server)) *conf.Server {
		s := fixtureServer(t)
		mut(s)
		return s
	}
	cases := []struct {
		name     string
		srv      *conf.Server
		wantTags []string
		wantObfs bool
	}{
		{"仅 vless", mk(func(s *conf.Server) { s.Shadowsocks, s.Hysteria2 = nil, nil }), []string{"vless-in"}, false},
		{"vless+ss", mk(func(s *conf.Server) { s.Hysteria2 = nil }), []string{"vless-in", "ss-in"}, false},
		{"仅 h2 带混淆", mk(func(s *conf.Server) {
			s.VLESS, s.Shadowsocks = nil, nil
			s.Hysteria2.ObfsPassword = "obfs-secret"
		}), []string{"hy2-in"}, true},
		{"h2 受信证书模式", mk(func(s *conf.Server) {
			s.VLESS, s.Shadowsocks = nil, nil
			s.Hysteria2.ServerName = "vpn.example.com"
		}), []string{"hy2-in"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := RenderConfig(tc.srv)
			if err != nil {
				t.Fatalf("渲染失败: %v", err)
			}
			m := parseJSON(t, data)
			list, _ := m["inbounds"].([]any)
			if len(list) != len(tc.wantTags) {
				t.Fatalf("inbound 数量 = %d, want %d", len(list), len(tc.wantTags))
			}
			for i, tag := range tc.wantTags {
				if got := walk(m, "inbounds", fmt.Sprint(i), "tag"); got != tag {
					t.Fatalf("inbound[%d] tag = %v, want %s", i, got, tag)
				}
			}
			h2 := inbound(m, len(tc.wantTags)-1)
			if tc.wantObfs {
				if got := walk(h2, "obfs", "type"); got != "salamander" {
					t.Fatalf("obfs.type = %v", got)
				}
			} else if walk(h2, "obfs") != nil {
				t.Fatal("不应渲染 obfs 段")
			}
		})
	}
}

// TestRenderConfig_异常 验证未回填/无通道等输入报错而非产出坏配置。
func TestRenderConfig_异常(t *testing.T) {
	cases := []struct {
		name string
		srv  *conf.Server
		want string
	}{
		{"无通道", &conf.Server{Name: "x", Address: "1.2.3.4"}, "无协议通道"},
		{"vless 未回填", &conf.Server{Name: "x", Address: "1.2.3.4", VLESS: &conf.VLESSConfig{ServerName: "a.com"}}, "未回填"},
		{"ss 未回填", &conf.Server{Name: "x", Address: "1.2.3.4", Shadowsocks: &conf.SSConfig{}}, "未回填"},
		{"h2 未回填", &conf.Server{Name: "x", Address: "1.2.3.4", Hysteria2: &conf.H2Config{}}, "未回填"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := RenderConfig(tc.srv)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("期望错误含 %q,实际: %v", tc.want, err)
			}
		})
	}
}

// ===== WriteArtifacts =====

// TestWriteArtifacts_文件与权限 验证产物集合、权限与内容要点。
func TestWriteArtifacts_文件与权限(t *testing.T) {
	s := fixtureServer(t)
	dir := t.TempDir()
	if err := WriteArtifacts(dir, s); err != nil {
		t.Fatalf("写产物失败: %v", err)
	}
	// 文件集合(自签 h2 → 含 cert.sh)
	for _, f := range []string{"config.json", "docker-compose.yml", "deploy.sh", "cert.sh"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatalf("缺产物 %s: %v", f, err)
		}
	}
	mustPerm(t, filepath.Join(dir, "config.json"), 0o600)
	mustPerm(t, filepath.Join(dir, "docker-compose.yml"), 0o644)
	mustPerm(t, filepath.Join(dir, "deploy.sh"), 0o755)
	mustPerm(t, filepath.Join(dir, "cert.sh"), 0o755)
	compose, _ := os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
	if !strings.Contains(string(compose), "ghcr.io/sagernet/sing-box:"+ImageVersion) {
		t.Fatal("compose 缺锁版镜像引用")
	}
	if !strings.Contains(string(compose), "./cert.pem:/etc/sing/cert.pem:ro") {
		t.Fatal("compose 缺证书挂载(h2 服务器)")
	}
	cfg, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	if !json.Valid(cfg) {
		t.Fatal("config.json 不是合法 JSON")
	}
	// deploy.sh 端口与镜像回退
	sh, _ := os.ReadFile(filepath.Join(dir, "deploy.sh"))
	for _, want := range []string{
		"for p in 443 8388",  // vless+ss TCP
		"for p in 8388 8443", // ss+hy2 UDP
		ImageVersion,
		"docker.io/sagernet/sing-box",
		"check -c /etc/sing-box/config.json",
		"force-recreate",
	} {
		if !strings.Contains(string(sh), want) {
			t.Fatalf("deploy.sh 缺关键内容 %q", want)
		}
	}
}

// TestWriteArtifacts_证书模式 验证受信证书模式不生成 cert.sh 且 deploy.sh
// 引导放置证书;无 h2 时清理残留 cert.sh。
func TestWriteArtifacts_证书模式(t *testing.T) {
	// 受信证书模式(server_name 非空)
	s := fixtureServer(t)
	s.VLESS, s.Shadowsocks = nil, nil
	s.Hysteria2.ServerName = "vpn.example.com"
	dir := t.TempDir()
	if err := WriteArtifacts(dir, s); err != nil {
		t.Fatalf("写产物失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "cert.sh")); !os.IsNotExist(err) {
		t.Fatal("受信证书模式不应生成 cert.sh")
	}
	sh, _ := os.ReadFile(filepath.Join(dir, "deploy.sh"))
	if !strings.Contains(string(sh), "受信证书") || !strings.Contains(string(sh), "cert.pem/key.pem") {
		t.Fatal("deploy.sh 应引导放置受信证书")
	}
	// 无 h2:先自签生成再改无 h2,残留 cert.sh 应被清理
	s2 := fixtureServer(t)
	s2.Hysteria2 = nil
	if err := WriteArtifacts(dir, s2); err != nil {
		t.Fatalf("写产物失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "cert.sh")); !os.IsNotExist(err) {
		t.Fatal("无 h2 后残留 cert.sh 未清理")
	}
}

// TestWriteArtifacts_幂等 验证重复写产物不翻新配置(内容一致)。
func TestWriteArtifacts_幂等(t *testing.T) {
	s := fixtureServer(t)
	dir := t.TempDir()
	if err := WriteArtifacts(dir, s); err != nil {
		t.Fatalf("首次写失败: %v", err)
	}
	first, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	if err := WriteArtifacts(dir, s); err != nil {
		t.Fatalf("二次写失败: %v", err)
	}
	second, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	if !bytes.Equal(first, second) {
		t.Fatal("重复写产物内容变化(凭据或模板漂移)")
	}
}

// mustPerm 断言文件权限(测试辅助)。
func mustPerm(t *testing.T, path string, perm os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if info.Mode().Perm() != perm {
		t.Fatalf("%s 权限 = %o, want %o", path, info.Mode().Perm(), perm)
	}
}

// ===== cert.sh / deploy 脚本细节 =====

// TestCertScript_CN 验证自签证书 CN 取地址(自签模式)且 shell 转义安全。
func TestCertScript_CN(t *testing.T) {
	s := fixtureServer(t)
	cs := certScript(s)
	if !strings.Contains(cs, "CN='hk.example.com'") {
		t.Fatalf("cert.sh CN 应为地址,实际:\n%s", cs)
	}
	// 注入字符地址应被单引号转义(纵深防御)
	s2 := &conf.Server{Name: "x", Address: "a';rm -rf /;'"}
	if err := (&conf.Inventory{Servers: []*conf.Server{s2}}).Backfill(); err != nil {
		t.Fatalf("回填失败: %v", err)
	}
	cs2 := certScript(s2)
	if !strings.Contains(cs2, `CN='a'\'';rm -rf /;'\'''`) {
		t.Fatalf("cert.sh CN 转义失败:\n%s", cs2)
	}
}

// TestDeployScript_证书分支 验证无 h2 时证书段为空、自签段自动生成。
func TestDeployScript_证书分支(t *testing.T) {
	s := fixtureServer(t)
	sh, err := deployScript(s)
	if err != nil {
		t.Fatalf("deployScript: %v", err)
	}
	if !strings.Contains(sh, "生成自签证书") {
		t.Fatal("自签模式 deploy.sh 应含自动生成分支")
	}
	s2 := fixtureServer(t)
	s2.Hysteria2 = nil
	sh2, err := deployScript(s2)
	if err != nil {
		t.Fatalf("deployScript: %v", err)
	}
	if strings.Contains(sh2, "cert.sh") {
		t.Fatal("无 h2 时 deploy.sh 不应引用证书脚本")
	}
	if strings.Contains(sh2, "for p in 8388 8443") {
		t.Fatal("无 h2 时 UDP 放行不应含 8443")
	}
}
