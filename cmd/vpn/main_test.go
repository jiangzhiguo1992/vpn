// Package main CLI 测试:gen 端到端(临时清单 → 全产物)与幂等、产物
// 权限、错误路径(清单校验/加载失败)、doctor 与 usage 输出。
//
// 黄金断言:服务端 config.json 与客户端产物凭据一致(部署闭环防漂移),
// 二次 gen 产物与清单不变(幂等);含凭据的产物与清单权限 0600。
package main

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// writeSmokeInventory 写临时三通道清单(测试基线,不含凭据字段)。
func writeSmokeInventory(t *testing.T, path string) {
	t.Helper()
	content := `{
  "servers": [
    {
      "name": "hk-01",
      "location": "香港",
      "address": "hk.example.com",
      "ssh": {"user": "root", "port": 22},
      "vless": {"port": 443, "server_name": "www.apple.com"},
      "hysteria2": {"port": 8443}
    },
    {
      "name": "jp-01",
      "address": "2001:db8::1",
      "vless": {"server_name": "www.apple.com"}
    }
  ]
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写清单: %v", err)
	}
}

// TestRunGen_端到端 gen 生成全产物,凭据回填清单,幂等,跨产物一致。
func TestRunGen_端到端(t *testing.T) {
	dir := t.TempDir()
	invPath := filepath.Join(dir, "servers.json")
	outDir := filepath.Join(dir, "dist")
	writeSmokeInventory(t, invPath)

	if err := runGen([]string{"-inventory", invPath, "-out", outDir}); err != nil {
		t.Fatalf("首次 gen 失败: %v", err)
	}
	// 1. 清单被回填凭据并保存
	rawInv, err := os.ReadFile(invPath)
	if err != nil {
		t.Fatalf("读清单: %v", err)
	}
	var inv struct {
		Servers []struct {
			Name  string `json:"name"`
			VLESS *struct {
				UUID string `json:"uuid"`
			} `json:"vless"`
			Hysteria2 *struct {
				Password string `json:"password"`
			} `json:"hysteria2"`
		} `json:"servers"`
	}
	if err := json.Unmarshal(rawInv, &inv); err != nil {
		t.Fatalf("解析清单: %v", err)
	}
	if inv.Servers[0].VLESS == nil || inv.Servers[0].VLESS.UUID == "" || inv.Servers[0].Hysteria2.Password == "" {
		t.Fatal("清单未被回填凭据")
	}
	if inv.Servers[1].VLESS == nil || inv.Servers[1].VLESS.UUID == "" {
		t.Fatal("第二台服务器未回填")
	}
	// 2. 产物齐全
	wantFiles := []string{
		"links.txt", "sub.txt", "clash.yaml", "sing-box.json",
		"servers/hk-01/config.json", "servers/hk-01/docker-compose.yml",
		"servers/hk-01/deploy.sh", "servers/hk-01/cert.sh",
		"servers/jp-01/config.json", "servers/jp-01/deploy.sh",
	}
	for _, f := range wantFiles {
		if _, err := os.Stat(filepath.Join(outDir, f)); err != nil {
			t.Fatalf("缺产物 %s: %v", f, err)
		}
	}
	// jp-01 无 h2:不应有 cert.sh
	if _, err := os.Stat(filepath.Join(outDir, "servers/jp-01/cert.sh")); !os.IsNotExist(err) {
		t.Fatal("jp-01 不应有 cert.sh")
	}
	// 3. 黄金断言:服务端 config.json 的 uuid/密码 == links.txt(防漂移)
	cfg, _ := os.ReadFile(filepath.Join(outDir, "servers/hk-01/config.json"))
	links, _ := os.ReadFile(filepath.Join(outDir, "links.txt"))
	uuid := inv.Servers[0].VLESS.UUID
	h2Pass := inv.Servers[0].Hysteria2.Password
	if !strings.Contains(string(cfg), uuid) {
		t.Fatal("服务端 config.json 缺清单 uuid(漂移)")
	}
	if !strings.Contains(string(links), uuid) {
		t.Fatal("links.txt 缺清单 uuid(漂移)")
	}
	if !strings.Contains(string(cfg), h2Pass) || !strings.Contains(string(links), h2Pass) {
		t.Fatal("h2 密码在服务端/links 间漂移")
	}
	// 4. links.txt 每台服务器每通道一个节点:2 服务器 = vless+h2 + vless = 3 行
	lines := strings.Split(strings.TrimSpace(string(links)), "\n")
	if len(lines) != 3 {
		t.Fatalf("links 行数 = %d, want 3(2 服务器 2+1 通道)", len(lines))
	}
	// 5. 幂等:二次 gen 后产物与清单一致
	cfgBefore, _ := os.ReadFile(filepath.Join(outDir, "servers/hk-01/config.json"))
	linksBefore := string(links)
	rawInvBefore := string(rawInv)
	if err := runGen([]string{"-inventory", invPath, "-out", outDir}); err != nil {
		t.Fatalf("二次 gen 失败: %v", err)
	}
	cfgAfter, _ := os.ReadFile(filepath.Join(outDir, "servers/hk-01/config.json"))
	linksAfter, _ := os.ReadFile(filepath.Join(outDir, "links.txt"))
	rawInvAfter, _ := os.ReadFile(invPath)
	if string(cfgBefore) != string(cfgAfter) || linksBefore != string(linksAfter) || rawInvBefore != string(rawInvAfter) {
		t.Fatal("二次 gen 产物或清单变化(幂等被破坏)")
	}
}

// TestRunGen_客户端配置可解析 验证 sing-box.json/clash.yaml 关键结构。
func TestRunGen_客户端配置可解析(t *testing.T) {
	dir := t.TempDir()
	invPath := filepath.Join(dir, "servers.json")
	outDir := filepath.Join(dir, "dist")
	writeSmokeInventory(t, invPath)
	if err := runGen([]string{"-inventory", invPath, "-out", outDir}); err != nil {
		t.Fatalf("gen 失败: %v", err)
	}
	sb, _ := os.ReadFile(filepath.Join(outDir, "sing-box.json"))
	var m map[string]any
	if err := json.Unmarshal(sb, &m); err != nil {
		t.Fatalf("sing-box.json 不是合法 JSON: %v", err)
	}
	outbounds, _ := m["outbounds"].([]any)
	// direct + 3 节点 + auto + proxy = 6
	if len(outbounds) != 6 {
		t.Fatalf("outbounds 数量 = %d, want 6", len(outbounds))
	}
	clash, _ := os.ReadFile(filepath.Join(outDir, "clash.yaml"))
	out := string(clash)
	for _, want := range []string{"jp-01-vless", "hk-01-vless", "hk-01-h2", "MATCH,PROXY"} {
		if !strings.Contains(out, want) {
			t.Fatalf("clash.yaml 缺 %q", want)
		}
	}
}

// TestRunGen_清单缺失 缺清单时给出复制示例的引导错误。
func TestRunGen_清单缺失(t *testing.T) {
	err := runGen([]string{"-inventory", filepath.Join(t.TempDir(), "nope.json"), "-out", t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "example-servers.json") {
		t.Fatalf("期望缺清单引导错误,实际: %v", err)
	}
}

// TestGoVersionAtLeast 版本判读:1.27+ 满足,旧版本拒绝,畸形串拒绝。
func TestGoVersionAtLeast(t *testing.T) {
	cases := []struct {
		ver  string
		want bool
	}{
		{"go version go1.27.0 darwin/arm64", true},
		{"go version go1.28.0 darwin/arm64", true},
		{"go version go2.0.0 linux/amd64", true},
		{"go version go1.26.7 darwin/arm64", false},
		{"go version go1.27 darwin/arm64", true}, // 无 patch 版本
		{"not-a-version", false},
	}
	for _, tc := range cases {
		if got := goVersionAtLeast(tc.ver, 1, 27); got != tc.want {
			t.Fatalf("goVersionAtLeast(%q) = %v, want %v", tc.ver, got, tc.want)
		}
	}
}

// ===== gen 产物权限 =====

// TestRunGen_产物权限 验证含凭据的客户端产物与清单均 0600,deploy.sh 为
// 0755(writeFile0600/SaveFile 显式 Chmod,mac 上 t.TempDir 目录权限不干扰)。
func TestRunGen_产物权限(t *testing.T) {
	dir := t.TempDir()
	invPath := filepath.Join(dir, "servers.json")
	outDir := filepath.Join(dir, "dist")
	writeSmokeInventory(t, invPath)

	if err := runGen([]string{"-inventory", invPath, "-out", outDir}); err != nil {
		t.Fatalf("gen 失败: %v", err)
	}
	cases := []struct {
		path string
		perm os.FileMode
	}{
		{"links.txt", 0o600},
		{"sub.txt", 0o600},
		{"clash.yaml", 0o600},
		{"sing-box.json", 0o600},
		{"servers/hk-01/deploy.sh", 0o755},
	}
	for _, tc := range cases {
		fi, err := os.Stat(filepath.Join(outDir, tc.path))
		if err != nil {
			t.Fatalf("stat %s: %v", tc.path, err)
		}
		if got := fi.Mode().Perm(); got != tc.perm {
			t.Fatalf("%s 权限 = %04o, want %04o", tc.path, got, tc.perm)
		}
	}
	// 清单本身也是凭据载体,SaveFile 后须 0600
	fi, err := os.Stat(invPath)
	if err != nil {
		t.Fatalf("stat 清单: %v", err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Fatalf("servers.json 权限 = %04o, want 0600", got)
	}
}

// ===== 错误路径(直接调用 runGen/runDeploy) =====

// TestRunGen_清单校验失败 验证非法清单(缺 server_name / name 含空格)在
// Validate 阶段被拒,错误经包装含"清单校验失败"。
func TestRunGen_清单校验失败(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"vless 缺 server_name", `{"servers":[{"name":"hk-01","address":"hk.example.com","vless":{"port":443}}]}`},
		{"name 含空格", `{"servers":[{"name":"hk 01","address":"hk.example.com","vless":{"server_name":"www.apple.com"}}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			invPath := filepath.Join(dir, "servers.json")
			if err := os.WriteFile(invPath, []byte(tc.content), 0o600); err != nil {
				t.Fatalf("写清单: %v", err)
			}
			err := runGen([]string{"-inventory", invPath, "-out", filepath.Join(dir, "dist")})
			if err == nil || !strings.Contains(err.Error(), "清单校验失败") {
				t.Fatalf("期望清单校验失败,实际: %v", err)
			}
		})
	}
}

