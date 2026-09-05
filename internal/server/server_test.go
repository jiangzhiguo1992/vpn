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
		Name:      "hk-01",
		Address:   "hk.example.com",
		VLESS:     &conf.VLESSConfig{ServerName: "www.apple.com"},
		Hysteria2: &conf.H2Config{},
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

// TestRenderConfig_双通道 验证 inbound 顺序/字段与清单凭据一致。
func TestRenderConfig_双通道(t *testing.T) {
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
	// inbound:类型顺序 vless/hysteria2
	wantTypes := []string{"vless", "hysteria2"}
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
	// h2:密码与证书路径
	if got := walk(m, "inbounds", "1", "users", "0", "password"); got != s.Hysteria2.Password {
		t.Fatal("h2 password 漂移")
	}
	if got := walk(m, "inbounds", "1", "tls", "certificate_path"); got != "/etc/sing/cert.pem" {
		t.Fatalf("h2 证书路径 = %v", got)
	}
	if walk(m, "inbounds", "1", "obfs") != nil {
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
		{"仅 vless", mk(func(s *conf.Server) { s.Hysteria2 = nil }), []string{"vless-in"}, false},
		{"仅 h2 带混淆", mk(func(s *conf.Server) {
			s.VLESS = nil
			s.Hysteria2.ObfsPassword = "obfs-secret"
		}), []string{"hy2-in"}, true},
		{"h2 受信证书模式", mk(func(s *conf.Server) {
			s.VLESS = nil
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
		"for p in 443",  // vless TCP
		"for p in 8443", // hy2 UDP
		ImageVersion,
		"docker.io/sagernet/sing-box",
		"docker compose version",
		"docker-compose-plugin",
		"check -c /etc/sing-box/config.json",
		"force-recreate",
		// 校验容器须与运行容器(compose)同挂载证书:config 的
		// certificate_path 指向容器内 /etc/sing/,check 读不到会误报
		`-v "$PWD/cert.pem:/etc/sing/cert.pem:ro"`,
		`-v "$PWD/key.pem:/etc/sing/key.pem:ro"`,
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
	s.VLESS = nil
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
	// 受信证书模式同样走容器内 /etc/sing/ 挂载,校验命令须挂证书
	if !strings.Contains(string(sh), `-v "$PWD/cert.pem:/etc/sing/cert.pem:ro"`) {
		t.Fatal("deploy.sh 校验命令缺证书挂载(受信证书模式)")
	}
	// 受信模式须带内容预检:自签模式切换后残留的旧自签证书同名存在,
	// 存在性检查拦不住;三道检查——cert/key 公钥匹配、30 天过期预检、
	// 无 SAN 扩展(cert.sh 自签形态)时比对 CN 与 server_name(带 SAN 的
	// 受信证书含通配符,不比对防误报)
	for _, want := range []string{
		"openssl x509 -noout -pubkey",
		"openssl pkey -passin pass: -pubout",
		"cert.pem 与 key.pem 不匹配",
		"-checkend 2592000",
		"-ext subjectAltName",
		"openssl x509 -noout -subject",
		"CN=$CN 与 hysteria2.server_name 不一致",
		"vpn.example.com", // 比对目标(server_name 注入)
	} {
		if !strings.Contains(string(sh), want) {
			t.Fatalf("deploy.sh 受信分支缺内容预检 %q", want)
		}
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
	// 无 h2:校验命令无证书挂载(无 hy2 inbound,config 不含 /etc/sing/)
	sh2, _ := os.ReadFile(filepath.Join(dir, "deploy.sh"))
	if strings.Contains(string(sh2), `-v "$PWD/cert.pem`) {
		t.Fatal("无 h2 时 deploy.sh 校验命令不应挂载证书")
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
	if strings.Contains(sh2, "for p in 8443") {
		t.Fatal("无 h2 时不应放行 8443")
	}
}

// TestDeployScript_运行判定 7b 判定必须同时检查 running 与 RestartCount:
// restart 策略的崩溃循环中进程每次重启的存活瞬间状态是 running,单查
// 状态会被 crash-loop 假通过(实测:端口被占时容器 running/restarting
// 交替,单查 ^running$ 误报成功)。
func TestDeployScript_运行判定(t *testing.T) {
	sh, err := deployScript(fixtureServer(t))
	if err != nil {
		t.Fatalf("deployScript: %v", err)
	}
	if !strings.Contains(sh, "{{.RestartCount}}") {
		t.Fatal("7b 判定应检查 RestartCount(防 crash-loop 的 running 窗口假通过)")
	}
	if !strings.Contains(sh, "^running|0$") {
		t.Fatal("7b 判定应要求 running 且 RestartCount=0")
	}
}

// ===== compose/deploy 端口口径 =====

// TestComposeYAML_无h2无证书挂载 无 h2 通道时 compose 不含证书挂载行。
func TestComposeYAML_无h2无证书挂载(t *testing.T) {
	s := fixtureServer(t)
	s.Hysteria2 = nil
	compose := composeYAML(s)
	if strings.Contains(compose, "./cert.pem") {
		t.Fatalf("无 h2 不应挂载证书:\n%s", compose)
	}
}

// TestDeployScript_自定义端口 端口直接取自清单非默认值时防火墙放行随之
// 变化(deploy.sh 端口与 config.json 监听一致,防放行口径漂移)。
func TestDeployScript_自定义端口(t *testing.T) {
	s := fixtureServer(t)
	s.VLESS.Port = 7443
	s.Hysteria2.Port = 9443
	sh, err := deployScript(s)
	if err != nil {
		t.Fatalf("deployScript: %v", err)
	}
	for _, want := range []string{"for p in 7443", "for p in 9443"} {
		if !strings.Contains(sh, want) {
			t.Fatalf("deploy.sh 缺 %q", want)
		}
	}
	for _, bad := range []string{"for p in 443", "for p in 8443"} {
		if strings.Contains(sh, bad) {
			t.Fatalf("deploy.sh 不应含默认端口放行 %q", bad)
		}
	}
}

// ===== certScript CN 契约 =====

// TestCertScript_受信证书CN 受信证书模式 certScript CN 取 server_name
// (函数契约:受信分支产品路径不可达,但函数本身支持)。
func TestCertScript_受信证书CN(t *testing.T) {
	s := &conf.Server{Address: "1.2.3.4", Hysteria2: &conf.H2Config{ServerName: "vpn.example.com"}}
	cs := certScript(s)
	if !strings.Contains(cs, "CN='vpn.example.com'") {
		t.Fatalf("cert.sh CN 应为 server_name,实际:\n%s", cs)
	}
}

// ===== WriteArtifacts 目录创建 =====

// TestWriteArtifacts_嵌套目录自动创建 目录不存在时逐级创建(MkdirAll)。
func TestWriteArtifacts_嵌套目录自动创建(t *testing.T) {
	s := fixtureServer(t)
	dir := filepath.Join(t.TempDir(), "a", "b", "hk-01")
	if err := WriteArtifacts(dir, s); err != nil {
		t.Fatalf("写产物失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err != nil {
		t.Fatalf("嵌套目录下缺 config.json: %v", err)
	}
}