// TestRunGen_空服务器清单 验证 {"servers": []} 经 Validate 拒绝,
// 包装错误同时含"清单校验失败"与"servers 为空"。
func TestRunGen_空服务器清单(t *testing.T) {
	dir := t.TempDir()
	invPath := filepath.Join(dir, "servers.json")
	if err := os.WriteFile(invPath, []byte(`{"servers": []}`), 0o600); err != nil {
		t.Fatalf("写清单: %v", err)
	}
	err := runGen([]string{"-inventory", invPath, "-out", filepath.Join(dir, "dist")})
	if err == nil || !strings.Contains(err.Error(), "清单校验失败") {
		t.Fatalf("期望清单校验失败(空清单),实际: %v", err)
	}
	if !strings.Contains(err.Error(), "servers 为空") {
		t.Fatalf("期望含 servers 为空,实际: %v", err)
	}
}

// TestRunDeploy_清单缺失 验证不存在的清单路径在 LoadFile 即失败,
// 错误消息须含路径(便于定位)。
func TestRunDeploy_清单缺失(t *testing.T) {
	dir := t.TempDir()
	invPath := filepath.Join(dir, "nope.json")
	err := runDeploy([]string{"-inventory", invPath, "-out", filepath.Join(dir, "dist")})
	if err == nil || !strings.Contains(err.Error(), invPath) {
		t.Fatalf("期望错误含清单路径 %s,实际: %v", invPath, err)
	}
}

// TestRunDeploy_校验失败 验证坏清单在 deploy.Run 之前即被 Validate 拒绝
// (不会发起任何 ssh/scp 调用)。
func TestRunDeploy_校验失败(t *testing.T) {
	dir := t.TempDir()
	invPath := filepath.Join(dir, "servers.json")
	content := `{"servers":[{"name":"hk-01","address":"hk.example.com","vless":{"port":443}}]}`
	if err := os.WriteFile(invPath, []byte(content), 0o600); err != nil {
		t.Fatalf("写清单: %v", err)
	}
	err := runDeploy([]string{"-inventory", invPath, "-out", filepath.Join(dir, "dist")})
	if err == nil || !strings.Contains(err.Error(), "清单校验失败") {
		t.Fatalf("期望清单校验失败,实际: %v", err)
	}
}

// ===== doctor 与 usage =====

// TestUsage_输出 验证 usage 向 stdout 打印帮助,含 gen/deploy/doctor 子命令。
func TestUsage_输出(t *testing.T) {
	old := os.Stdout
	defer func() { os.Stdout = old }()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	usage()
	w.Close()
	out, err := io.ReadAll(r)
	r.Close()
	if err != nil {
		t.Fatalf("读输出: %v", err)
	}
	for _, want := range []string{"vpn gen", "vpn deploy", "vpn doctor"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("usage 输出缺 %q:\n%s", want, out)
		}
	}
}

// TestRunDoctor_环境通过 验证本机 go/ssh/scp 齐全时自检返回 nil
// (openssl 缺失仅打印提示,不影响结果)。
func TestRunDoctor_环境通过(t *testing.T) {
	for _, bin := range []string{"go", "ssh", "scp"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("本机缺 %s,跳过 doctor 检查: %v", bin, err)
		}
	}
	if err := runDoctor(); err != nil {
		t.Fatalf("runDoctor 应通过(openssl 缺失仅打印): %v", err)
	}
}
